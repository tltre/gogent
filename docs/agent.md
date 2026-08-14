# Agent Core 设计文档（v0.15.x）

> 本文档描述 gogent 框架 React Agent Core 的完整 ReAct 范式设计，涵盖现状分析、架构调整、协议扩展、上下文管理与 ReAct 循环设计。
> 状态：**设计中（v0.15.x）** — 待决策点见第六章，版本规划见第八章
> 关联版本：v0.14.7（当前基线）

---

## 一、背景与目标

### 1.1 目标

**v0.15.x 的目标是打通 React Agent Core 的全流程**——从 v0.14.7 的"单轮问答"扩展为完整的 **ReAct 范式**（Reasoning → Acting → Observing → 迭代）：

| | v0.14.7（现状） | v0.15.x（目标） |
|---|---|---|
| 行为 | 单轮问答（Generate → 返回） | ReAct 循环：推理 → 工具调用 → 结果观察 → 再推理 |
| 工具 | 不涉及 | 声明工具给模型 → 执行 tool_calls → 结果回填 → 迭代 |
| 消息 | Input 一次性传入 | 循环内维护多轮消息（assistant tool_calls + tool 结果） |

### 1.2 术语

| 术语 | 含义 |
|------|------|
| **IAgentCore** | agent 逻辑接口（Run/Stream/SetAgentRuntime），开发者可替换 |
| **AgentRuntime** | IAgentCore 的组件包装（AgentCore Component），持有框架注入的资源 |
| **ReactAgent** | `type: "react"` 的默认 IAgentCore 实现（v0.14.7 起） |
| **ReAct 循环** | 模型推理 → 工具调用 → 执行 → 结果回填 → 再推理的迭代过程 |
| **ToolManager** | 框架管控的薄 gRPC 客户端（daemon 工具执行代理，v0.12.2 起） |
| **tool.Service** | 暴露给 agent core 的工具调用面（List/Execute 接口） |

---

## 二、架构基础：ToolManager 的定位（v0.12.2 决策回顾）

### 2.1 为什么 ToolManager 在 App 级（现状）

v0.12.2（提交 `ec0c8ab`）将 ToolManager 从 registry 组件改为**薄 gRPC 客户端**，由 App 持有：

```go
// pkg/app/app.go
type App struct {
    ...
    toolManager *tool.ToolManager   // v0.12.2
}
```

**原因（四层）**：

1. **工具来源收敛为单一——daemon**：工具（尤其 MCP Server 子工具）的注册、启动、健康检查全部在 daemon 进程。App 侧无本地工具注册（`// v0.12.2: No local ITool registration. Daemon is the only tool source.`）
2. **MCP 生态本质**：工具属于 MCP Server，Server 生命周期（fork 子进程、Ping 探活）由 daemon 的 McpRunner 管理，App 进程内不管理外部进程
3. **多 agent 共享 + 声明式使用**：多 App 共享 daemon 工具，App 通过 manifest 声明使用意图（`Config.Tools`），daemon 校验展开
4. **执行管线在 daemon 侧**：ExecuteTool 管线（Auth → Hook:pre → Run → Hook:post → Result）在 daemon 执行，App 侧 ToolManager 只是双向 gRPC 流转发代理

**生命周期**由 `App.Run()` 管理（app.go:117-127）：
```
a.Initialize(ctx)          // 组件初始化
a.Start(ctx)               // 组件启动
a.toolManager.Start(ctx)   // ToolManager 连接 daemon gRPC + 注册 manifest
...
a.iface.Run(ctx, a.registry)  // agent 执行（此时 tm 已连接）
defer a.toolManager.Stop(ctx) // 清理
```

### 2.2 结论：App 是生命周期所有者

```
App → toolManager 生命周期（Start/Stop/连接 daemon）——框架独占
```

---

## 三、核心架构设计（v0.15.x）

### 3.1 AgentRuntime 通用注入（已确认）

ToolManager 是**所有 agent 范式**（react、未来其他类型）都需要的工具调用能力，因此注入到 **AgentRuntime**（通用路径），而非为 ReactAgent 开特殊注入接口：

```go
// pkg/agentcore/component.go
type AgentRuntime struct {
    component.BasicComponent
    Agent       IAgentCore
    toolService tool.Service   // v0.15.x: 框架管控薄客户端的 agent 可见面
}
```

- 任何 IAgentCore 通过 `runtime.ToolService()` 访问（零特殊接口）
- 与 `Reg()` 暴露 Registry 的既有哲学一致（AgentRuntime = agent 环境）

