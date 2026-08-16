# Agent Core 设计

> **文档状态**：本文档记录 Gogent 在 v0.14-v0.15 的架构演进与决策过程，包括方案对比、取舍理由与后续演进方向。代码与本文档如有出入，以代码为准。

本文档描述 React Agent Core 的 ReAct 范式设计：ToolManager 定位、tool.Service 接口隔离、ProviderMessage 协议扩展、ReAct 循环与上下文/记忆架构。

## 一、背景与目标

v0.14.7 的 ReactAgent 是**单轮问答**（Generate → 返回）。v0.15.x 将其扩展为完整的 **ReAct 范式**（Reasoning → Acting → Observing → 迭代）：

| | 单轮问答（v0.14.7 基线） | ReAct 循环（v0.15 目标） |
|---|---|---|
| 行为 | 单轮问答 | 推理 → 工具调用 → 结果观察 → 再推理 |
| 工具 | 不涉及 | 声明工具给模型 → 执行 tool_calls → 结果回填 → 迭代 |
| 消息 | Input 一次性传入 | 循环内维护多轮消息（assistant tool_calls + tool 结果） |

## 二、术语

| 术语 | 含义 |
|------|------|
| **IAgentCore** | agent 逻辑接口（Run/Stream/SetAgentRuntime），开发者可替换 |
| **AgentRuntime** | IAgentCore 的组件包装（AgentCore Component），持有框架注入的资源 |
| **ReactAgent** | `type: "react"` 的默认 IAgentCore 实现 |
| **ReAct 循环** | 模型推理 → 工具调用 → 执行 → 结果回填 → 再推理的迭代过程 |
| **ToolManager** | 框架管控的薄 gRPC 客户端（daemon 工具执行代理） |
| **tool.Service** | 暴露给 agent core 的工具调用面（List/Execute 接口） |

## 三、ToolManager 定位

### 3.1 为什么 ToolManager 在 App 级

v0.12.2 将 ToolManager 从 registry 组件改为**薄 gRPC 客户端**，由 App 持有。原因（四层）：

1. **工具来源收敛为单一——daemon**：工具（尤其 MCP Server 子工具）的注册、启动、健康检查全部在 daemon 进程。App 侧无本地工具注册
2. **MCP 生态本质**：工具属于 MCP Server，Server 生命周期（fork 子进程、Ping 探活）由 daemon 的 McpRunner 管理，App 进程内不管理外部进程
3. **多 agent 共享 + 声明式使用**：多 App 共享 daemon 工具，App 通过 manifest 声明使用意图（`Config.Tools`），daemon 校验展开
4. **执行管线在 daemon 侧**：ExecuteTool 管线（Auth → Hook:pre → Run → Hook:post → Result）在 daemon 执行，App 侧 ToolManager 只是双向 gRPC 流转发代理

**生命周期**由 `App.Run()` 管理：

```
a.Initialize(ctx)          // 组件初始化
a.Start(ctx)               // 组件启动
a.toolManager.Start(ctx)   // ToolManager 连接 daemon gRPC + 注册 manifest
...
a.iface.Run(ctx, a.registry)  // agent 执行（此时 tm 已连接）
defer a.toolManager.Stop(ctx) // 清理
```

**结论：App 是生命周期所有者**。App → toolManager 生命周期（Start/Stop/连接 daemon）——框架独占。

### 3.2 AgentRuntime 通用注入

ToolManager 是**所有 agent 范式**都需要的工具调用能力，因此注入到 **AgentRuntime**（通用路径），而非为 ReactAgent 开特殊注入接口：

```go
// pkg/agentcore/component.go
type AgentRuntime struct {
    component.BasicComponent
    Agent       IAgentCore
    toolService tool.Service   // v0.15.x: 框架管控薄客户端的 agent 可见面
}
```

任何 IAgentCore 通过 `runtime.ToolService()` 访问（零特殊接口），与 `Reg()` 暴露 Registry 的既有哲学一致（AgentRuntime = agent 环境）。

### 3.3 tool.Service 接口隔离

为防止自定义 agentCore 误调 ToolManager 生命周期方法（Start/Stop/dial），AgentRuntime **只注入接口**，不注入具体类型：

```go
// pkg/tool/service.go
type Service interface {
    List() []ToolInfo                                       // 工具声明（ReAct 首轮注入给模型）
    Execute(ctx context.Context, name string, params map[string]any) (Result, error)
}
var _ Service = (*ToolManager)(nil)
```

