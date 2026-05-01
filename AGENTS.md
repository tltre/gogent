# AGENTS.md — Gogent

## Project at a Glance

- **Module**: `github.com/tltre/gagent` (README still says `github.com/yourorg/gagent` — use the go.mod value)
- **Go**: 1.25.5
- **Entrypoint**: `cmd/gagent/main.go` — expects a YAML config path as first argument
- **Deps**: `gopkg.in/yaml.v3` (direct), `github.com/mark3labs/mcp-go` (indirect)
- **Tests**: none yet
- **CI / lint / Makefile**: none

## Development Commands

```bash
go build ./...            # build all packages
go run ./cmd/gagent <config.yaml>   # run the app
go test ./...             # no tests yet, but this is the expected invocation
go vet ./...              # static analysis
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

A **Registry** (`pkg/component/registry.go`) holds components and does topological-sort initialization. Every component wrapping struct implements the `Component` interface and delegates to a "business interface" (e.g., `IMemory`, `IProvider`, `IAgentCore`).

### Component types are constants

Defined in `pkg/component/component.go`:
`ComponentChannel`, `ComponentAgentCore`, `ComponentProvider`, `ComponentTool`, `ComponentHook`, `ComponentEventBus`, `ComponentContextManager`, `ComponentMemory`, `ComponentSandbox`.

### Builder pattern

`app.Builder` reads YAML config → creates components → registers them → returns `*App`. The builder's `buildComponent` switch handles `"channel"`, `"agentcore"`, `"provider"`, `"tool"`, `"hook"`, `"eventbus"`, `"contextmanager"`, `"memory"`, `"sandbox"`.

Use `BuildOption` functions (`WithAgentCore`, `WithProvider`, etc.) to inject custom implementations. Some BuildOptions are still TODO stubs.

### YAML config format

See `config/example.yaml` for a complete example. Each component has `name`, `type`, `driver` (native/http/process), optional `config` map, optional `dependencies` map. `defaults` section sets which named component is default for each type.

The `agentcore` component is special — it depends on all other components and resolves them from the registry at Initialize time by calling `registry.GetDefault(type).(concreteInterface)`.

### Transport layer (`internal/client/`)

`Transport` is the abstraction for stdio/http communication between components and their remote daemons:

```go
type Transport interface {
    Start(ctx context.Context) error
    Close() error
    Call(ctx context.Context, method string, params any, result any) error
}
```

- **`StdioTransport`** wraps `mark3labs/mcp-go/transport.Stdio` for subprocess communication with JSON-RPC + Content-Length framing
- **Handshake**: simplified initialize exchange (`InitRequest`/`InitResponse`) with `componentType`, `protocolVersion`, and `methods`
- Method names are Gogent-defined (e.g., `"provider/generate"`, `"tools/call"`, `"memory/query"`), not MCP standard methods
- **No server/daemon side in this repo** — daemon will be provided by the future `gagent-sdk` repo

### Process driver

All 9 component types now support `driver: "process"` in YAML config. A `Process*` struct implements the component's business interface, internally using `Transport.Call()` to delegate everything to a remote daemon:

```
builder sees driver:"process"
  → creates StdioTransport from config (command, args, env)
  → creates ProcessProvider(transport)
  → wraps in ProviderComponent → Register

agentCore calls provider.Generate()
  → ProcessProvider.Generate()
    → transport.Call("provider/generate", msgs, &resp)
    → stdio → daemon → real IProvider.Generate()
```

Config example:
```yaml
- name: "provider-remote"
  type: "provider"
  driver: "process"
  config:
    command: "go"
    args: ["run", "./cmd/my-provider-daemon"]
    env: ["KEY=value"]
```

## Patterns & Conventions

- **Naming**: Component wrappers are created via `NewComponent(name, impl)` — not `NewXxxComponent`. Check each package for exact signature.
- **Driver dispatch**: Builder's `buildXxx` methods switch on `cc.Driver`. Supported values: `"native"`, `"http"`, `"process"`.
- **build* functions access config as raw `map[string]any`** — not through the typed config structs defined in `config.go`. The typed config structs (`ChannelConfig`, `ProviderConfig`, etc.) appear to be unused by the builder. Stay consistent if adding new fields.
- **Sentinel errors**: `pkg/component/errors.go` and `pkg/app/error.go` define sentinel errors — use `errors.Is()` when checking them.

## Gotchas

1. **`NativeAgent` doesn't implement `IAgentCore`**: `agentcore/native.go`'s `*NativeAgent` is missing `SetAgentRuntime(*AgentRuntime)`. If you need to use it as an `IAgentCore`, add the method.

2. **Mismatched `IChannel` interface**: `channel.IChannel` has `Name() string` (no Get prefix), but `component.Component` uses `GetName()`. The `ChannelManager` wrapping struct satisfies both correctly.

3. **Readme import paths**: The README uses `github.com/yourorg/gagent` throughout. Don't trust README code snippets — check imports against `go.mod`.

4. **AgentRuntime naming**: `agentcore/component.go`'s `*AgentRuntime` is the component wrapping struct, not the `IAgentCore` implementation. `IAgentCore` is the agent logic interface; `*AgentRuntime` wraps it as a `Component`.

5. **`contextmanager` http driver returns native**: Builder's `buildContextManager` http case returns the native component — no actual HTTP implementation exists for contextmanager.

6. **Native implementations are provided by users**: The framework does NOT ship native implementations of component interfaces. Native components are injected via `With*()` BuildOption functions. The stub native implementations in the repo (`NativeAgent`, `NativeProvider`, etc.) are for testing/demo only.
