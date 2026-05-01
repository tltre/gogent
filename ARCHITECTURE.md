# Gogent 架构设计文档

## 概述

Gogent 是一个基于 Go 语言的 IAgentCore 基础设施框架，采用模块化组件架构，支持多种驱动方式（Native、HTTP、Process），可快速构建可扩展的 IAgentCore 应用。

## 核心设计原则

### 1. 组件化架构

所有子系统统一实现 `AgentRuntime` 接口：

```go
type AgentRuntime interface {
    Name() string
    Type() ComponentType
    Initialize(ctx context.Context, registry Dependencies) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Dependencies() map[string]DependencySpec
}
```

**优势**：
- 统一生命周期管理
- 自动依赖解析
- 支持热插拔

### 2. 驱动分离

每个子系统支持多种驱动实现：

| 驱动 | 用途 | 通信方式 |
|------|------|----------|
| Native | 代码级实现 | 直接调用 |
| HTTP | 远程服务 | REST API |
| Process | 本地进程 | MCP 协议 (stdio) |

**优势**：
- 用户可选择二开、远程调用或进程隔离
- 同一接口，不同实现
- 便于测试和 Mock

### 3. 依赖注入

组件依赖通过 `Dependencies` 接口注入：

```go
type Dependencies interface {
    Get(name string) AgentRuntime
    GetByType(typ ComponentType) []AgentRuntime
    GetDefault(typ ComponentType) AgentRuntime
}
```

**初始化流程**：
1. 解析所有组件的 `Dependencies()`
2. 构建依赖图
3. 拓扑排序
4. 按顺序初始化

### 4. 配置驱动

通过 YAML 配置组装应用：

```yaml
components:
  - name: "provider-openai"
    type: "provider"
    driver: "http"
    config:
      endpoint: "https://api.openai.com/v1"
      apiKey: "${OPENAI_API_KEY}"
```

## 子系统详解

### IChannel（消息渠道）

**职责**：用户消息接入

**接口**：
```go
type IChannel interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Receive(ctx context.Context) (<-chan Message, error)
    Send(ctx context.Context, msg Message) error
}
```

**实现**：
- `NativeChannel`: 内存 IChannel，适合 CLI
- `HttpChannel`: HTTP 轮询
- `ProcessChannel`: MCP 协议

### AgentCore（IAgentCore 核心）

**职责**：IAgentCore 执行逻辑与协调

**接口**：
```go
type IAgentCore interface {
    Run(ctx context.Context, input Input) (Output, error)
    Stream(ctx context.Context, input Input) (<-chan Event, error)
}
```

**依赖**：
- IProvider（必需）
- ToolManager（可选）
- HookManager（可选）
- IContextManager（可选）
- IMemory（可选）
- IEventBus（可选）

### IProvider（LLM 提供者）

**职责**：与 LLM 交互

**接口**：
```go
type IProvider interface {
    Generate(ctx context.Context, messages []ProviderMessage) (Response, error)
    Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
    ModelInfo() ModelInfo
}
```

### ITool（工具）

**职责**：工具定义与执行

**接口**：
```go
type ITool interface {
    Info() ToolInfo
    Execute(ctx context.Context, params map[string]any) (Result, error)
    Stream(ctx context.Context, params map[string]any) (<-chan StreamChunk, error)
}

type ToolManager interface {
    Register(tool ITool) error
    Get(name string) ITool
    List() []ToolInfo
    Execute(ctx context.Context, name string, params map[string]any) (Result, error)
}
```

### IHook（钩子）

**职责**：生命周期拦截

**事件类型**：
- EventBeforeRun / EventAfterRun
- EventBeforeTool / EventAfterTool
- EventBeforeLLM / EventAfterLLM
- EventError

**接口**：
```go
type IHook interface {
    OnEvent(ctx context.Context, event Event) (context.Context, error)
    Events() []EventType
}
```

### IEventBus（事件总线）