### 3.2 tool.Service 接口包裹（已确认）

为防止自定义 agentCore 误调 ToolManager 生命周期方法（Start/Stop/dial），AgentRuntime **只注入接口**，不注入具体类型：

```go
// pkg/tool/service.go — 新增
// Service is the tool-calling surface exposed to agent cores. It deliberately
// omits lifecycle methods (Start/Stop/dial) so custom IAgentCore implementations
// cannot tamper with daemon connectivity — the framework (App) owns lifecycle.
type Service interface {
    List() []ToolInfo                                       // 工具声明（ReAct 首轮注入给模型）
    Execute(ctx context.Context, name string, params map[string]any) (Result, error)
}

// ToolManager 已实现这两个方法（manager.go:126/177）
var _ Service = (*ToolManager)(nil)
```

**接口隔离的价值**：

| 维度 | 注入 ToolManager（完整） | 注入 tool.Service（接口） |
|------|------------------------|--------------------------|
| agent 可见面 | 含生命周期/连接方法 | 仅 List/Execute |
| 误用风险 | 可误调 Start/Stop | 物理上不可能 |
| 可测试性 | 需真实 ToolManager | 可注入 mock Service |
| 依赖方向 | agentcore → 具体类型 | agentcore → 小接口 |

### 3.3 职责矩阵（已确认）

```
App            → ToolManager 生命周期（Start/Stop/连接 daemon）——框架独占
tool.Service   → List/Execute —— agent 可见面
AgentRuntime   → 持 tool.Service 引用，提供 ToolService() getter
自定义 agentCore → 只能 List/Execute，碰不到生命周期
```

**关键**：AgentRuntime **绝不调用** tm.Start/Stop——生命周期是 App 的职责。同一实例，两个持有者（App 管生命周期、AgentRuntime 管访问），类比 Registry（App.Registry 持有 + 组件 BasicComponent.Registry 持有）。

### 3.4 注入方式：setter（已确认，与项目既有模式一致）

| 注入目标 | 方式 | 先例 |
|----------|------|------|
| **Registry** | `Initialize(ctx, registry)` 参数 | 组件系统固有机制（生命周期钩子携带） |
| **CredentialStore** | `SetCredentialStore(s)` | `CredentialStoreAware`（builder.go:500） |
| **ToolManager 配置** | `SetManifest`/`SetHookManager`/`SetSandboxConfig` | builder.go:135/157/151 |
| **CLI 凭证** | `c.SetCredentialStore(s)` | builder.go:512 |
| **AgentRuntime 工具** | `SetToolService(s)`（v0.15.x） | 同上模式 |

**为什么不能用 Initialize 参数**：
1. ToolManager 不是 registry 组件（v0.12.2 决策），`Initialize(ctx, registry)` 只有 registry 参数，无法携带
2. 时序不可行：`tm` 在 `Build()` 尾部创建，`AgentRuntime.Initialize` 在 `registry.InitializeAll`（`App.Initialize`）执行——**tm 创建晚于 Initialize**
3. setter 注入是项目"非组件资源注入"的既有统一模式

**Builder 接入**（Build() 尾部，tm 创建后）：

```go
tm := tool.NewToolManager(b.config.Name)
...
if rt, ok := b.registry.GetDefault(component.ComponentAgentCore).(*agentcore.AgentRuntime); ok {
    rt.SetToolService(tm)   // 类型自动收窄为 tool.Service
}
```

---

## 四、ProviderMessage 协议扩展（阻塞项）

### 4.1 缺口

当前 `ProviderMessage` 只有 `Role/Content/Tools`，**无法表达** OpenAI 多轮工具调用的两种消息形态：

```json
// assistant 回传（模型上一轮的 tool_calls）
{"role":"assistant","content":"","tool_calls":[
  {"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]}
// tool 结果（工具执行后的观察）
{"role":"tool","content":"20°C","tool_call_id":"call_1"}
```

### 4.2 扩展（已确认方向）

```go
// pkg/provider/provider.go
type ProviderMessage struct {
    Role       string
    Content    string
    Tools      []ToolDefinition
    // v0.15.0 新增：
    ToolCalls  []ToolCall   // assistant 消息回传（OpenAI tool_calls 字段）
    ToolCallID string       // tool 结果消息关联（OpenAI tool_call_id 字段）
}
```

### 4.3 引擎同步（openai.go）