| 维度 | 注入 ToolManager（完整） | 注入 tool.Service（接口，采用） |
|------|------------------------|------------------------------|
| agent 可见面 | 含生命周期/连接方法 | 仅 List/Execute |
| 误用风险 | 可误调 Start/Stop | 物理上不可能 |
| 可测试性 | 需真实 ToolManager | 可注入 mock Service |
| 依赖方向 | agentcore → 具体类型 | agentcore → 小接口 |

**职责矩阵**：

```
App            → ToolManager 生命周期（Start/Stop/连接 daemon）——框架独占
tool.Service   → List/Execute —— agent 可见面
AgentRuntime   → 持 tool.Service 引用，提供 ToolService() getter
自定义 agentCore → 只能 List/Execute，碰不到生命周期
```

### 3.4 注入方式：setter

`SetToolService(s)` 在 Builder 的 `Build()` 尾部调用（ToolManager 创建后）。选择 setter 而非 Initialize 参数的原因：

1. ToolManager 不是 registry 组件，`Initialize(ctx, registry)` 只有 registry 参数，无法携带
2. 时序不可行：`tm` 在 `Build()` 尾部创建，`AgentRuntime.Initialize` 在 `registry.InitializeAll`（`App.Initialize`）执行——tm 创建晚于 Initialize
3. setter 注入是项目"非组件资源注入"的既有统一模式（`SetCredentialStore`/`SetManifest`/`SetHookManager` 同型）

## 四、ProviderMessage 协议扩展

单轮问答的 `ProviderMessage` 只有 `Role/Content/Tools`，**无法表达** OpenAI 多轮工具调用的两种消息形态：

```json
// assistant 回传（模型上一轮的 tool_calls）
{"role":"assistant","content":"","tool_calls":[
  {"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]}
// tool 结果（工具执行后的观察）
{"role":"tool","content":"20°C","tool_call_id":"call_1"}
```

为此扩展 `ProviderMessage`：

```go
// pkg/provider/provider.go
type ProviderMessage struct {
    Role       string
    Content    string
    Tools      []ToolDefinition
    ToolCalls  []ToolCall   // assistant 消息回传（OpenAI tool_calls 字段）
    ToolCallID string       // tool 结果消息关联（OpenAI tool_call_id 字段）
}
```

引擎侧同步（openai.go）：`chatMessage` 加 `ToolCalls` 字段（json 序列化），`buildChatRequest` 映射两个新字段（`ToolCall.Args map` → `arguments` JSON 字符串）。DeepSeek 自动继承（共享实现）。

## 五、ReactAgent ReAct 循环

### 5.1 流程

```
Run(input):
 ① 工具声明：runtime.ToolService().List() → []provider.ToolDefinition（附加到首轮消息）
 ② 组装输入：ContextManager.BuildInput（系统提示 + 记忆 + 会话历史 + 用户消息）
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

- **Output.Actions** 累积每次工具调用（ToolName/Params/Result）
- **工具执行错误**：错误作为 tool 结果回填（模型可自纠正继续），不中断循环

### 5.2 停止条件

| 条件 | 行为 |
|------|------|
| 无 ToolCalls（finish） | 正常返回 |
| maxIterations（默认 10） | 返回部分结果 + `maxIterationsReached` metadata |
| ctx 取消 | 返回错误 |
| 工具执行出错 | 错误回填，模型自纠正继续 |

### 5.3 循环参数选择

| 参数 | 值 | 理由 |
|------|-----|------|
| maxIterations | 10（硬编码） | 防止无限工具调用循环；后续版本评估是否加入 config 项 |
| 工具错误处理 | 错误回填 + 计入迭代次数 | 模型自纠正，同时防反复失败刷迭代 |
| 工具结果大小 | 截断 8KB（头 4KB + 尾 4KB） | 保护上下文窗口 |

### 5.4 事件/钩子

- `EventBeforeLLM`/`EventAfterLLM`：每次 Generate 前后
- `EventBeforeTool`/`EventAfterTool`：每次工具执行前后
- 通过 AgentRuntime 的 HookManager（可选依赖）触发

## 六、跨轮次上下文与记忆

ReAct 循环只解决"单次 Run 内的工具迭代"；**跨轮次的历史消息**与**长期记忆**由 ContextManager 与 Memory 打通。

**核心架构决策：ContextManager 是 agent 的唯一输入来源**——agent 不直接碰历史/记忆，由 ContextManager 组装完整输入。

```
ContextManager（唯一输入构造者）
  ├── 持有 Memory 引用（依赖边声明 + Registry 获取）
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

