# Provider 模块设计方案（v0.14.x）

> 本文档描述 gogent 框架 Provider 模块的多供应商支持设计，涵盖现状分析、目标架构、路线选择、凭证管理与待决策问题。
> 状态：**设计中（v0.14.x 规划）**
> 关联版本：v0.13.x（当前基线）

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
│ YAML 配置（App 开发者编写 — 声明"支持哪些供应商"）              │
│  provider-openai   provider-gemini   provider-claude         │
│  provider-openrouter  ...                                    │
└──────────────────────┬──────────────────────────────────────┘
                       │ Builder 解析
                       ▼
┌─────────────────────────────────────────────────────────────┐
│ Registry                                                    │
│  ComponentProvider: [openai, gemini, claude, ...]           │
│  ComponentProviderManager: [manager]  (新增，待定方案)       │
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

### 3.2 ProviderManager 定位

```
ProviderManager = 所有 Provider 实例的统一查询目录

职责边界：
  ✅ 管理 Provider 的注册 / 注销 / 发现
  ✅ 为 Interface 层提供 ProviderInfo 查询（展示给终端用户）
  ✅ 为 AgentCore 提供按名选取
  ✅ 可选：统一健康检查、模型索引
  ❌ 不包装 Generate()/Stream() 调用（那是 AgentCore 的职责）
  ❌ 不做请求/响应格式转换（那是各 Provider 实现的职责）
  ❌ 不管理凭证（CredentialStore 是独立抽象）
```

### 3.3 预期接口设计（草案）

```go
// ProviderManager 草案
type ProviderManager struct {
    providers map[string]IProvider
}

func (pm *ProviderManager) Register(name string, p IProvider)
func (pm *ProviderManager) Unregister(name string)
func (pm *ProviderManager) Get(name string) IProvider
func (pm *ProviderManager) List() []ProviderInfo      // Interface 层展示
func (pm *ProviderManager) Health(name string) error  // 可选

type ProviderInfo struct {
    Name        string   // "provider-openai"
    Engine      string   // "openai"
    DisplayName string   // "OpenAI" — 终端用户可见
    Models      []string // ["gpt-4o", "gpt-4o-mini"]
}
```

---

## 四、两条路线对比（待决策）

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


**推荐方向：路线 2（统一管理）**，理由：单入口、支持 Router 扩展、配置一致，符合框架"可插拔可替换"的核心哲学。最终决策待问题清单确认后定稿。

---

## 五、Provider 实现策略

### 5.1 代码布局（草案）

```
pkg/provider/
├── provider.go              # IProvider 接口 + 共享类型（不变）
├── component.go             # ProviderComponent 封装（微调）
├── process.go               # ProcessProvider（不变或适配）
├── default.go               # DefaultProvider（保留向后兼容或移除）
├── manager.go               # ProviderManager（新增）
├── config.go                # 统一配置模型 + ProviderFactory（新增）
├── errors.go                # 统一错误类型（新增）
├── models.go                # 模型信息注册表（新增）
├── openai.go                # OpenAIProvider（新增）
├── openai_compat.go         # OpenAICompatibleProvider（复用 OpenAI 核心）
├── gemini.go                # GeminiProvider（新增）
├── anthropic.go             # AnthropicProvider（新增）
├── credentials.go           # CredentialStore 接口 + 默认实现（新增）
└── *_test.go                # 各 Provider 单元测试（mock HTTP）
```

### 5.2 引擎与依赖


| 引擎            | 依赖                   | 说明                                                         |
| ------------- | -------------------- | ---------------------------------------------------------- |
| openai        | 无（标准 net/http）       | 市场覆盖率最高                                                    |
| openai-compat | 无                    | 复用 OpenAI 逻辑，仅替换 baseURL，覆盖 DeepSeek/Groq/Mistral/Ollama 等 |
| gemini        | 无（REST API，不走官方 SDK） | 减少依赖树，统一错误处理                                               |
| anthropic     | 无                    | 标准 net/http                                                |
| openrouter    | 无                    | 本质是 openai-compat，自定义 endpoint                             |
| process       | 现有 gRPC 依赖           | 保持现状                                                       |


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