- `chatMessage` 加 `ToolCalls []chatToolCall` 字段（json 序列化）
- `buildChatRequest` 映射两个新字段（`ToolCall.Args map` → `arguments` JSON 字符串）
- DeepSeek 自动继承（共享实现，v0.14.2 起）

---

## 五、ReactAgent ReAct 循环

### 5.1 流程

```
Run(input):
 ① 工具声明：runtime.ToolService().List() → []provider.ToolDefinition（附加到首轮消息）
 ② 初始 messages = input.Messages
 ③ 循环（maxIterations 上限，默认 10）：
    a. 触发 hook EventBeforeLLM
    b. provider.Generate(messages) → response
    c. 触发 hook EventAfterLLM
    d. 若 response.ToolCalls 为空 → 返回最终 Output
    e. 追加 assistant 消息（含 ToolCalls）→ messages
    f. 遍历 ToolCalls：
       - 触发 hook EventBeforeTool
       - toolService.Execute(name, args) → Result
       - 触发 hook EventAfterTool
       - 追加 tool 结果消息（Role:"tool", ToolCallID 关联）→ messages
    g. → 回到 a
 ④ 超 maxIterations → 返回部分结果（Metadata 标记 maxIterationsReached）
```

- **Output.Actions** 累积每次工具调用（ToolName/Params/Result）——复用现有 `Action` 结构
- **工具执行错误**：错误作为 tool 结果回填（模型可自纠正继续），不中断循环

### 5.2 停止条件

| 条件 | 行为 |
|------|------|
| 无 ToolCalls（finish） | 正常返回 |
| maxIterations（默认 10） | 返回部分结果 + `maxIterationsReached` metadata |
| ctx 取消 | 返回错误 |
| 工具执行出错 | 错误回填，模型自纠正继续 |

### 5.3 事件/钩子

- `EventBeforeLLM`/`EventAfterLLM`：每次 Generate 前后
- `EventBeforeTool`/`EventAfterTool`：每次工具执行前后
- 通过 AgentRuntime 的 HookManager（`AgentRuntime.Dependencies()` 已有 HookManager 可选依赖）

### 5.4 跨轮次上下文与记忆（v0.15.x 补充，已确认）

ReAct 循环只解决"单次 Run 内的工具迭代"；**跨轮次的历史消息**与**长期记忆**需打通 ContextManager 与 Memory。

**核心架构决策：ContextManager 是 agent 的唯一输入来源**——agent 不直接碰历史/记忆，由 ContextManager 组装完整输入。

```
ContextManager（唯一输入构造者）
  ├── 持有 Memory 引用（依赖边声明 + Registry 获取，见 C1）
  ├── BuildInput(sessionID, userMsgs)：
  │     ① memory.Query(userMsgs) → 相关记忆
  │     ② 构造系统提示（含记忆注入）
  │     ③ GetMessages(sessionID) → 历史
  │     ④ 返回完整输入（系统 + 记忆 + 历史 + 用户消息）
  └── AddMessage → 持久化每轮 user/assistant 历史

Agent（ReactAgent）——只管推理 + 工具编排
  ├── input = cm.BuildInput(sessionID, msgs)   ← 唯一输入来源
  ├── ReAct 循环（provider + toolService）
  ├── 循环内 tool 交互 → memory.Add（存档，非对话历史）
  └── cm.AddMessage(user + 最终 assistant)     ← 历史持久化
```

**消息流边界**：
- **进 CM 历史**：每轮的 user 消息 + 最终 assistant 回复（对话上下文）
- **进 Memory 存档**：ReAct 循环内的 tool 调用/结果（过程量存档，跨会话可 recall 工具调用经验，不污染对话历史）

**ContextManager 默认实现**（C1）：

```go
// pkg/contextmanager/default.go — 新增
type DefaultContextManager struct {
    component.BasicComponent
    sessions  map[string][]ContextMessage
    summaries map[string]Summary
    counter   int64   // sessionID 生成
}

// Dependencies 声明 Memory 依赖（依赖边）
func (c *DefaultContextManager) Dependencies() map[string]component.DependencySpec {
    return map[string]component.DependencySpec{
        "Memory": {Type: component.ComponentMemory, Required: false},
    }
}

// Initialize 持有 registry，内部通过 Registry 获取 memory
func (c *DefaultContextManager) Initialize(ctx, registry) error {
    c.SetRegistry(registry)
    return nil
}

func (c *DefaultContextManager) memory() memory.IMemory {
    comp := c.Registry().GetDefault(component.ComponentMemory)
    if m, ok := comp.(*memory.MemoryComponent); ok {
        return m
    }
    return nil
}
```

