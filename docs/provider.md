# Provider 模块设计方案（v0.14.x）

> 本文档描述 gogent 框架 Provider 模块的多供应商支持设计，涵盖现状分析、目标架构、路线选择、凭证管理与待决策问题。
> 状态：**设计中（v0.14.1 实施中）**
> 关联版本：v0.13.x（当前基线）
>
> **已定稿决策**：
> - 路线：**路线 2**（Process/HTTP 统一由 ProviderManager 管理，终态）
> - 架构模式：**HookManager 模式**（ProviderManager 本身是 Component，`GetType()` 返回 `ComponentProvider`，无需新增 ComponentType）
> - 配置机制：**黑名单**（默认提供所有 native 引擎，用户用 `exclude` 排除）
> - YAML 只声明**连接可能性**，不含 apiKey/model（终端用户凭证与偏好运行时决定，属凭证管理阶段）
> - `ModelInfo` 扩展：新增 `DisplayName` + `Models` 字段
> - `DefaultProvider` / `ProviderComponent`：保留文件，标记 deprecated，后续统一清理
> - ProcessProvider 配置字段：结构先按完整字段定型，v0.14.1 阶段 process/http 驱动保持现状，后续按问题 C 调整

---

## 一、背景与需求

### 1.1 目标

为使用 gogent 框架构建的 agent 应用提供**多种不同 LLM 供应商**的连接能力，包括但不限于：

- OpenAI
- OpenAI 兼容格式（DeepSeek、Groq、Mistral、Ollama、vLLM 等 10+ 家）
- Google Gemini
- Anthropic Claude
- OpenRouter（聚合网关）
- 未来可扩展：本地模型（Ollama）、企业私有端点等

### 1.2 需求澄清（关键）

```
❌ 错误理解：App 开发者在 YAML 中指定"我要用哪一家供应商"，终端用户只能用这一家
✅ 正确理解：App 开发者将"支持哪些供应商"配置进 YAML，终端用户在运行时自由选择/切换
```

**核心原则**：框架提供全面的供应商连接能力，App 默认向终端用户暴露所有已配置的供应商，由终端用户自主选择。市面上几乎所有 AI 应用都尽可能提供全面的供应商连接，没有拒绝的理由。

### 1.3 术语约定


| 术语                  | 含义                                                             |
| ------------------- | -------------------------------------------------------------- |
| **供应商 (Provider)**  | 具体的 LLM 服务提供商（OpenAI、Gemini、Claude…）                           |
| **引擎 (Engine)**     | Provider 的实现类型，如 `openai`、`gemini`、`anthropic`、`openai-compat` |
| **ProviderManager** | 管理所有 Provider 实例的统一协调器（本阶段核心新增）                                |
| **CredentialStore** | 凭证（API Key）的存储抽象                                               |
| **进程型 Provider**    | 通过 gRPC/JSON-RPC 与远程 daemon 通信的 Provider（现有 ProcessProvider）   |
| **原生 Provider**     | 应用进程内直接通过 REST API 调用的 Provider（本阶段新增）                         |


---

## 二、现状分析

### 2.1 pkg/provider/ 现有结构


| 文件             | 内容                                                                            | 状态                 |
| -------------- | ----------------------------------------------------------------------------- | ------------------ |
| `provider.go`  | `IProvider` 接口 + 共享类型（`ProviderMessage`、`Response`、`StreamChunk`、`ModelInfo`） | ✅ 接口简洁，可复用         |
| `default.go`   | `DefaultProvider` — 函数回调注入的占位实现                                               | ⚠️ 仅占位，无真实逻辑       |
| `component.go` | `ProviderComponent` — 组件封装（日志、生命周期）                                           | ✅ 可复用              |
| `process.go`   | `ProcessProvider` — gRPC 委托到远端 daemon                                         | ✅ 进程型 Provider 通信层 |


### 2.2 IProvider 接口（现状）

```go
type IProvider interface {
    Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
    Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
    ModelInfo() ModelInfo
}
```

**评估**：接口只有 3 个方法，边界清晰，适合作为所有 Provider 实现的统一契约，**保持不变**。

### 2.3 Builder 现状（buildProvider）

```go
func (b *Builder) buildProvider(cc ComponentConfig) (component.Component, error) {
    switch cc.Driver {
    case string(component.DriverHTTP), string(component.DriverProcess):
        p := provider.NewProcessProvider(&provider.ProcessProviderConfig{
            Name:   cc.Name,
            Pool:   b.pool,
            Target: targetFromEnvOrConfig("GOGENT_PROVIDER_TARGET", cc.Config),
        })
        return provider.NewComponent(cc.Name, p), nil
    default:
        return nil, nil   // ← driver: native 直接跳过，无原生实现
    }
}
```