**依赖获取方式**：`DefaultContextManager.Dependencies()` 声明 Memory 依赖（拓扑排序保证先初始化），`Initialize` 持有 registry 引用，通过 `Registry().GetDefault(ComponentMemory)` 获取——与 ProviderManager 通过 `AgentRuntime.Reg()` 获取的模式一致。

**BuildInput 返回类型**：`[]ContextMessage`（CM 自有类型，agent 侧转换）——保持包依赖单向（agentcore → contextmanager）。

## 七、决策记录

| # | 决策点 | 决策 |
|---|--------|------|
| D1 | Stream 是否支持 ReAct | 先只做 `Run`（非流式）完整 ReAct；`Stream` 保持单轮（工具调用过程由 daemon 侧日志呈现） |
| D2 | 系统提示来源 | 由 ContextManager.BuildInput 构造（含记忆注入），非 agent 内置 |
| D3 | maxIterations 默认值 | 硬编码 10；后续版本评估是否加入 config 项 |
| D4 | 工具错误处理 | 错误作为 tool 结果回填，模型自纠正继续；计入迭代次数，超限终止 |
| D5 | 工具结果大小 | 截断 8KB 回填（头部 4KB + 尾部 4KB，中间省略） |
| D6 | Input 是否扩展 | 加 `SessionID string`（跨轮次会话标识，CLI chat 维护） |
| D7 | 工具调用流式执行 | 非流式（ToolManager.Execute 阻塞式，ReAct"等结果再推理"语义天然匹配） |

**上下文与记忆决策**：

| # | 决策点 | 决策 |
|---|--------|------|
| C1 | ContextManager default | in-memory（进程内会话）+ Dependencies 声明 Memory 依赖，Initialize 持 registry，通过 Registry 获取 |
| C2 | Memory 实现 | 复用现有 DefaultMemory（已注册 componentDefaults） |
| C3 | Input.SessionID | 新增字段，CLI chat 会话启动时 `cm.NewSession()` 并传递 |
| C4 | CM 唯一输入源 | BuildInput 组装历史+记忆+系统提示，agent 只从 CM 拿输入 |
| C5 | 循环内 tool 消息 | 进 Memory 存档（`memory.Add` tool 交互条目），不进 CM 历史 |
| C6 | BuildInput 返回类型 | `[]ContextMessage`（包依赖单向 agentcore → contextmanager） |
| C7 | 记忆学习 | 简单版只存 tool 交互存档；对话摘要入 memory 后续版本 |

## 八、影响面

| 文件 | 改动 |
|------|------|
| `pkg/provider/provider.go` | ProviderMessage + `ToolCalls`/`ToolCallID` |
| `pkg/provider/openai.go` | chatMessage/buildChatRequest 映射 |
| `pkg/tool/service.go` | Service 接口 + `var _ Service = (*ToolManager)(nil)` |
| `pkg/agentcore/component.go` | AgentRuntime + `toolService` 字段 + `SetToolService`/`ToolService()` |
| `pkg/agentcore/agent.go` | Input + `SessionID` 字段 |
| `pkg/agentcore/react.go` | ReAct 循环 + `BuildInput` 集成 + 工具编排 + tool 存档入 Memory |
| `pkg/contextmanager/default.go` | DefaultContextManager（in-memory + Memory 依赖 + Registry 获取） |
| `pkg/contextmanager/context.go` | IContextManager + `BuildInput` 方法 |
| `pkg/contextmanager/component.go` | Dependencies 声明 Memory 依赖 |
| `pkg/iface/cli/cmd_chat.go` | 会话维护：启动 `cm.NewSession()`，消息携带 `SessionID` |
| `pkg/app/builder.go` | Build 尾部 `rt.SetToolService(tm)` + componentDefaults 注册 ContextManager |
| 测试 | ProviderMessage wire、ReAct 循环（mock provider + mock Service）、上下文组装、错误回填、maxIterations、端到端多轮 |

## 九、参考

- 工具生态架构：[tool.md](./tool.md)
- Provider 模块设计：[provider.md](./provider.md)
- AgentRuntime 组件：`pkg/agentcore/component.go`（Reg() 暴露 Registry 的既有设计）
