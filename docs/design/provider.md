# Provider 模块设计

> **文档状态**：本文档记录 Gogent 在 v0.14-v0.15 的架构演进与决策过程，包括方案对比、取舍理由与后续演进方向。代码与本文档如有出入，以代码为准。

本文档描述 Provider 模块的多供应商支持设计：引擎体系、ProviderManager 统一管理、凭证体系与运行时切换。

## 一、设计目标

为使用 Gogent 构建的 agent 应用提供**多种 LLM 供应商**的连接能力：

- OpenAI（原生引擎）
- OpenAI 兼容格式（DeepSeek、Groq、Mistral、Ollama、vLLM 等 10+ 家）
- 未来可扩展：本地模型、企业私有端点等

**核心原则**：App 开发者配置"支持哪些供应商"，终端用户在运行时自由选择/切换。框架默认向终端用户暴露所有已配置的供应商。

## 二、术语

| 术语 | 含义 |
|------|------|
| **供应商（Provider）** | 具体的 LLM 服务提供商（OpenAI、DeepSeek…） |
| **引擎（Engine）** | Provider 的实现类型，如 `openai`、`deepseek`、`openai-compat` |
| **ProviderManager** | 管理所有 Provider 实例的统一协调器 |
| **CredentialStore** | 凭证（API Key）的存储抽象 |
| **原生 Provider** | 应用进程内直接通过 REST API 调用的 Provider |

## 三、架构

### 3.1 ProviderManager（统一管理入口）

ProviderManager 采用 **HookManager 模式**：管理器本身是一个 Component（`GetType()` 返回 `ComponentProvider`，无需新增 ComponentType），单个 Provider 实例是普通 `IProvider` 对象，通过 `Register()` 注册进 Manager，不进 Registry。

```
ProviderManager = 所有 Provider 实例的统一查询目录，且自身是 Component

设计要点：
  · GetType() 返回 ComponentProvider —— 无需新增 ComponentType
  · AgentRuntime.Dependencies()["provider"] 依赖声明原样有效
  · 子项（IProvider）不进 Registry，由 Manager 统一持有
  · 管理 Provider 的注册 / 注销 / 发现
  · 为 Interface 层提供 ProviderInfo 查询（展示给终端用户）
  · 为 AgentCore 提供按名选取
  · 不包装 Generate()/Stream() 调用（那是 AgentCore 的职责）
  · 不做请求/响应格式转换（那是各 Provider 实现的职责）
  · 不管理凭证（CredentialStore 是独立抽象）
```

选择该模式而非其他备选方案的原因：

| 方案 | 结论 |
|------|------|
| ProviderManager 作为独立 Component，新增 `ComponentProviderManager` 类型 | 放弃——变体采用：Manager 自身即 Component，复用 `ComponentProvider` 类型 |
| Builder 注入 `ProviderComponent.SetManager(pm)` | 放弃——增加组件间耦合 |
| 全局注册（database/sql 风格） | 放弃——全局状态影响测试隔离与多 App 实例 |

### 3.2 引擎注册表

采用 database/sql 驱动模式：引擎实现通过 `init()` 自我注册，Builder 枚举所有已注册引擎并实例化进 ProviderManager。

```go
// pkg/provider/config.go
type EngineFactory func() (IProvider, error)

func RegisterEngine(name string, f EngineFactory) error
func CreateEngine(name string) (IProvider, error)   // 未知引擎 → ErrUnknownEngine
func RegisteredEngines() []string
```

**Builder 注册语义**（native 路径）：

```go
for _, engine := range provider.RegisteredEngines() {
    if excluded[engine] {
        continue
    }
    impl, err := provider.CreateEngine(engine)
    if err != nil {
        return err
    }
    b.providerManager.Register(engine, impl)   // 以引擎名为注册名
}
```

### 3.3 配置机制（顶级 provider 域）

**v0.15.x 起 provider 采用顶级 `provider:` 域配置**，与 tool（`tools:`）/ sandbox（`sandboxes:`）对齐，不再占用 `components[]`：

- **内置引擎始终注册**：框架内置引擎（openai/deepseek/...）无条件构建——`CreateEngine` 只构造实例不发请求，无 key 时调用才报错。无 `provider:` 域 = 全部内置引擎可用
- **黑名单排除**：`provider.exclude` 排除不需要的引擎
- **外部供应商**：`provider.servers` 以 gRPC endpoint 直连外部 ProviderService，注册进同一 ProviderManager
- **只声明连接可能性**：YAML 不含 apiKey/model——凭证与偏好属终端用户，运行时决定