**关键问题**：

1. **不存在任何原生内嵌的 LLM 供应商实现** — 用户想连 OpenAI 必须自建 gRPC daemon
2. YAML 中 `config.endpoint/apiKey/model/timeout` 字段**被忽略**（ProcessProviderConfig 只取 target）
3. `driver: native` 分支直接返回 nil，`componentDefaults` 中 `// TODO: default provider` 一直未实现

### 2.4 依赖系统现状（拓扑排序）

```go
// pkg/component/registry.go — topologicalSort 只使用 dep.Name 建边
for name, c := range r.components {
    for _, dep := range c.Dependencies() {
        if dep.Name != "" {
            graph[dep.Name] = append(graph[dep.Name], name)
            inDegree[name]++
        }
    }
}
```

```go
// AgentRuntime.Dependencies() 中 provider 依赖
"provider": {
    Type:     component.ComponentProvider,
    Required: true,     // 但 Name 为空，Required 从未被校验
    Multiple: false,    // 尚未使用
}
```

**结论**：由于 `dep.Name == ""`，拓扑排序**不会为 provider 依赖建边**。这意味着：

- 多个 Provider 组件可以自由注册，不影响初始化顺序
- 没有任何校验强制"必须有 provider"
- **Registry 天然支持多 Provider 实例共存**（`GetByType(ComponentProvider)` 可拿到全部）

### 2.5 已有凭证基础设施（internal/credentials/）

项目已存在 `internal/credentials/` 包：

```go
// Resolver 解析顺序：credentials.yaml → os.Getenv
resolver := credentials.NewResolver(creds)
resolver.Resolve("${OPENAI_API_KEY}")   // → ~/.gogent/credentials.yaml → 环境变量
```

- 凭证文件位置：`~/.gogent/credentials.yaml`（0600 权限）
- 支持 fsnotify 热加载（`Resolver.Watch(ctx)`）
- 已被 tool / sandbox 子系统共用（如 `${E2B_API_KEY}`）

**含义**：框架已有"配置不存密钥、密钥走统一凭证文件"的既定模式，Provider 模块的凭证管理应**复用/扩展**此基础设施，而非另起炉灶。

---

## *三、目标架构*

### 3.1 总体架构图

```
┌─────────────────────────────────────────────────────────────┐
│ YAML 配置（App 开发者编写 — 声明"连接可能性"）                │
│  provider-main: { exclude: [...] }   ← 黑名单，默认全开      │
│  provider-remote: { target: ... }    ← process/http 显式声明 │
└──────────────────────┬──────────────────────────────────────┘
                       │ Builder 解析
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ Registry                                                    │
│  ComponentProvider: [provider-manager]   ← 唯一 provider 组件 │
│    └── ProviderManager 内部持有 openai/gemini/claude/...     │
└──────────────────────┬──────────────────────────────────────┘
                       │ Interface.Run(ctx, reg)
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ Interface Layer (CLI / HTTP)                                 │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 终端用户交互                                            │  │
│  │  /provider            → 列出所有可用供应商              │  │
│  │  /provider gemini     → 切换供应商                     │  │
│  │  消息 → Input{ProviderName: "gemini", ...}            │  │
│  └────────────┬──────────────────────────────────────────┘  │
└───────────────┼─────────────────────────────────────────────┘
                │ agent.Run(ctx, input)
                ▼
┌─────────────────────────────────────────────────────────────┐
│ AgentCore (DefaultAgent)                                     │
│  ① 解析 Input.ProviderName                                   │
│  ② 从 ProviderManager / Registry 查找 Provider 实例          │
│  ③ 调用 provider.Generate(ctx, messages)                    │
└───────────────┬─────────────────────────────────────────────┘
                │ provider.Generate()
                ▼
┌──────────┬──────────┬──────────┬──────────────┬───────────┐
│ OpenAI   │ Gemini   │ Claude   │ OpenRouter   │ Process   │
│ (native) │ (native) │ (native) │ (compat)     │ (gRPC)    │
└──────────┴──────────┴──────────┴──────────────┴───────────┘
```

### 3.2 ProviderManager 定位（已定稿：HookManager 模式）

**架构模式**（对齐 `pkg/hook/manager.go`）：ProviderManager **本身是 Component**——嵌入 `component.BasicComponent`，直接实现 `GetType/Initialize/Start/Stop/Dependencies`。单个 provider 实例（OpenAI、Gemini、Process…）是普通 `IProvider` 对象，通过 `Register()` 注册进 Manager，**不进 Registry**。