**依赖获取方式**（符合整体设计）：`DefaultContextManager.Dependencies()` 声明 Memory 依赖（拓扑排序保证先初始化），`Initialize` 持有 registry 引用，通过 `Registry().GetDefault(ComponentMemory)` 获取——与 ProviderManager 通过 `AgentRuntime.Reg()` 获取的模式一致。

**ContextManager 接口扩展**：

```go
// pkg/contextmanager/context.go — 新增
// BuildInput assembles the complete model input for an agent run:
// recalled memory + system prompt + session history + user messages.
// The agent treats ContextManager as its single input source.
BuildInput(sessionId string, messages []ContextMessage) []ContextMessage
```

返回 `[]ContextMessage`（CM 自有类型，agent 侧转换）——保持包依赖单向（agentcore → contextmanager）。

**默认链**：`DefaultMemory`（已有）→ `DefaultContextManager`（新增，依赖边声明 Memory）→ 均注册进 componentDefaults。

---

## 六、决策点（全部已确认）

| # | 决策点 | 决策 | 状态 |
|---|--------|------|------|
| D1 | **Stream 是否支持 ReAct** | 先只做 `Run`（非流式）完整 ReAct；`Stream` 保持单轮（工具调用过程由 daemon 侧日志呈现） | ✅ 已确认 |
| D2 | **系统提示来源** | 由 ContextManager.BuildInput 构造（含记忆注入），非 agent 内置 | ✅ 已确认（见 5.4） |
| D3 | **maxIterations 默认值** | **硬编码 10**；后续版本评估是否加入 config 项 | ✅ 已确认 |
| D4 | **工具错误处理** | 错误作为 tool 结果回填，模型自纠正继续；**计入迭代次数**（防反复失败刷迭代），超限终止并返回最后错误 | ✅ 已确认 |
| D5 | **工具结果大小** | 截断 8KB 回填（头部 4KB + 尾部 4KB，中间 `...[truncated]...`） | ✅ 已确认 |
| D6 | **Input 是否扩展** | 加 `SessionID string`（跨轮次会话标识，CLI chat 维护） | ✅ 已确认 |
| D7 | **工具调用流式执行** | 非流式（ToolManager.Execute 阻塞式，ReAct"等结果再推理"语义天然匹配）；流式执行后续版本 | ✅ 已确认 |

### C 系列：上下文管理与记忆（已确认）

| # | 决策点 | 决策 |
|---|--------|------|
| C1 | ContextManager default | in-memory（进程内会话）+ **Dependencies 声明 Memory 依赖，Initialize 持 registry，通过 `Registry().GetDefault(ComponentMemory)` 获取**（符合整体设计） |
| C2 | Memory 实现 | 复用现有 `DefaultMemory`（已注册 componentDefaults），无需新做 |
| C3 | Input.SessionID | 新增字段（跨轮次必要），CLI chat 会话启动时 `cm.NewSession()` 并传递 |
| C4 | CM 唯一输入源 | ✅ `BuildInput(sessionID, msgs)` 组装历史+记忆+系统提示，agent 只从 CM 拿输入 |
| C5 | 循环内 tool 消息 | 进 **Memory 存档**（`memory.Add` tool 交互条目），**不进 CM 历史**（不污染对话上下文） |
| C6 | BuildInput 返回类型 | `[]ContextMessage`（CM 自有类型，agent 转换；包依赖单向 agentcore → contextmanager） |
| C7 | 记忆学习 | 简单版只存 tool 交互存档；对话摘要入 memory 后续版本 |

---

## 七、影响面

| 文件 | 改动 |
|------|------|
| `pkg/provider/provider.go` | ProviderMessage + `ToolCalls`/`ToolCallID` |
| `pkg/provider/openai.go` | chatMessage/buildChatRequest 映射 |
| `pkg/tool/service.go` | **新增**：Service 接口 + `var _ Service = (*ToolManager)(nil)` |
| `pkg/agentcore/component.go` | AgentRuntime + `toolService` 字段 + `SetToolService`/`ToolService()` |
| `pkg/agentcore/agent.go` | Input + `SessionID` 字段（D6 修订） |
| `pkg/agentcore/react.go` | ReAct 循环 + `BuildInput` 集成 + 工具编排 + tool 存档入 Memory |
| `pkg/contextmanager/default.go` | **新增**：DefaultContextManager（in-memory + Dependencies 声明 Memory + Registry 获取） |
| `pkg/contextmanager/context.go` | IContextManager + `BuildInput` 方法 |
| `pkg/contextmanager/component.go` | Dependencies 声明 Memory 依赖 |
| `pkg/iface/cli/cmd_chat.go` | 会话维护：启动 `cm.NewSession()`，消息携带 `SessionID` |
| `pkg/app/builder.go` | Build 尾部 `rt.SetToolService(tm)` + componentDefaults 注册 ContextManager |
| 测试 | ProviderMessage wire、ReAct 循环（mock provider + mock Service）、上下文组装（BuildInput）、错误回填、maxIterations、端到端多轮 |