```yaml
provider:
  exclude: ["openai"]        # 可选；留空则全部启用
  servers:                   # 可选；外部供应商
    - name: "my-gateway"
      endpoint: "localhost:9092"
```

**命名空间保护**：内置引擎名为保留名，`servers[].name` 与之冲突时构建报错——避免 `/provider openai` 静默路由到远端而非原生引擎（与 tool 的 builtin 名保护同构）。

**defaults.provider 废弃**：ProviderManager 是唯一的 provider 组件且 Register 时自动成为默认，`defaults.provider` 条目失去意义（会被忽略并提示）。

### 3.4 路线选择（统一管理）

设计时对比了两条路线：

| 维度 | 路线 1（process/http 绕过 Manager） | 路线 2（统一管理，采用） |
|------|-----------------------------------|------------------------|
| 终止态完整性 | 永远维护两套查询 | 单入口 |
| RouterProvider 支持 | 不可能 | 可选扩展 |
| 配置一致性 | 两套配置模型 | 统一抽象 |
| 实现风险 | 短期低，长期高 | 短期稍高，长期低 |

**选择路线 2**：单入口、支持 Router 扩展、配置一致，符合框架"可插拔可替换"的核心哲学。

**实施演进**：native 路径先行（ProviderManager + 引擎注册表 + exclude）；**v0.15.x 完成 problem C**——process/http 供应商（`provider.servers`）统一注册进 ProviderManager，以 gRPC endpoint 直连，不再以独立组件形式出现在 Registry。同时 provider 移出 `components[]`，采用顶级 `provider:` 域配置。

## 四、Provider 实现策略

### 4.1 代码布局

```
pkg/provider/
├── provider.go              # IProvider 接口 + 共享类型
├── manager.go               # ProviderManager — Component + Register/Get/List
├── config.go                # 引擎注册表 RegisterEngine/CreateEngine
├── errors.go                # 哨兵错误 + ProviderError
├── models.go                # ModelInfo（DisplayName + Models）
├── openai.go                # OpenAIProvider（Generate/Stream/SSE/ToolCall）
├── openai_compat.go         # NewOpenAICompat 内部基类（不注册为独立引擎）
├── deepseek.go              # DeepSeekProvider（OpenAI 兼容薄包装）
├── credentials.go           # CredentialStore 接口 + FileCredentialStore
├── ctx.go                   # WithProviderName/WithModel（ctx 携带路由）
├── process.go               # ProcessProvider（gRPC 客户端，servers 外部供应商用）
└── default.go               # 保留兼容（deprecated 标记）
```

> `component.go`（ProviderComponent）已在 v0.15.x 删除——process/http 供应商不再需要组件包装，统一由 ProcessProvider 注册进 Manager。

### 4.2 IProvider 接口

```go
type IProvider interface {
    Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
    Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
    ModelInfo() ModelInfo
}
```

接口只有 3 个方法，边界清晰，适合作为所有 Provider 实现的统一契约，保持不变。

### 4.3 引擎差异与统一策略

**全部使用标准库 `net/http`，不引入各供应商官方 SDK**。理由：减少依赖树、统一错误处理与重试策略、避免 SDK 版本破坏兼容性。

核心工作量在**消息格式转换**（OpenAI `chat/completions` 的 JSON/SSE 与各家格式不同），网络传输模式本身对齐。

**OpenAI 兼容引擎清单**（复用 `NewOpenAICompat` 基类 + 独立 env key/baseURL 注册）：