```
ProviderManager = 所有 Provider 实例的统一查询目录，且自身是 Component

关键设计点：
  ✅ GetType() 返回 component.ComponentProvider —— 无需新增 ComponentType
  ✅ AgentRuntime.Dependencies()["provider"] 依赖声明原样有效
  ✅ 子项（IProvider）不进 Registry，由 Manager 统一持有
  ✅ 与 HookManager 逐点对齐：嵌入 BasicComponent / 生命周期日志 / Register 注册
  ✅ 管理 Provider 的注册 / 注销 / 发现
  ✅ 为 Interface 层提供 ProviderInfo 查询（展示给终端用户）
  ✅ 为 AgentCore 提供按名选取
  ❌ 不包装 Generate()/Stream() 调用（那是 AgentCore 的职责）
  ❌ 不做请求/响应格式转换（那是各 Provider 实现的职责）
  ❌ 不管理凭证（CredentialStore 是独立抽象）
```

### 3.3 预期接口设计（定稿）

```go
// pkg/provider/manager.go — ProviderManager（HookManager 模板）
type ProviderManager struct {
    component.BasicComponent
    mu        sync.RWMutex
    providers map[string]IProvider   // 以引擎名为 key："openai"、"gemini"…
}

func NewManagerComponent(name string) *ProviderManager

// Component 生命周期（照抄 HookManager 模板）
func (m *ProviderManager) GetType() component.ComponentType { return component.ComponentProvider }
func (m *ProviderManager) Initialize(ctx, reg) error
func (m *ProviderManager) Start(ctx) error
func (m *ProviderManager) Stop(ctx) error
func (m *ProviderManager) Dependencies() map[string]component.DependencySpec

// 业务方法
func (m *ProviderManager) Register(name string, p IProvider) error
func (m *ProviderManager) Unregister(name string) error
func (m *ProviderManager) Get(name string) IProvider
func (m *ProviderManager) List() []ProviderInfo      // Interface 层展示

// pkg/provider/config.go — 引擎注册表（database/sql 风格）
type EngineFactory func() (IProvider, error)

func RegisterEngine(name string, f EngineFactory) error
func CreateEngine(name string) (IProvider, error)   // 未知引擎 → ErrUnknownEngine
func RegisteredEngines() []string                    // 已注册引擎列表
func IsEngineRegistered(name string) bool

// 展示结构
type ProviderInfo struct {
    Name        string   // "openai" — 引擎标识（注册名）
    DisplayName string   // "OpenAI" — 终端用户可见
    Models      []string // 能力声明（该引擎支持的模型），供用户选择
}
```

**Builder 注册语义**：

```go
// native 路径：遍历引擎注册表，exclude 过滤后全部注册进 Manager
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

### 3.4 YAML 配置设计（定稿：黑名单机制）

**设计原则**：

| 原则 | 说明 |
|------|------|
| 默认全开 | 框架内置的所有 native 引擎默认启用，无需逐个声明 |
| 黑名单排除 | 用户用 `exclude` 字段排除不需要的引擎（白名单的逆向） |
| 只声明连接可能性 | YAML 只描述"app 支持连接哪些供应商"，不含 apiKey/model |
| 能力 vs 偏好 | 引擎支持的模型列表（能力）进代码；具体选哪个模型/哪个 key（偏好）运行时决定 |

**YAML 结构**：

```yaml
name: my-agent
version: "1.0.0"

interface:
  type: cli

components:
  # native provider：默认全开所有内置引擎，exclude 黑名单排除
  - name: "provider-main"
    type: "provider"
    driver: "native"
    config:
      exclude: ["openai", "openrouter"]    # 可选；留空则全部启用

  # 远程 provider：显式声明（需要 target，无法默认）
  - name: "provider-remote"
    type: "provider"
    driver: "process"
    config:
      target: "localhost:9092"
```

**结构要点**：
- **apiKey / model / endpoint / timeout 全部移出 YAML** —— 属于终端用户凭证与偏好，进 CredentialStore（v0.14.5 凭证管理阶段）
- native provider 为**单一组件条目**（对应 ProviderManager），配置仅 `exclude`
- process/http 保持**显式声明**（`target` 是框架级连接信息，无法默认，v0.14.1 暂保持现状，问题 C 后续统一）
- 引擎由"默认全开"机制确定，YAML 无需 `engine` 字段

---

## 四、两条路线对比（已定稿：路线 2）

> 本章记录设计时的路线对比分析。**结论：路线 2（统一管理）已定稿**，ProviderManager 采用 HookManager 模式实现（详见 3.2）。

### 4.1 路线 1：Process/HTTP 绕过 ProviderManager

```
ProviderManager                  ProcessProvider
  ├── OpenAIImpl (native)         (gRPC daemon, 独立存在)
  ├── GeminiImpl (native)
  ├── AnthropicImpl (native)
  └── ...