### 6.2 与现有 internal/credentials/ 的关系

项目已有 `~/.gogent/credentials.yaml`（0600）+ `Resolver`（${VAR} 解析 + fsnotify 热加载），被 tool/sandbox 共用。

**设计取舍**：

```
方案 X：直接复用 internal/credentials（credentials.yaml）
  ✅ 零新增依赖，与现有子系统一致
  ❌ 明文 YAML（0600 权限），无加密；不满足 Tier 1 最佳实践
  ❌ internal/ 包无法被 pkg/ 下公共 API 引用（Go internal 规则）

方案 Y：新建 CredentialStore 抽象，默认实现走 keyring
  ✅ 符合行业最佳实践（Tier 1 优先）
  ✅ 可注入自定义实现（企业 Vault 等）
  ❌ 需引入第三方 keyring 依赖（zalando/go-keyring 或 byteness/keyring）

方案 Z：CredentialStore 抽象 + 默认实现封装现有 credentials.yaml
  ✅ 复用现有基础设施，无新依赖
  ✅ 提供注入点，后续可替换为 keyring 实现
  ❌ 默认安全性弱于 keyring（但 0600 + 明文与项目现状一致）
```

### 6.3 CredentialStore 接口草案

```go
type CredentialStore interface {
    Get(providerName string) (apiKey string, err error)
    Set(providerName, apiKey string) error
    Delete(providerName string) error
    List() ([]string, error)  // 已配置凭证的 provider 列表
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
| 支持哪些供应商          | YAML 配置（app 仓库）                             | App 开发者      |
| API Key 等密钥      | CredentialStore（keyring / credentials.yaml） | 终端用户（首次运行引导） |
| 默认模型名 / endpoint | YAML 配置                                     | App 开发者      |


**必须允许 App 开发者自定义凭证管理行为**——通过 `WithCredentialStore(BuildOption)` 注入，框架提供默认实现。

---

## 七、未解决问题清单（待决策）

### 问题 A：ProviderManager 的填充/注入机制


| 方案  | 描述                                                                                             | 优点                                       | 缺点                                                   |
| --- | ---------------------------------------------------------------------------------------------- | ---------------------------------------- | ---------------------------------------------------- |
| A-1 | ProviderManager 作为独立 Component（新增 `ComponentProviderManager` 类型），Provider 声明依赖它，Initialize 时注册 | 依赖关系清晰、拓扑排序保证顺序、Interface 可从 Registry 获取 | 需改 component.go、改动最大                                 |
| A-2 | Builder 注入：`ProviderComponent.SetManager(pm)`                                                  | 改动最小、Builder 已有很多类似模式                    | ProviderComponent 多一个公开方法、Interface 获取 Manager 需额外路径 |
| A-3 | 全局注册（`database/sql` 风格）                                                                        | 极简                                       | 全局状态影响测试隔离、多 App 实例冲突                                |


**推荐倾向**：A-1 最符合框架"组件化 + 依赖声明"哲学；A-2 实现成本最低。**待决策。**

### 问题 B：凭证存储的控制权


| 方案  | 描述                                                            |
| --- | ------------------------------------------------------------- |
| B-1 | 框架内置 OS keyring + 加密文件 fallback，默认启用（符合行业最佳实践，需引入依赖）          |
| B-2 | 框架提供 CredentialStore 接口，默认实现封装现有 `internal/credentials`（零新依赖） |
| B-3 | 混合：默认 OS keyring，但提供 `WithCredentialStore` 覆盖（推荐，兼顾安全与可扩展）    |


**待决策。**

### 问题 C：ProcessProvider 的配置字段去留

当前 `ProcessProviderConfig` 忽略了 YAML `config.endpoint/apiKey/model`。走路线 2 后：

```
C-a：这些字段对 process 驱动仍无意义（ProcessProvider 仅走 gRPC），忽略
C-b：ProcessProvider 同时支持 REST API 直连（成为"远程 REST Provider"的通道）
C-c：拆分为两个独立实现：ProcessProvider（gRPC）+ RemoteRESTProvider
```

**待决策。**

### 问题 D：Provider 切换粒度

```
D-a：会话级切换（CLI 会话状态，简单）
D-b：逐条消息切换（HTTP API 每条请求带 provider 字段，灵活）
D-c：两者都支持（会话级默认 + 消息级覆盖）
```

**待决策。**

### 问题 E：RouterProvider（聚合/容灾）是否纳入 v0.14.x

```
E-a：纳入 —— 提供 failover / 负载均衡 / 按权重路由
E-b：延后到 v0.15.x —— 先做单实例原生 Provider 的多路注册
```

**待决策。**（注意：RouterProvider 是框架内部的透明容灾机制，不是与 OpenRouter SaaS 竞争）

### 问题 F：IProvider 接口是否微调

现状 `ModelInfo()` 返回 `ModelInfo{Name, Provider, ContextSize, SupportsTool, SupportsVision}`。是否需要扩展：

```go
type ModelInfo struct {
    Name           string
    Provider       string    // 引擎名 "openai"
    DisplayName    string    // 品牌名 "OpenAI"（终端用户可见）
    ContextSize    int
    SupportsTool   bool
    SupportsVision bool
    Models         []string  // 可用模型列表
}
```

**待决策。**

### 问题 G：DefaultProvider 的去留

现状 `DefaultProvider`（函数回调注入的占位）：

- 保留：向后兼容（现有用户可能通过 `SetGenerate/SetStream/SetModelInfo` 注入）
- 移除：无真实用户场景，回归到 `TODO: default provider` 语义

**待决策。**

---

## 八、实施路线图（草案）

```
v0.14.1 — Provider 基础设施
    ├── pkg/provider/{errors,config,models}.go    共享类型
    ├── pkg/provider/manager.go                   ProviderManager 骨架
    ├── pkg/app/builder.go                        新增 native driver engine 分发
    └── 解决 问题 A（注入机制）