| 引擎 | baseURL | API key env |
|------|---------|-------------|
| deepseek | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` |
| groq | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` |
| mistral | `https://api.mistral.ai/v1` | `MISTRAL_API_KEY` |
| ollama（本地） | `http://localhost:11434/v1` | 无 |
| openrouter | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` |
| 其他（together/vllm/...） | 各自 | 各自 |

### 4.4 模型列表与默认模型

- `Models` 由引擎在 `ModelInfo()` 时动态拉取 `GET {baseURL}/models`（OpenAI 兼容格式 `data[].id`），TTL 10 分钟缓存；拉取失败返回空列表。**移除所有硬编码模型名**。
- **默认模型**同样不硬编码：解析链为 `cfg.Model → env → /models 列表第一个 → 空`（空时引擎在 Generate 时动态解析）。
- `ContextSize` 字段保留用于接口稳定，但引擎不填充（恒为 0）——各家 `/models` 端点大多不返回 context window，业界做法（LiteLLM 集中规格表等）留作后续演进。

## 五、凭证管理

### 5.1 设计原则

配置文件**不包含 secrets**。配置存稳定的、非敏感的信息（连接可能性）；密钥存凭证文件/外部存储。

**复用 `internal/credentials` 的加载/解析/合并逻辑**（`LoadCredentials`/`WriteCredentials`/`DeleteCredentials`），但凭证文件**按 app 隔离**——`FileCredentialStore` 写入 `~/.gogent/apps/<app-name>/credentials.yaml`，不共用 daemon 文件（避免多 agent 应用的 provider 凭证与 daemon 工具/沙箱凭证混淆）。

### 5.2 CredentialStore 接口

```go
type CredentialStore interface {
    Get(providerName string) (string, error)   // providerName = 引擎名精确键（"openai"、"deepseek"）
    Set(providerName, apiKey string) error
    Delete(providerName string) error
    List() ([]string, error)                   // 已配置凭证的 provider 列表
}

// 引擎接入（可选接口，Builder 统一注入，无全局状态）
type CredentialStoreAware interface {
    SetCredentialStore(s CredentialStore)
}
```

引擎 key 解析优先级：**CredentialStore > 构造时 env**（向后兼容）。`WithCredentialStore` 可注入自定义实现（Vault、keyring）。

### 5.3 交互式配置

REPL 内 `/key` 命令（写入 app 作用域 credentials.yaml，密钥脱敏显示，设置即生效）：

```
> /key
Provider credentials:
  openai     (not configured)
  deepseek   (not configured)

> /key openai sk-xxxxxxxxxxxx
Saved API key for openai

> /key
Provider credentials:
  openai     ****xxxx
  deepseek   (not configured)
```

| 命令 | 行为 |
|------|------|
| `/key` | 列出所有 provider 的凭证状态（密钥脱敏） |
| `/key <name>` | 查看单个 provider 凭证状态 |
| `/key <name> <apiKey>` | 设置凭证（写 app 作用域 credentials.yaml，立即生效） |
| `/key <name> --delete` | 删除凭证 |

### 5.4 敏感信息放置原则

| 信息 | 存放位置 | 谁写入 |
|------|---------|--------|
| 连接可能性（黑名单 exclude） | YAML 配置 | App 开发者 |
| API Key 等密钥 | CredentialStore（app 作用域 credentials.yaml） | 终端用户（首次运行引导） |
| 默认模型名 | 终端用户偏好（运行时） | 终端用户 |

## 六、运行时路由（ProviderManager dispatch）

ProviderManager 是路由入口：从 context 读取 provider 名（由 AgentCore 从 `Input.ProviderName` 设置），转发到匹配引擎；模型名同样经 ctx 传递，引擎内部覆盖默认。

```go
// pkg/provider/ctx.go
provider.WithProviderName(ctx, name)   // 写入 ctx
provider.WithModel(ctx, model)
// pkg/provider/manager.go
func (m *ProviderManager) Generate(ctx, messages)   // 从 ctx 读 provider 名路由
```

**切换粒度**：

| 层 | 粒度 | 实现 |
|----|------|------|
| CLI chat | 会话级 | REPL 内 `currentProvider` + `currentModel` 状态，`/provider` `/model` 命令切换 |
| CLI run | 消息级 | `run -p "..." --provider <name> --model <name>` 单次指定 |
| HTTP（未来） | 消息级 | 每条请求 `Input.ProviderName` / `Input.ModelName` |

选择 **ctx 携带**而非扩展 IProvider 接口加参数：接口保持不变，引擎实现无需感知路由机制。

## 七、参考

- 行业模式：gh CLI、Claude Code、AWS CLI — 凭证管理（OS keyring 优先）
- Go 凭证库：zalando/go-keyring、byteness/keyring
- 项目现有凭证：`internal/credentials/` — credentials.yaml + ${VAR} 解析 + fsnotify
- 项目架构总览：`ARCHITECTURE.md`（仓库根目录）