```

**表现**：Builder 按 driver 分支——native 进 Manager，process/http 独立注册。

**暴露的问题**：


| 问题                  | 严重度   | 说明                                          |
| ------------------- | ----- | ------------------------------------------- |
| 提供商清单分裂             | 🔴 致命 | Interface 层要同时查 Manager 和 Registry，查不全或不同步  |
| RouterProvider 不可实现 | 🔴 致命 | native 与 process 之间无法统一 failover/负载均衡       |
| 健康检查重复              | 🟡 中  | 两套健康检查逻辑                                    |
| 配置语义不一致             | 🟡 中  | `engine` 对 process 无意义，`target` 对 native 无效 |
| ModelInfo 无法聚合      | 🟡 中  | 展示"所有可用模型"需合并两个来源                           |


**结论**：过渡态，不是终局。后续每加一个跨 Provider 功能都会遇到分裂问题。

### 4.2 路线 2：Process/HTTP 统一由 ProviderManager 管理

```
ProviderManager
  ├── OpenAIImpl (native)
  ├── GeminiImpl (native)
  ├── AnthropicImpl (native)
  ├── OpenRouterImpl (native)
  ├── ProcessProvider (现有 gRPC daemon，封装后注册)
  └── RouterProvider (可选，聚合多个后端)
```

**表现**：ProviderManager 是**唯一**查询入口；ProcessProvider 作为 IProvider 实现之一注册进 Manager。

**暴露的问题**（详见第六章）：


| 问题                     | 严重度       | 说明                                                     |
| ---------------------- | --------- | ------------------------------------------------------ |
| Manager 如何被填充          | 🔴 核心设计问题 | 三种方案待选（见 6.1）                                          |
| 配置模型冲突                 | 🟡 中      | ProcessProvider 的 gRPC target 与原生 REST endpoint 配置模型不同 |
| 需要统一 config 抽象         | 🟡 中      | 需引入 ProviderFactory 注册表模式                              |
| ProcessProvider 配置字段去留 | 🟡 中      | endpoint/apiKey/model 是否重新生效（见 6.3）                    |


### 4.3 对比结论


| 维度                | 路线 1（绕过）   | 路线 2（统一）            |
| ----------------- | ---------- | ------------------- |
| 终止态完整性            | ❌ 永远维护两套查询 | ✅ 单入口               |
| RouterProvider 支持 | ❌ 不可能      | ✅ 可选扩展              |
| 配置一致性             | ❌ 两套配置模型   | ✅ 统一抽象              |
| 实现风险              | 🟡 短期低，长期高 | 🟡 短期稍高，长期低         |
| 与当前架构冲突           | 🟢 几乎零改动   | 🟡 需新增 Manager 注册机制 |


**推荐方向：路线 2（统一管理）**，理由：单入口、支持 Router 扩展、配置一致，符合框架"可插拔可替换"的核心哲学。**✅ 已定稿。**

**v0.14.1 实施范围界定**：native 路径先行（ProviderManager + 引擎注册表 + exclude 黑名单）；process/http 驱动暂保持现有独立行为（不阻塞基础设施落地），问题 C 在后续阶段统一改造。

---

## 五、Provider 实现策略

### 5.1 代码布局（当前状态）

```
pkg/provider/
├── provider.go              # IProvider 接口 + 共享类型（不变）
├── component.go             # ProviderComponent — deprecated（v0.14.1 标记，后续清理）
├── process.go               # ProcessProvider（v0.14.1 不动，问题 C 后续调整）
├── default.go               # DefaultProvider — deprecated（保留向后兼容）
├── manager.go               # ProviderManager — Component + Register/Get/List ✅ v0.14.1
├── config.go                # 引擎注册表 RegisterEngine/CreateEngine ✅ v0.14.1
├── errors.go                # 哨兵错误 + ProviderError ✅ v0.14.1/2
├── models.go                # ModelInfo 扩展（DisplayName+Models）✅ v0.14.1
├── openai.go                # OpenAIProvider ✅ v0.14.2（Generate/Stream/SSE/ToolCall）
├── openai_compat.go         # NewOpenAICompat 内部基类 ✅ v0.14.2（不注册为独立引擎）
├── deepseek.go              # DeepSeekProvider ✅ v0.14.2（OpenAI 兼容薄包装）
├── gemini.go                # GeminiProvider ⏳ TODO（v0.14.3 规划）
├── anthropic.go             # AnthropicProvider ⏳ TODO（v0.14.4 规划）
├── credentials.go           # CredentialStore 接口 + FileCredentialStore ✅ v0.14.5
└── *_test.go                # 各 Provider 单元测试
```

**OpenAI 兼容供应商引擎清单**（复用 `NewOpenAICompat` 基类 + 独立 env key/baseURL 注册）：

| 引擎 | baseURL | API key env | 状态 |
|------|---------|-------------|------|
| deepseek | `https://api.deepseek.com/v1` | `DEEPSEEK_API_KEY` | ✅ v0.14.2 |
| groq | `https://api.groq.com/openai/v1` | `GROQ_API_KEY` | ⏳ TODO |
| mistral | `https://api.mistral.ai/v1` | `MISTRAL_API_KEY` | ⏳ TODO |
| ollama（本地） | `http://localhost:11434/v1` | 无 | ⏳ TODO |
| openrouter | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | ⏳ TODO |
| 其他（together/vllm/...） | 各自 | 各自 | ⏳ TODO |