v0.14.2 — OpenAI + OpenAI 兼容
    ├── pkg/provider/openai.go                    OpenAI + OpenAICompatible
    └── pkg/provider/openai_test.go

v0.14.3 — Gemini
    ├── pkg/provider/gemini.go                    Gemini REST API
    └── pkg/provider/gemini_test.go

v0.14.4 — Anthropic
    ├── pkg/provider/anthropic.go                 Claude API
    └── pkg/provider/anthropic_test.go

v0.14.5 — 凭证管理
    ├── pkg/provider/credentials.go               CredentialStore 接口 + 默认实现
    └── 解决 问题 B（凭证方案）

v0.14.6 — Interface 层多 Provider 交互
    ├── pkg/iface/cli/cmd_chat.go                 /provider 命令
    ├── pkg/iface/cli/cmd_run.go                  --provider flag
    └── pkg/iface/cli/cmd_provider.go             新命令实现

v0.14.7 — AgentCore 路由 + 文档
    ├── pkg/agentcore/agent.go                    多 Provider 路由
    ├── pkg/agentcore/component.go                Dependencies 标记 Multiple=true
    └── tests/provider/                           端到端集成测试
```

---

## 九、参考资源

- 行业模式：[gh CLI](https://github.com/cli/cli)、[Claude Code](https://github.com/anthropics/claude-code)、[AWS CLI](https://aws.amazon.com/cli/) — 凭证管理（OS keyring 优先）
- Go 凭证库：[zalando/go-keyring](https://github.com/zalando/go-keyring)、[byteness/keyring](https://github.com/byteness/keyring)
- Go 多 Provider LLM 框架：lingo、omnillm-core、rho-llm、llms-go、dracory/llm — 统一接口 + 注册表模式
- 项目现有凭证：[`internal/credentials/`](../internal/credentials/) — credentials.yaml + ${VAR} 解析 + fsnotify
- 项目现有架构：[`ARCHITECTURE.md`](../ARCHITECTURE.md)

