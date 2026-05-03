# AGENTS.md — Gogent

## Project at a Glance

- **Module**: `github.com/tltre/gagent`
- **Go**: 1.25.5
- **Version**: v0.2.0
- **Entrypoint**: `cmd/gagent/main.go` — expects a YAML config path as first argument
- **Deps**: `gopkg.in/yaml.v3` (direct), `github.com/mark3labs/mcp-go` (indirect), `go.uber.org/zap` (indirect)
- **CI / lint / Makefile**: none

## Development Commands

```bash
go build ./...                              # build all packages
go vet ./...                                # static analysis
go test ./tests/native/ -v -timeout 30s     # native mock tests (13)
go test ./tests/stdio/ -v -timeout 60s      # stdio round-trip tests (11)
go test ./tests/http/ -v -timeout 30s       # http round-trip tests (9)
go test ./tests/integration/ -v -timeout 60s # full integration test
```

## Architecture

### Component system

Every subsystem is a `component.Component` (interface in `pkg/component/component.go`):

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

A **Registry** (`pkg/component/registry.go`) holds components and does topological-sort initialization. Every component wrapping struct implements `Component` and delegates to a "business interface" (e.g., `IMemory`, `IProvider`, `IAgentCore`).

10 component types: `ComponentChannel`, `ComponentAgentCore`, `ComponentProvider`, `ComponentTool`, `ComponentHook`, `ComponentEventBus`, `ComponentContextManager`, `ComponentMemory`, `ComponentSandbox`, `ComponentLogger`.

### Builder pattern

`app.Builder` reads YAML config → creates components → registers them → returns `*App`. Builder only handles `driver: "http"` and `driver: "process"`. Native (`driver: "native"`) components are skipped — they must be injected via `With*()` BuildOptions or provided by `componentDefaults` fallback.

**Priority chain**: `With*` injection > YAML config > `componentDefaults` fallback.

`registerDefaults()` provides out-of-box defaults for: Logger, EventBus, Memory, Sandbox.

### YAML config format

See `config/example.yaml`. Each component: `name`, `type`, `driver` (http/process), optional `config` map, optional `dependencies` map. `defaults` section sets which named component is default for each type.

The `agentcore` depends on all other components, resolved from registry at Initialize via `registry.GetDefault(type).(concreteInterface)`.

### Transport layer (`internal/client/`)

```go
type Transport interface {
    Start(ctx context.Context) error
    Close() error
    Call(ctx context.Context, method string, params any, result any) error
    OnNotify(method string, handler func(params json.RawMessage))
}
```

| Implementation | Wraps | Use |
|---|---|---|
| `StdioTransport` | `mark3labs/mcp-go/transport.Stdio` | Subprocess communication |
| `HTTPTransport` | `mark3labs/mcp-go/transport.StreamableHTTP` | Remote HTTP communication |

Both transports use simplified JSON-RPC 2.0 with Gogent-defined method names and a custom `initialize` handshake exchanging `componentType`, `protocolVersion`, and `methods`. After handshake, `services/announce` notification informs the daemon of available services.

**`LazyTransport`** wraps any `Transport` and auto-starts it on first `Call()`. All `Process*` implementations use `client.WrapLazy()` internally.

### Observability

All instrumentation flows through EventBus topic `system.log`:

- **AgentRuntime**: `Initialize`/`Start`/`Stop`/`Run` publish `LogEvent` with traceId via `c.EventBus.Publish("system.log", ...)`
- **Transport**: `Call()` logs method + duration + success/failure via injected `client.Logger`
- **LoggerComponent**: subscribes EventBus `system.log`, goroutine dispatches to underlying `Logger` (default: `DefaultLogger` wrapping zap)
- **Remote daemon**: sends `logger/log` notification → main process `OnNotify` → routed to Logger

`WithTraceID(ctx)` generates a per-request traceId carried in context for cross-component correlation.

### AgentRuntime fields

```
Provider       provider.IProvider
ToolManager    *tool.ToolManager
HookManager    *hook.HookManager
ContextManager contextmanager.IContextManager
Memory         memory.IMemory
EventBus       eventbus.IEventBus
Sandbox        sandbox.ISandbox
```

Resolved from registry during `Initialize()`, injected via `c.Agent.SetAgentRuntime(c)`.

### Default implementations (framework-provided, user-replaceable)

| Component | Type | File |
|-----------|------|------|
| Logger | `DefaultLogger` (zap) | `pkg/logger/default.go` |
| Memory | `DefaultMemory` (in-memory map) | `pkg/memory/default.go` |
| EventBus | `DefaultEventBus` (in-memory pub/sub) | `pkg/eventbus/default.go` |
| Sandbox | `DefaultSandbox` (os/exec + command whitelist) | `pkg/sandbox/default.go` |
| Provider | `DefaultProvider` (stub placeholder) | `pkg/provider/default.go` |