### 5.2 引擎与依赖

| 引擎            | 依赖                   | 说明                                                         | 状态   |
| ------------- | -------------------- | ---------------------------------------------------------- | ---- |
| openai        | 无（标准 net/http）       | 市场覆盖率最高                                                    | ✅ v0.14.2 |
| openai-compat | 无                    | 内部基类，仅替换 baseURL（不注册为独立引擎）                                 | ✅ v0.14.2 |
| deepseek      | 无                    | OpenAI 兼容薄包装，`DEEPSEEK_API_KEY` + 内置 baseURL                 | ✅ v0.14.2 |
| gemini        | 无（REST API，不走官方 SDK） | 减少依赖树，统一错误处理                                               | ⏳ TODO v0.14.3 |
| anthropic     | 无                    | 标准 net/http                                                | ⏳ TODO v0.14.4 |
| openrouter    | 无                    | 本质是 openai-compat，自定义 endpoint                             | ⏳ TODO |
| groq/mistral/ollama 等 | 无                    | 各为独立引擎（薄包装 + 独立 env key + 内置 baseURL）                   | ⏳ TODO |
| process       | 现有 gRPC 依赖           | 保持现状                                                       | 现状    |


**推荐策略**：全部使用标准库 `net/http`，不引入各供应商官方 SDK。理由：减少依赖树、统一错误处理和重试策略、避免 SDK 版本破坏兼容性。

### 5.3 各引擎差异点（实现核心工作量）


| 差异点          | OpenAI                  | Gemini                            | Anthropic            |
| ------------ | ----------------------- | --------------------------------- | -------------------- |
| API 端点       | `/v1/chat/completions`  | `/{model}:generateContent`        | `/v1/messages`       |
| 消息格式         | `[{role, content}]`     | `{contents: [{parts: [{text}]}]}` | `[{role, content}]`  |
| Tool calling | `tools` → `tool_calls`  | `tools` → `functionCallingConfig` | `tools` → `tool_use` |
| Stream       | SSE                     | SSE（格式不同）                         | SSE（格式不同）            |
| Auth header  | `Authorization: Bearer` | `x-goog-api-key`                  | `x-api-key`          |


**核心工作量在消息格式转换**，网络传输模式（HTTP + SSE）本身对齐。

---

## 六、凭证管理设计

### 6.1 行业最佳实践（调研结论）


| 层级     | 技术                                                                             | 适用场景                       | 代表项目                         |
| ------ | ------------------------------------------------------------------------------ | -------------------------- | ---------------------------- |
| Tier 1 | OS Keyring（macOS Keychain / Windows Credential Manager / Linux Secret Service） | 终端用户本地运行                   | gh CLI, Claude Code, AWS CLI |
| Tier 2 | 加密文件 fallback                                                                  | 无 keyring 环境（容器、WSL、无头服务器） | byteness/keyring             |
| Tier 3 | 环境变量                                                                           | CI/CD 自动化                  | 所有 CI 平台                     |
| Tier 4 | 自定义 CredentialProvider                                                         | 企业自有密钥管理                   | Vault, AWS Secrets Manager   |


**关键原则**：配置文件**不包含 secrets**。配置存稳定的、非敏感的信息（URL、默认模型名等）；密钥存 OS keyring / 加密凭证文件。

### 6.2 与现有 internal/credentials/ 的关系（✅ 已定稿）

项目已有 `~/.gogent/credentials.yaml`（0600）+ `Resolver`（${VAR} 解析 + fsnotify 热加载），被 tool/sandbox 共用。

**决策**：复用 `internal/credentials` 的**加载/解析/合并逻辑**（`LoadCredentials`/`WriteCredentials`/`DeleteCredentials`），但凭证文件**按 app 隔离**——`FileCredentialStore` 写入 `~/.gogent/apps/<app-name>/credentials.yaml`，不共用 daemon 文件。

> 注：`pkg/provider` 与 `internal/credentials` 同属 `github.com/tltre/gogent/` 模块树，可正常导入（早期文档"internal 无法被 pkg 引用"的顾虑不成立）。

