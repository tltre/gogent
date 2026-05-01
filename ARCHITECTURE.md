# Gogent 架构设计文档

## 概述

Gogent 是一个基于 Go 语言的 Agent 基础设施框架，采用模块化组件架构，支持 Native（进程内）、Process（stdio 子进程）、HTTP（远程）三种驱动方式。

## 核心设计原则

### 1. 组件化架构

所有子系统统一实现 `Component` 接口：

```go
type Component interface {
    GetName() string
    GetType() ComponentType
    Initialize(ctx context.Context, deps *Registry) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Dependencies() map[string]DependencySpec
}
```

每个组件是一个包装器（如 `ProviderComponent`），内部持有一个业务接口实现（`IProvider`），同时暴露 `Component` 生命周期方法和业务方法。

### 2. 驱动分离

同一业务接口，三种驱动实现，对外透明：

| 驱动 | 通信方式 | 组件获取方式 |
|------|----------|-------------|
| Native | 直接函数调用 | BuildOption `With*()` 注入 |
| Process | stdio + JSON-RPC | `StdioTransport` 管理子进程 |
| HTTP | HTTP POST + JSON-RPC | `HTTPTransport` 连接远程端点 |

关键：AgentCore 调用 `provider.Generate()` 时，不关心对方是本地代码、子进程还是远程 HTTP——接口一致。

### 3. 依赖注入

`Registry` 管理所有组件，`topologicalSort()` 自动解析依赖顺序：

```
InitializeAll → topologicalSort → [eventbus, memory, provider, tool, hook, context, sandbox, agentcore]
```

AgentCore 在 `Initialize()` 中通过 `registry.GetDefault(type)` 获取所有依赖，注入 `AgentRuntime`。

### 4. 配置驱动

YAML 声明组件 → Builder 解析 → Registry 注册 → App 运行：

```yaml
components:
  - name: "provider-main"
    type: "provider"
    driver: "process"
    config:
      command: "./provider-daemon"
      env: ["KEY=value"]
defaults:
  provider: "provider-main"
```

## Transport 层

### 设计

`Transport` 接口是框架与外部通信的唯一抽象：

```go
type Transport interface {
    Start(ctx context.Context) error
    Close() error
    Call(ctx context.Context, method string, params any, result any) error
}
```

### 实现

| 实现 | 底层 | 帧协议 |
|------|------|--------|
| `StdioTransport` | `mark3labs/mcp-go/transport.Stdio` | Content-Length |
| `HTTPTransport` | `mark3labs/mcp-go/transport.StreamableHTTP` | HTTP |

### 协议

简化版 JSON-RPC 2.0，Gogent 自定义 method：

```
Request:  {"jsonrpc":"2.0","id":1,"method":"provider/generate","params":{...}}
Response: {"jsonrpc":"2.0","id":1,"result":{...}}
```

握手（`initialize`）：

```
Client: {"jsonrpc":"2.0","method":"initialize","params":{"componentType":"provider","protocolVersion":"0.1.0"}}
Server: {"jsonrpc":"2.0","result":{"protocolVersion":"0.1.0","componentType":"provider","methods":["provider/generate",...]}}
```

### LazyTransport

包装任意 `Transport`，首次 `Call()` 时自动 `Start()`。所有 `Process*` 实现内部使用 `WrapLazy()`。

### 组件方法映射

| 业务接口方法 | 远程 method |
|-------------|------------|
| `IProvider.Generate()` | `provider/generate` |
| `IProvider.ModelInfo()` | `provider/modelInfo` |
| `ToolManager.Execute()` | `tools/call` |
| `IMemory.Add()` | `memory/add` |
| `IMemory.Query()` | `memory/query` |
| `IMemory.Count()` | `memory/count` |
| `IHook.OnEvent()` | `hook/onEvent` |
| `IEventBus.Publish()` | `eventbus/publish` |
| `IContextManager.NewSession()` | `context/newSession` |
| `IContextManager.GetSummary()` | `context/getSummary` |
| `IContextManager.BuildSystemPrompt()` | `context/buildSystemPrompt` |
| `ISandbox.Create()` | `sandbox/create` |
| `ISandbox.Execute()` | `sandbox/execute` |

## 子系统详解

### AgentCore

**接口**：
```go
type IAgentCore interface {
    Run(ctx context.Context, input Input) (Output, error)
    Stream(ctx context.Context, input Input) (<-chan Event, error)
    SetAgentRuntime(runtime *AgentRuntime)
}
```

**AgentRuntime** 持有所有子系统引用：
```
Provider       provider.IProvider
ToolManager    *tool.ToolManager
HookManager    *hook.HookManager
ContextManager contextmanager.IContextManager
Memory         memory.IMemory
EventBus       eventbus.IEventBus
Sandbox        sandbox.ISandbox
```

全部从 Registry 在 `Initialize()` 时解析。

### Provider

```go
type IProvider interface {
    Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
    Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
    ModelInfo() ModelInfo
}
```

### Tool

```go
type ITool interface {
    Info() ToolInfo
    Execute(ctx context.Context, params map[string]any) (Result, error)
}
```

`ToolManager` 是工具容器，同时是 `Component`，管理多个 `ITool` 实例的注册、查找和执行。

### Hook

```go
type IHook interface {
    OnEvent(ctx context.Context, event Event) (context.Context, error)
    Events() []EventType
}
```

`HookManager` 按 `EventType` 索引，`Trigger()` 按序调用匹配的 hook。

### EventBus

```go
type IEventBus interface {
    Publish(ctx context.Context, topic Topic, event Event) error
    Subscribe(ctx context.Context, topic Topic) (Subscription, error)
    Unsubscribe(sub Subscription) error
}
```

### ContextManager

```go
type IContextManager interface {
    NewSession() string
    AddMessage(sessionId string, msg ContextMessage) error
    GetMessages(sessionId string) []ContextMessage
    GetSummary(sessionId string) (Summary, error)
    BuildSystemPrompt(sessionId string) string
    Clear(sessionId string) error
    DeleteSession(sessionID string)
}
```

### Memory

```go
type IMemory interface {
    Add(ctx context.Context, item MemoryItem) error
    AddBatch(ctx context.Context, items []MemoryItem) error
    Query(ctx context.Context, q Query) ([]MemoryItem, error)
    Get(ctx context.Context, id string) (MemoryItem, error)
    Delete(ctx context.Context, id string) error
    Clear(ctx context.Context) error
    Count(ctx context.Context) (int64, error)
}
```

### Sandbox

```go
type ISandbox interface {
    Create(ctx context.Context) (string, error)
    Destroy(ctx context.Context, id string) error
    Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error)
    SetLimits(limits ResourceLimits)
    GetLimits() ResourceLimits
}
```

### Channel

```go
type IChannel interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Receive(ctx context.Context) (<-chan Message, error)
    Send(ctx context.Context, msg Message) error
}
```

## 扩展指南

### 新增子系统

1. 创建 `pkg/newsubsystem/` → 定义业务接口 `types.go`
2. 实现组件包装器 `component.go`（`GetName`/`GetType`/`Initialize`/`Start`/`Stop`/`Dependencies`）
3. 实现三种驱动：`native.go`、`process.go`、`http.go`
4. 在 `app/builder.go` `buildComponent` 中添加 `case` 分支
5. 如需 AgentCore 依赖，在 `agentcore/component.go` 添加字段和解析逻辑

## 未来规划

- [ ] SDK 独立仓库（daemon/server 侧实现）
- [ ] Composite 组件（多实例组合、failover）
- [ ] 可观测性（metrics、tracing）
- [ ] 动态配置更新