---

## 八、版本规划（分小版本迭代）

> 按依赖顺序拆分，每个小版本独立可验证、可合并。待决策点（D1/D3/D4/D5/D7）在对应版本实现前确认。

### v0.15.1 — 协议与注入基础设施

**目标**：打通"agent 能声明工具 + 能执行工具"的底层协议与注入面。

```
├── pkg/provider/provider.go              ProviderMessage + ToolCalls/ToolCallID
├── pkg/provider/openai.go                chatMessage/buildChatRequest 映射（tool_calls 回传 + tool_call_id）
├── pkg/tool/service.go                   【新增】Service 接口（List/Execute）+ var _ Service = (*ToolManager)(nil)
├── pkg/agentcore/component.go            AgentRuntime + toolService 字段 + SetToolService/ToolService()
├── pkg/app/builder.go                    Build 尾部 rt.SetToolService(tm)
└── 测试                                  ProviderMessage wire 格式、Service 接口收窄、注入生效
```

**验证**：`go build` + `go vet` + provider/tool/agentcore/app 单测；现有测试无回归（ProviderMessage 加字段向后兼容）。

### v0.15.2 — 上下文管理打通

**目标**：ContextManager 成为 agent 唯一输入源；历史 + 记忆可组装。

```
├── pkg/contextmanager/default.go         【新增】DefaultContextManager（in-memory + Dependencies 声明 Memory + Registry 获取）
├── pkg/contextmanager/context.go         IContextManager + BuildInput(sessionID, msgs) 方法
├── pkg/contextmanager/component.go       Dependencies 声明 Memory 依赖
├── pkg/agentcore/agent.go                Input + SessionID 字段（D6 已确认）
├── pkg/app/builder.go                    componentDefaults 注册 contextmanager default
└── 测试                                  BuildInput 组装（记忆注入 + 历史 + 用户消息）、Memory 依赖解析
```

**验证**：contextmanager/app 单测；默认链（DefaultMemory → DefaultContextManager）装配正确。

### v0.15.3 — ReactAgent ReAct 循环核心

**目标**：单轮问答 → 完整 ReAct 循环（推理 → 工具调用 → 观察 → 迭代）。

```
├── pkg/agentcore/react.go                ReAct 循环（工具声明注入 / 迭代 / 停止条件 / 错误回填 / 结果截断）
│                                          + BuildInput 集成（CM 唯一输入源）+ cm.AddMessage 持久化
│                                          + 循环内 tool 交互 → memory.Add 存档（C5）
└── 测试                                  mock provider + mock Service 多轮循环、错误回填、maxIterations、上下文持久化
```

**前置确认**：D3（maxIterations=10）、D4（错误回填）、D5（结果截断 8KB）。

**验证**：ReAct 单测全绿；端到端 mock LLM server 多轮工具调用。

### v0.15.4 — CLI 会话 + 端到端验证

**目标**：用户交互层跑通完整流程（跨轮次对话 + 工具调用）。

```
├── pkg/iface/cli/cmd_chat.go             会话维护：启动 cm.NewSession()，消息携带 SessionID
├── 端到端测试                             多轮对话（历史累积）+ 工具调用（ReAct）+ 记忆存档
└── 手动验证脚本                           更新 verify 脚本：chat 多轮 + /tools 场景
```

**前置确认**：D1（Stream 保持单轮）、D7（非流式工具执行）。

**验证**：CLI chat 连续对话历史正确累积；工具调用回填正确；层 3 手动脚本跑通。

---

## 九、参考

- 工具生态架构：[`docs/tool.md`](./tool.md) — ToolManager/daemon 工具管线（v0.12.8）
- Provider 模块设计：[`docs/provider.md`](./provider.md) — ProviderManager/凭证/路由（v0.14.x）
- v0.12.2 决策提交：`ec0c8ab` — manifest registration, thin ToolManager, Builder cleanup
- AgentRuntime 组件：`pkg/agentcore/component.go`（Reg() 暴露 Registry 的既有设计）