对比记录（设计时选项）：

```
方案 X：直接复用 internal/credentials（共用 daemon 的 credentials.yaml）
  ❌ 否决 —— 多 agent 应用的 provider 凭证与 daemon 工具/沙箱凭证混淆

方案 Y：新建 CredentialStore 抽象，默认实现走 keyring
  ⚠️ 延后 —— 需引入第三方 keyring 依赖；接口已抽象，后续可无痛升级

方案 Z：CredentialStore 抽象 + 默认实现封装现有 credentials.yaml（✅ 采用）
  ✅ 复用 internal/credentials 逻辑，零新依赖
  ✅ 按 app 隔离（apps/<name>/credentials.yaml），不与 daemon 文件混淆
  ✅ 提供 WithCredentialStore 注入点，可替换为 Vault/keyring
```

### 6.3 CredentialStore 接口（✅ 已定稿）

```go
type CredentialStore interface {
    Get(providerName string) (string, error)   // providerName = 引擎名精确键（"openai"、"deepseek"）
    Set(providerName, apiKey string) error
    Delete(providerName string) error
    List() ([]string, error)  // 已配置凭证的 provider 列表
}

// 引擎接入（可选接口，Builder 统一注入，无全局状态）：
type CredentialStoreAware interface {
    SetCredentialStore(s CredentialStore)
}
```

**终端用户体验（首次运行）**：

```
$ ./my-agent

Welcome! First-time setup...

[1] OpenAI        — no key configured
[2] Google Gemini — no key configured
[3] Anthropic     — no key configured

Select a provider to configure: 1
Enter your OpenAI API key: ************
Validating... ✓
Key saved

> /provider openai
Switched to OpenAI
```

**Builder 接入点**：

```go
builder.Build()                          // 默认 CredentialStore
builder.Build(
    app.WithCredentialStore(myStore),    // App 开发者自定义
)
```

### 6.4 敏感信息放置原则（结论）


| 信息               | 存放位置                                        | 谁写入          |
| ---------------- | ------------------------------------------- | ------------ |
| 连接可能性（黑名单 exclude） | YAML 配置（app 仓库）                             | App 开发者      |
| API Key 等密钥      | CredentialStore（keyring / credentials.yaml） | 终端用户（首次运行引导） |
| 默认模型名 / endpoint | 终端用户偏好（运行时）                                | 终端用户        |


**必须允许 App 开发者自定义凭证管理行为**——通过 `WithCredentialStore(BuildOption)` 注入，框架提供默认实现。

> ⚠️ 注意：`默认模型名 / endpoint` 不再进 YAML。模型与 key 均属终端用户偏好与凭证，由运行时选择 / CredentialStore 提供（v0.14.5）。YAML 只保留黑名单 exclude（连接可能性）与 process/http 的 target（框架级连接信息）。

---

## 七、未解决问题清单

> **已定稿项**标注 ✅；**仍待决策项**标注 ⏳。

### 问题 A：ProviderManager 的填充/注入机制 — ✅ 已定稿

**决策**：采用 HookManager 模式（A-1 变体）——**ProviderManager 本身是 Component**，嵌入 `BasicComponent`，`GetType()` 返回现有 `ComponentProvider` 类型（**无需新增 ComponentType**）。单个 provider 实例以 `IProvider` 对象通过 `Register()` 注册进 Manager，不进 Registry。`AgentRuntime.Dependencies()` 的 `"provider"` 依赖声明原样有效。

对比记录：


| 方案  | 描述                                                                                             | 结论   |
| --- | ---------------------------------------------------------------------------------------------- | ---- |
| A-1 | ProviderManager 作为独立 Component（新增 `ComponentProviderManager` 类型），Provider 声明依赖它，Initialize 时注册 | ⚠️ 变体采用：Manager 自身即 Component，复用 `ComponentProvider` 类型 |
| A-2 | Builder 注入：`ProviderComponent.SetManager(pm)`                                                  | ❌ 否决 |
| A-3 | 全局注册（`database/sql` 风格）                                                                        | ❌ 否决（全局状态影响测试隔离、多 App 实例冲突） |

### 问题 B：凭证存储的控制权 — ✅ 已定稿（B-2 变体：app 作用域文件）

**决策**：采用 **B-2 变体**——`CredentialStore` 接口 + 默认实现 `FileCredentialStore` 封装现有 `internal/credentials`（复用加载/解析/合并逻辑，**零新依赖**）。

**关键设计：app 作用域隔离**：

```
~/.gogent/
├── credentials.yaml                    ← daemon 级（现有，tool/sandbox 凭证，不动）
├── apps/
    └── <app-name>/
        └── credentials.yaml            ← app 级 provider 凭证（v0.14.5 新增）
```

