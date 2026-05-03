# AGENTS.md — Gogent

## Project at a Glance

- **Module**: `github.com/tltre/gagent`
- **Go**: 1.25.5
- **Version**: v0.3.0
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

### Registry Transparent Access (v0.3.0)

AgentRuntime no longer holds fixed fields for each subsystem. All dependency access goes through the Registry:

```go
type AgentRuntime struct {
    Name  string
    Agent IAgentCore
    reg   *component.Registry
}

func (c *AgentRuntime) Reg() *component.Registry
```

- **Framework instrumentation**: `reg.GetDefault(ComponentEventBus)` — always available via defaults
- **User code**: `reg.Get("provider-smart")` for named instances, `reg.GetByType(ComponentProvider)` for all
- **Dependencies()**: declaration preserved for topological sort ordering (type only, no instance name)
- Multi-instance: same type (`"eventbus"`) can have multiple named instances (`eventbus-business`, `eventbus-log`)

### Remote Discovery Protocol (v0.3.0)

Stdio transports expose `services/lookup` and `services/lookupAll` for daemon processes to query the main Registry:

```
daemon → main: {"method": "services/lookup", "params": {"type": "eventbus", "name": "log"}}
main → daemon: {"result": {"name": "eventbus-log"}}
```

Builder registers the lookup handler on all stdio transports via `StdioTransportConfig.RequestHandler`.

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
