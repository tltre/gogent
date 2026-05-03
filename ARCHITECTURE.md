# Gogent 架构设计文档

## 概述

Gogent 是一个基于 Go 语言的 Agent 基础设施框架，采用模块化组件架构，支持 Process（stdio 子进程）、HTTP（远程）两种驱动方式。Native 实现由用户注入。

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

同一业务接口，两种框架驱动实现，对外透明：

| 驱动 | 通信方式 | 组件获取方式 |
|------|----------|-------------|
| Native | 直接函数调用 | `With*` BuildOption 注入 |
| Process | stdio + JSON-RPC | `StdioTransport` 管理子进程 |
| HTTP | HTTP POST + JSON-RPC | `HTTPTransport` 连接远程端点 |

关键：AgentCore 调用 `provider.Generate()` 时不关心对方是本地代码、子进程还是远程 HTTP。

### 3. 优先级链

```
With* 注入（最高） > YAML 配置 > componentDefaults 兜底（最低）
```

`registerDefaults()` 为 Logger、EventBus、Memory、Sandbox 提供开箱即用的默认实现。

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
    OnNotify(method string, handler func(params json.RawMessage))
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
Client: {"method":"initialize","params":{"componentType":"provider","protocolVersion":"0.1.0"}}
Server: {"result":{"protocolVersion":"0.1.0","componentType":"provider","methods":[...]}}
```

握手后发送 `services/announce` 通知 daemon 可用服务。`OnNotify` 接收 daemon 回发的 `logger/log`、`eventbus/publish` 等通知并路由。

### LazyTransport

包装任意 `Transport`，首次 `Call()` 时自动 `Start()`。所有 `Process*` 实现内部使用 `WrapLazy()`。

### 组件方法映射

| 业务接口方法 | 远程 method |
|-------------|------------|
| `IProvider.Generate()` | `provider/generate` |
| `IProvider.ModelInfo()` | `provider/modelInfo` |
| `ToolManager.Execute()` | `tools/call` |
| `IMemory.Query()` | `memory/query` |
| `IMemory.Add()` | `memory/add` |
| `IHook.OnEvent()` | `hook/onEvent` |
| `IEventBus.Publish()` | `eventbus/publish` |
| `IContextManager.NewSession()` | `context/newSession` |
| `IContextManager.GetSummary()` | `context/getSummary` |
| `ISandbox.Create()` | `sandbox/create` |
| `ISandbox.Execute()` | `sandbox/execute` |

## 可观测性

全部埋点通过 `EventBus.Publish("system.log", LogEvent{...})` 发布，`LoggerComponent` 订阅消费。

- **AgentRuntime**: `Initialize`/`Start`/`Stop`/`Run` 发布 `LogEvent`，含 traceId
- **Transport**: `Call()` 前后记录 method + duration + 成功/失败
- **LoggerComponent**: `Initialize()` 获取 EventBus，`Start()` 订阅，goroutine 分发到 `Logger` 实现
- **默认 Logger**: `DefaultLogger`（基于 zap），支持 console/JSON 格式

`WithTraceID(ctx)` 生成 traceId，同一 `Run()` 内所有日志共享。

## 子系统详解

### AgentCore

```go
type IAgentCore interface {
    Run(ctx context.Context, input Input) (Output, error)
    Stream(ctx context.Context, input Input) (<-chan Event, error)
    SetAgentRuntime(runtime *AgentRuntime)
}
```

`AgentRuntime` 持有 `reg *component.Registry`，所有依赖通过 Registry 按需获取。框架埋点用 `reg.GetDefault(type)`，用户代码用 `reg.Get(name)` 获取命名实例。

### Registry 透明访问（v0.3.0）

AgentRuntime 不再持有固定字段。所有子系统引用通过 Registry 获取：

```go
type AgentRuntime struct {
    Name  string
    Agent IAgentCore
    reg   *component.Registry
}
func (c *AgentRuntime) Reg() *component.Registry
```

- **单实例**: `reg.GetDefault(ComponentProvider)` — 自动走 YAML `defaults` 或第一个注册的
- **多实例**: `reg.Get("provider-smart")` — 精确命名获取；`reg.GetByType(ComponentProvider)` — 全部
- **Dependencies()**: 保留类型声明，仅用于拓扑排序保序

### Remote Discovery Protocol（v0.3.0）

Stdio transport 注册 `services/lookup` 和 `services/lookupAll` 处理器，daemon 进程可查询主进程 Registry：

```
daemon → Client: {"method": "services/lookup", "params": {"type": "eventbus", "name": "log"}}
Client → daemon: {"result": {"name": "eventbus-log"}}
```

`StdioTransportConfig.RequestHandler` 在 `Start()` 时注册。未来 SDK 封装为 `sdk.Lookup(type, name)`。

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

`ChannelManager` 管理多个 `IChannel`，同时实现 `component.Component`。

### Provider

```go
type IProvider interface {
    Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
    Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
    ModelInfo() ModelInfo
}
```

框架提供 `DefaultProvider`（stub）。`ProcessProvider` 通过 Transport 委派给远程 daemon。

### Tool / Hook

`ITool` 和 `IHook` 分别由 `ToolManager` 和 `HookManager` 管理多实例。两个 Manager 同时是 `Component`。

### EventBus

```go
type IEventBus interface {
    Publish(ctx context.Context, topic Topic, event Event) error
    Subscribe(ctx context.Context, topic Topic) (Subscription, error)
}
```

框架提供 `DefaultEventBus`（内存 pub/sub）。所有可观测性日志通过 `system.log` topic 流转。

### ContextManager

```go
type IContextManager interface {
    NewSession() string
    AddMessage(sessionId string, msg ContextMessage) error
    GetMessages(sessionId string) []ContextMessage
    GetSummary(sessionId string) (Summary, error)
    BuildSystemPrompt(sessionId string) string
    Clear(sessionId string) error
}
```

### Memory

```go
type IMemory interface {
    Add(ctx context.Context, item MemoryItem) error
    Query(ctx context.Context, q Query) ([]MemoryItem, error)
    Clear(ctx context.Context) error
}
```

框架提供 `DefaultMemory`（内存 map）。

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

`ResourceLimits` 含 `AllowedCommands` 白名单和 `ReadOnlyRoot`。框架提供 `DefaultSandbox`（os/exec + 命令白名单）。

## 扩展指南

### 新增子系统

1. 创建 `pkg/newsubsystem/` → 定义业务接口 + 组件包装器 `component.go`
2. 实现驱动：`process.go`、可选的 `default.go`（提供框架默认实现）
3. 在 `app/builder.go` 添加 `case component.ComponentXxx` 分支 + `buildXxx` 方法
4. 如需 AgentCore 依赖，在 `agentcore/component.go` 添加字段和解析逻辑
5. 如需默认实现，在 `componentDefaults` 注册工厂

### 加入 componentDefaults

```go
var componentDefaults = map[component.ComponentType]func() component.Component{
    component.ComponentNewType: func() component.Component {
        impl := newsubsystem.NewDefaultXxx()
        return newsubsystem.NewComponent("xxx-default", impl)
    },
    // ...
}
```

自动补充 YAML 未配置的组件。

## 未来规划

- [ ] SDK 独立仓库（daemon/server 侧实现，提供 RemoteLogger、ComponentServer）
- [ ] AgentCore 执行循环（LLM → tool_call → LLM → output）
- [ ] Channel ↔ EventBus 消息管道
- [ ] BuildOption 补齐（WithTools、WithHooks、WithChannels）
- [ ] Composite 组件（多实例组合、failover）
- [ ] 动态配置更新