## Patterns & Conventions

- **Naming**: Component wrappers via `NewComponent(name, impl)`. Default implementations use `Default*` prefix (`DefaultMemory`, `DefaultLogger`, etc.).
- **Driver dispatch**: Builder dispatches on `cc.Driver` using `DriverHTTP`/`DriverProcess` constants. `DriverNative` returns nil — components are user-injected.
- **Type matching**: `buildComponent` converts `cc.Type` to `component.ComponentType` before switching.
- **build* functions access config as raw `map[string]any`** — not via typed config structs. Stay consistent.
- **Sentinel errors**: `pkg/component/errors.go` and `pkg/app/error.go` — use `errors.Is()`.

## Gotchas

1. **Mismatched `IChannel` interface**: `channel.IChannel` has `Name() string` (no Get prefix), but `component.Component` uses `GetName()`. `ChannelManager` satisfies both.

2. **AgentRuntime naming**: `agentcore/component.go`'s `*AgentRuntime` is the component wrapping struct, not the `IAgentCore` implementation. `IAgentCore` is the agent logic interface; `*AgentRuntime` wraps it as a `Component`.

3. **Native implementations are user-provided**: The framework does NOT ship native implementations of business interfaces. They must be injected via `With*()` BuildOptions, or the `componentDefaults` fallback provides defaults for Logger/EventBus/Memory/Sandbox.

4. **Process* implementations use LazyTransport**: transports auto-start on first `Call()`. Always use `client.WrapLazy()` for new process implementations.

5. **Driver constants**: Use `DriverHTTP`/`DriverProcess`/`DriverNative` from `pkg/app/builder.go`, never hardcoded strings.

## v0.3.0 Roadmap — Multi-Instance Components & Registry Transparency

### Phase 1: Registry Transparent Access（本地 Registry 按需获取）

**目标**：组件通过 Registry 自主获取任意实例，不再由 Initialize 自动注入单一实例。

**设计要点**：

- `Dependencies()` 保留——声明**类型**依赖（`ComponentEventBus`），仅用于拓扑排序保证初始化顺序
- `Initialize(ctx, registry)` 中组件保留 Registry 引用，**不再**自动解析实例到固定字段
- 框架埋点用 `reg.GetDefault(type)` 获取默认实例（总是可用）
- 用户代码用 `reg.Get(name)` 精确获取命名实例、`reg.GetByType(type)` 获取同类型全部实例
- AgentRuntime 移除 `Provider`/`EventBus`/`Memory` 等固定字段，改为 `reg` 引用

**改动清单**：

| 文件 | 操作 |
|------|------|
| `pkg/agentcore/component.go` | 移除固定字段（Provider/ToolManager/...），保留 `Agent` + `reg`；提供 `Reg()` 访问器 |
| `pkg/agentcore/component.go` | `publishLog` 改为 `reg.GetDefault(ComponentEventBus)` 直出 |
| 各组件 `component.go` | `Initialize()` 只保留 `reg`，移除自动类型断言注入 |
| `tests/*/` | 更新所有使用 `r.Provider` 等字段的代码为 `r.Reg().Get(...)` |

**验证点**：
- 单实例：`reg.GetDefault(ComponentProvider)` 返回唯一实例
- 多实例：`reg.Get("provider-smart")` 返回指定实例
- 拓扑排序：AgentRuntime 初始化前保证同类型所有实例已初始化

### Phase 2: Remote Discovery Protocol（远端 daemon 查询可用服务）

**目标**：daemon 进程通过 Transport 查询主进程 Registry 中可用的组件实例。

**协议**：

```
daemon → Client: {"method": "services/lookup", "params": {"type": "eventbus", "name": "log"}}
Client → daemon: {"result": {"name": "eventbus-log"}}

daemon → Client: {"method": "services/lookup", "params": {"type": "eventbus", "name": ""}}
Client → daemon: {"result": {"name": "eventbus-business"}}   // default

daemon → Client: {"method": "services/lookupAll", "params": {"type": "eventbus"}}
Client → daemon: {"result": {"instances": ["eventbus-business", "eventbus-log"]}}
```

**改动清单**：

| 文件 | 操作 |
|------|------|
| `internal/client/stdio_transport.go` | 注册 `services/lookup` / `services/lookupAll` handler |
| `internal/client/http_transport.go` | 同上 |
| daemon 端 handler（测试用） | 实现 lookup 模拟返回 |

**SDK 侧（未来独立仓库）**：
- `sdk.Lookup(type)` → `Transport.Call("services/lookup")` → 返回默认实例名
- `sdk.Lookup(type, name)` → 返回指定实例名
- `sdk.LookupAll(type)` → 返回全部实例名列表

**验证点**：
- daemon 进程通过 lookup 查询到主进程注册的 eventbus/provider 等实例名
- 单实例 YAML 无 dependencies → lookup 返回 default