**职责**：跨子系统异步消息

**接口**：
```go
type IEventBus interface {
    Publish(ctx context.Context, topic Topic, event Event) error
    Subscribe(ctx context.Context, topic Topic) (Subscription, error)
    Unsubscribe(sub Subscription) error
}
```

### IContextManager（上下文管理）

**职责**：对话与执行上下文

**功能**：
- 会话管理（多 session）
- 消息历史
- 变量存储
- Summary 生成
- System Prompt

### IMemory（记忆）

**职责**：长期记忆存储

**接口**：
```go
type IMemory interface {
    Add(ctx context.Context, item MemoryItem) error
    Query(ctx context.Context, q Query) ([]MemoryItem, error)
    Clear(ctx context.Context) error
}
```

### ISandbox（沙箱）

**职责**：安全执行环境

**功能**：
- 代码执行隔离
- 资源限制（内存、CPU、时间）
- 网络访问控制

## 扩展指南

### 新增子系统

1. **创建目录**：`pkg/newsubsystem/`

2. **定义接口**：`newsubsystem/types.go`
```go
type MyInterface interface {
    DoSomething(ctx context.Context) error
}
```

3. **实现 AgentRuntime**：`newsubsystem/component.go`
```go
type AgentRuntime struct {
    name string
    impl MyInterface
}

func (c *AgentRuntime) Name() string { return c.name }
func (c *AgentRuntime) Type() component.ComponentType { return "newsubsystem" }
func (c *AgentRuntime) Initialize(ctx context.Context, registry component.Dependencies) error { return nil }
func (c *AgentRuntime) Start(ctx context.Context) error { return nil }
func (c *AgentRuntime) Stop(ctx context.Context) error { return nil }
func (c *AgentRuntime) Dependencies() map[string]component.DependencySpec { return nil }
```

4. **实现驱动**：
   - `native.go`: Native 实现
   - `http.go`: HTTP 实现
   - `process.go`: MCP 实现

5. **更新 Builder**：`pkg/app/builder.go`
```go
case "newsubsystem":
    return b.buildNewSubsystem(cc)
```

6. **配置使用**：
```yaml
components:
  - name: "my-subsystem"
    type: "newsubsystem"
    driver: "native"
```

### 横向扩展（预留）

未来支持多实例和组合：

```yaml
components:
  - name: "eventbus-primary"
    type: "eventbus"
    driver: "composite"
    config:
      instances: ["eventbus-mq", "eventbus-local"]
      strategy: "failover"
```

**预留设计**：
- `GetByType()` 返回数组
- 组件名称唯一标识
- 依赖支持指定名称

## 通信协议

### MCP 协议（Process 驱动）

基于 JSON-RPC 2.0，通过 stdio 通信：

```json
{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "search", "arguments": {}}}
```

### REST API（HTTP 驱动）

统一 REST 风格：
- `POST /generate` - LLM 生成
- `POST /execute` - 工具执行
- `POST /hook` - IHook 触发
- `POST /publish/{topic}` - 事件发布

## 安全考虑

### ISandbox 隔离

```go
type ResourceLimits struct {
    MaxMemoryMB   int
    MaxCPUTime    time.Duration
    MaxFileSize   int64
    NetworkAccess bool
}
```

### 环境变量

配置支持 `${VAR}` 语法：
```yaml
config:
  apiKey: "${OPENAI_API_KEY}"
```

## 性能优化建议

1. **连接池**：HTTP 驱动复用 client
2. **缓冲 IChannel**：消息队列设置合理 buffer
3. **超时控制**：所有外部调用设置 timeout
4. **拓扑排序**：依赖初始化顺序最优

## 未来规划

- [ ] Composite 组件（多实例组合）
- [ ] 路由策略（failover、broadcast、sharding）
- [ ] 可观测性（metrics、tracing）
- [ ] 动态配置更新
- [ ] 插件系统（Go plugin）
