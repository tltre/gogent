# AGENTS.md — Gogent

## Project at a Glance

- **Module**: `github.com/tltre/gagent`
- **Go**: 1.25.5
- **Entrypoint**: `cmd/gagent/main.go` — expects a YAML config path as first argument
- **Deps**: `gopkg.in/yaml.v3` (direct), `github.com/mark3labs/mcp-go` (indirect)
- **Version**: v0.0.1
- **CI / lint / Makefile**: none

## Development Commands

```bash
go build ./...                              # build all packages
go vet ./...                                # static analysis
go test ./tests/native/ -v -timeout 30s     # native mock tests (9)
go test ./tests/stdio/ -v -timeout 60s      # stdio round-trip tests (8)
go test ./tests/http/ -v -timeout 30s       # http round-trip tests (8)
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

Component types: `ComponentChannel`, `ComponentAgentCore`, `ComponentProvider`, `ComponentTool`, `ComponentHook`, `ComponentEventBus`, `ComponentContextManager`, `ComponentMemory`, `ComponentSandbox`.

### Builder pattern

`app.Builder` reads YAML config → creates components → registers them → returns `*App`. The builder's `buildComponent` switch handles all 9 component types.

Use `BuildOption` functions (`WithAgentCore`, `WithProvider`, `WithMemory`, `WithEventBus`, `WithContextManager`) to inject custom implementations. `WithHooks`, `WithTools`, `WithChannels` are TODO stubs.

### YAML config format

See `config/example.yaml`. Each component: `name`, `type`, `driver` (native/http/process), optional `config` map, optional `dependencies` map. `defaults` section sets which named component is default for each type.

The `agentcore` is special — depends on all other components, resolves them from the registry at Initialize via `registry.GetDefault(type).(concreteInterface)`.

### Transport layer (`internal/client/`)

```go
type Transport interface {
    Start(ctx context.Context) error
    Close() error
    Call(ctx context.Context, method string, params any, result any) error
}
```

| Implementation | Wraps | Use |
|---|---|---|
| `StdioTransport` | `mark3labs/mcp-go/transport.Stdio` | Subprocess communication |
| `HTTPTransport` | `mark3labs/mcp-go/transport.StreamableHTTP` | Remote HTTP communication |

Both transports use simplified JSON-RPC 2.0 with Gogent-defined method names (`provider/generate`, `tools/call`, etc.) and a custom `initialize` handshake exchanging `componentType`, `protocolVersion`, and `methods`.

**`LazyTransport`** wraps any `Transport` and auto-starts it on first `Call()`. All `Process*` implementations use `client.WrapLazy()` internally.

### Process / HTTP drivers

All 9 component types support `driver: "process"` and `driver: "http"`. A `Process*` struct implements the business interface, internally using `Transport.Call()`:

```
builder sees driver:"process"
  → newStdioTransport(config) → StdioTransport{command, args, env}
  → NewProcessProvider(transport) → implements IProvider via LazyTransport
  → NewComponent(name, p) → Register

agentCore calls provider.Generate()
  → ProcessProvider.Generate()
    → transport.Call("provider/generate", msgs, &resp)
```

Config example for process:
```yaml
- name: "provider-remote"
  type: "provider"
  driver: "process"
  config:
    command: "go"
    args: ["run", "./cmd/my-provider-daemon"]
    env: ["KEY=value"]
```

Config example for http:
```yaml
- name: "provider-remote"
  type: "provider"
  driver: "http"
  config:
    endpoint: "http://localhost:8080"
    timeout: "30s"
```

**No server/daemon side in this repo** — daemon will be provided by the future `gagent-sdk` repo.

### AgentRuntime fields

`agentcore/component.go`'s `*AgentRuntime` exposes all subsystems as fields:

```
Provider       provider.IProvider
ToolManager    *tool.ToolManager
HookManager    *hook.HookManager
ContextManager contextmanager.IContextManager
Memory         memory.IMemory
EventBus       eventbus.IEventBus
Sandbox        sandbox.ISandbox
```

All resolved from registry during `Initialize()`, then injected via `c.Agent.SetAgentRuntime(c)`.

## Patterns & Conventions

- **Naming**: Component wrappers created via `NewComponent(name, impl)`. Check each package for exact signature.
- **Driver dispatch**: Builder's `buildXxx` methods switch on `cc.Driver`. Supported values: `"native"`, `"http"`, `"process"`.
- **build* functions access config as raw `map[string]any`** — not through the typed config structs in `config.go`. Stay consistent if adding config fields.
- **Sentinel errors**: `pkg/component/errors.go` and `pkg/app/error.go` define sentinel errors — use `errors.Is()`.

## Gotchas

1. **`NativeAgent` doesn't implement `IAgentCore`**: `agentcore/native.go`'s `*NativeAgent` is missing `SetAgentRuntime(*AgentRuntime)`.

2. **Mismatched `IChannel` interface**: `channel.IChannel` has `Name() string` (no Get prefix), but `component.Component` uses `GetName()`. `ChannelManager` satisfies both correctly.

3. **AgentRuntime naming**: `agentcore/component.go`'s `*AgentRuntime` is the component wrapping struct, not the `IAgentCore` implementation. `IAgentCore` is the agent logic interface; `*AgentRuntime` wraps it as a `Component`.

4. **Native implementations are provided by users**: The framework does NOT ship native implementations. Native components are injected via `With*()` BuildOption functions. Stub natiive implementations (`NativeAgent`, `NativeProvider`, etc.) are for testing/demo only.

5. **Process* implementations use LazyTransport**: transports are auto-started on first `Call()`, not during `Initialize()` or `Start()`. If adding a new process implementation, wrap the transport with `client.WrapLazy()`.

## v0.2.0 Roadmap — Observability & Auxiliary Components

### Phase 1: Logger Component Infrastructure

- Add `go.uber.org/zap` dependency
- `pkg/logger/logger.go` — `Logger` interface, `LogEntry`, `Field`, `Level`
- `pkg/logger/zap.go` — `ZapLogger` wrapping `zap.Logger`; supports console (human-readable) and JSON formats
- `pkg/logger/component.go` — `LoggerComponent` implements `component.Component`
- Add `ComponentLogger` to `pkg/component/component.go`
- AgentRuntime: add `Logger` field, resolve from Registry in `Initialize()`, fallback to `logger.Default()` (zap stderr)
- YAML config supports `logger` component with `level`, `format` (json/console), `output`

### Phase 2: Transport Bidirectional Notifications

- Extend `Transport` interface with `OnNotify(method, handler)`
- `StdioTransport` / `HTTPTransport` / `LazyTransport` implement `OnNotify`
- Handshake extension: send `services/announce` notification after initialize
- Route `logger/log` notifications to Logger, `eventbus/publish` to EventBus

### Phase 3: Framework Instrumentation

- Transport `Call()` logs method + duration + error via Logger
- AgentRuntime `Initialize`/`Start`/`Run`/`Stop` emit structured log entries

### Phase 4: Sandbox Security Fields

- Extend `ResourceLimits` with `AllowedCommands` and `ReadOnlyRoot`
- Builder parses new fields from YAML

### Phase 5: Tests

- Logger component unit tests
- Transport notification round-trip tests (stdio + http)
- Sandbox field validation