- **不使用 daemon 的 `~/.gogent/credentials.yaml`**——避免多 agent 应用的 provider 凭证与 daemon 工具/沙箱凭证混淆
- 文件结构：**精确键**（`openai: "sk-..."`），不兼容大写命名
- `Get(providerName)` 精确键查找；`WithCredentialStore` 可注入自定义实现（Vault/keyring）
- 引擎 key 解析优先级：**CredentialStore > 构造时 env**（v0.14.2 行为向后兼容）
- 注入机制：`CredentialStoreAware` 可选接口，Builder 在 opts 应用后统一注入（无全局状态）

### 问题 C：ProcessProvider 的配置字段去留 — ⏳ 延后（v0.14.7 确认不动）

**决策（v0.14.1）**：`Config` 结构按完整字段定型，但 process/http 驱动保持现状（`ProcessProviderConfig` 仅取 target）。

**决策（v0.14.7）**：本阶段**不改造 ProcessProvider**——本地 native 路由与 process gRPC 路由是两条独立路径（远端 agent 有自己的 AgentCore）。C-b/C-c 改动大且无真实 process provider 用户场景，保持 ⏳ 延后。

```
C-a：这些字段对 process 驱动仍无意义（ProcessProvider 仅走 gRPC），忽略
C-b：ProcessProvider 同时支持 REST API 直连（成为"远程 REST Provider"的通道）
C-c：拆分为两个独立实现：ProcessProvider（gRPC）+ RemoteRESTProvider
```

### 问题 D：Provider 切换粒度 — ✅ 已定稿（D-c：会话级 + 消息级）

**决策**：

| 层 | 粒度 | 实现 |
|----|------|------|
| CLI chat | 会话级（D-a） | REPL 内 `currentProvider` + `currentModel` 状态，`/provider` `/model` 命令切换 |
| CLI run | 消息级（D-b） | `run -p "..." --provider <name> --model <name>` 单次指定 |
| HTTP（未来） | 消息级 | 每条请求 `Input.ProviderName` / `Input.ModelName`（HTTP iface deferred） |

**引擎侧 model 传递决策（v0.14.7 已定稿）**：采用 **ctx 携带**——`provider.WithProviderName` / `provider.WithModel` 写入 ctx，`ProviderManager.Generate/Stream` 从 ctx 读 provider 名路由到具体引擎，引擎内部读 ctx model 覆盖默认。IProvider 接口保持不变（不加 opts）。

### 问题 E：RouterProvider（聚合/容灾）是否纳入 v0.14.x — ✅ 已定稿（延后 v0.15.x）

**决策（v0.14.7）**：**不纳入 v0.14.x**。v0.14.7 仅实现单 provider 路由；RouterProvider（failover/负载均衡/按权重）作为独立复杂功能延后到 v0.15.x 单独设计。

```
E-a：纳入 —— 提供 failover / 负载均衡 / 按权重路由
E-b：延后到 v0.15.x —— 先做单实例原生 Provider 的多路注册
```

**✅ 采用 E-b。**（RouterProvider 是框架内部的透明容灾机制，不是与 OpenRouter SaaS 竞争）

### 问题 F：IProvider 接口是否微调 — ✅ 已定稿（扩展）

**决策**：`ModelInfo` 扩展 `DisplayName` + `Models` 字段：

```go
type ModelInfo struct {
    Name           string
    Provider       string    // 引擎名 "openai"
    DisplayName    string    // 品牌名 "OpenAI"（终端用户可见）
    ContextSize    int
    SupportsTool   bool
    SupportsVision bool
    Models         []string  // 可用模型列表（能力声明）
}
```

### 问题 G：DefaultProvider 的去留 — ✅ 已定稿（保留 + deprecated）

**决策**：保留 `DefaultProvider` 与 `ProviderComponent` 文件，标记 deprecated，Builder 不再使用（`WithProvider` 同步改为注册进 Manager）。后续确认无用户依赖后统一清理。

### 问题 H：Provider 配置机制（白名单 vs 黑名单） — ✅ 已定稿（黑名单）

**决策**：采用**黑名单机制**——框架默认提供所有 native 引擎，用户用 `exclude` 字段排除。YAML 只声明"连接可能性"，apiKey/model 全部移出（属凭证管理）。详见 3.4。

---

## 八、实施路线图

```
v0.14.1 — Provider 基础设施 ✅ 已完成
    ├── pkg/provider/errors.go                     哨兵错误（ErrUnknownEngine 等）
    ├── pkg/provider/models.go                     ModelInfo 扩展（DisplayName+Models）
    ├── pkg/provider/config.go                     引擎注册表（RegisterEngine/CreateEngine/RegisteredEngines）
    ├── pkg/provider/manager.go                    ProviderManager（HookManager 模板）+ ProviderInfo
    ├── pkg/provider/component.go                  标记 deprecated
    ├── pkg/app/builder.go                         buildProvider native 路径（ensureProviderManager + exclude 黑名单）
    ├── pkg/provider/manager_test.go               Register/Get/List/Unregister 单测（mock IProvider）
    ├── pkg/provider/config_test.go                引擎注册表 + exclude 过滤 + 未知引擎报错测试
    └── 已定稿：问题 A / F / G / H；问题 C 部分（process 保持现状）

v0.14.2 — OpenAI + OpenAI 兼容 ✅ 已完成
    ├── pkg/provider/openai.go                    OpenAIProvider（Generate/Stream/SSE/ToolCall）
    ├── pkg/provider/openai_compat.go             NewOpenAICompat 内部基类（不注册为独立引擎）
    ├── pkg/provider/deepseek.go                  DeepSeekProvider（OpenAI 兼容薄包装，注册进引擎表）
    ├── pkg/provider/errors.go                    ProviderError + ErrAPIKeyMissing
    └── pkg/provider/openai_test.go + deepseek_test.go

v0.14.3 — Gemini ⏳ 待实现
    ├── pkg/provider/gemini.go                    Gemini REST API（注册进引擎注册表）
    └── pkg/provider/gemini_test.go

v0.14.4 — Anthropic ⏳ 待实现
    ├── pkg/provider/anthropic.go                 Claude API（注册进引擎注册表）
    └── pkg/provider/anthropic_test.go

v0.14.5 — 凭证管理 ✅ 已完成
    ├── pkg/provider/credentials.go               CredentialStore 接口 + FileCredentialStore（app 作用域）
    ├── pkg/provider/openai.go + deepseek.go      引擎接入 CredentialStoreAware（store 优先，env fallback）
    ├── pkg/app/builder.go                        WithCredentialStore + 默认 store + injectCredentialStore
    └── 已定稿：问题 B（B-2 变体：app 作用域文件，精确键）

v0.14.6 — Interface 层多 Provider 交互 ✅ 已完成
    ├── pkg/agentcore/agent.go                     Input 增加 ProviderName + ModelName
    ├── pkg/iface/cli/cmd_provider.go              /provider + /model 命令（纯函数解析/校验/渲染）
    ├── pkg/iface/cli/cmd_chat.go                  REPL 集成，会话级 currentProvider + currentModel
    ├── pkg/iface/cli/cmd_run.go                   --provider + --model flags
    └── 已定稿：问题 D（D-c：会话级 + 消息级）；引擎侧 model 传递 v0.14.7 定

v0.14.7 — AgentCore 路由 + ReactAgent ✅ 已完成
    ├── pkg/provider/ctx.go                      WithProviderName/WithModel + From（ctx 携带）
    ├── pkg/provider/manager.go                  Generate/Stream dispatch（ctx 读 provider 名路由）
    ├── pkg/provider/openai.go                   引擎读 ctx model 覆盖默认
    ├── pkg/agentcore/registry.go                类型注册表（RegisterAgentType/CreateAgent）
    ├── pkg/agentcore/react.go                   ReactAgent（type: "react"，ctx 路由 + 单轮对话）
    ├── pkg/app/builder.go                       buildAgentCore native → CreateAgent(config.type 默认 react)
    └── 已定稿：问题 E（E-b 延后 v0.15.x）；问题 C（延后）；model 传递（ctx 方案）
    └── ⏳ 后续：ReAct 工具循环（tool_calls → 执行 → 迭代）

⏳ 待补：groq / mistral / ollama / openrouter 等其他 OpenAI 兼容供应商引擎
    （各为薄包装：独立 env key + 内置 baseURL + ModelInfo，模式参考 deepseek.go）
```

---

## 九、参考资源

- 行业模式：[gh CLI](https://github.com/cli/cli)、[Claude Code](https://github.com/anthropics/claude-code)、[AWS CLI](https://aws.amazon.com/cli/) — 凭证管理（OS keyring 优先）
- Go 凭证库：[zalando/go-keyring](https://github.com/zalando/go-keyring)、[byteness/keyring](https://github.com/byteness/keyring)
- Go 多 Provider LLM 框架：lingo、omnillm-core、rho-llm、llms-go、dracory/llm — 统一接口 + 注册表模式
- 项目现有凭证：[`internal/credentials/`](../internal/credentials/) — credentials.yaml + ${VAR} 解析 + fsnotify
- 项目现有架构：[`ARCHITECTURE.md`](../ARCHITECTURE.md)

