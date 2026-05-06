# AGENTS.md — Gogent

## Project at a Glance

- **Module**: `github.com/tltre/gagent`
- **Go**: 1.25.5
- **Version**: v0.4.2
- **Entrypoint**: `cmd/gagent/main.go` — expects a YAML config path as first argument
- **Deps**: `gopkg.in/yaml.v3` (direct), `github.com/mark3labs/mcp-go` (indirect), `go.uber.org/zap` (indirect), `github.com/spf13/cobra` (direct)
- **CI / lint / Makefile**: none

## Development Commands

```bash
go build ./...                              # build all packages
go vet ./...                                # static analysis
go test ./tests/native/ -v -timeout 30s     # native mock tests (13)
go test ./tests/stdio/ -v -timeout 60s      # stdio round-trip tests (13)
go test ./tests/http/ -v -timeout 30s       # http round-trip tests (9)
go test ./tests/integration/ -v -timeout 60s # full integration test (1)
go test ./tests/cli/ -v -timeout 30s        # CLI framework tests (19)
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

### BasicComponent (v0.3.1)

All component wrappers embed `component.BasicComponent` to share common fields:

```go
type BasicComponent struct {
    name string
    reg  *Registry
}

func (b *BasicComponent) GetName() string
func (b *BasicComponent) Registry() *Registry
func (b *BasicComponent) SetRegistry(r *Registry)
```

Embedding eliminates per-component `name`/`reg` fields and `GetName()` boilerplate. Ten component types: `ComponentChannel`, `ComponentAgentCore`, `ComponentProvider`, `ComponentTool`, `ComponentHook`, `ComponentEventBus`, `ComponentContextManager`, `ComponentMemory`, `ComponentSandbox`, `ComponentLogger`.

### Interface Layer (v0.4.0)

The **interface layer** is NOT a Component — it is a top-level abstraction that drives the application's interaction loop. It starts after all backend Components are started and blocks until user exit.

```go
// pkg/iface/iface.go
type Interface interface {
    Run(ctx context.Context, reg *component.Registry) error
}
```

**Key design decisions:**
- Not registered in Registry; not a `component.Component`
- Not a ComponentType; YAML driver dispatch does not apply
- `Run()` is blocking — replaces `<-ctx.Done()` in `App.Run()`
- Interface types are **mutually exclusive** per application instance

**Three implementations** (phased rollout):

| Type | YAML key | Description | Version |
|------|----------|-------------|---------|
| CLI | `type: cli` | cobra-based extensible command tree | v0.4.2 |
| TUI | `type: tui` | bubbletea interactive terminal | deferred |
| HTTP | `type: http` | embedded web server | deferred |

### v0.4.x Roadmap

```
v0.4.1 — 框架搭建
    ├── pkg/iface/iface.go          Interface 接口
    ├── pkg/app/config.go           追加 InterfaceConfig
    ├── pkg/app/builder.go          追加 buildInterface() + WithInterface()
    └── pkg/app/app.go              追加 iface 字段 + Run() 改造（iface != nil 时调 Run，else <-ctx.Done）

v0.4.2 — CLI 命令体系
    ├── pkg/iface/cli/              CLI 子包（命令树 + 注册/卸载 API）
    │   ├── cli.go                 DefaultCLI 结构体 + Register/Unregister/Run
    │   ├── command.go             CommandEntry + CommandBuilder + RegisterByPath
    │   ├── cmd_chat.go            默认命令: chat (交互式 REPL)
    │   ├── cmd_run.go             默认命令: run -p "..." (非交互单次调用)
    │   ├── cmd_version.go         默认命令: version
    │   ├── cmd_config.go          预制命令: config validate
    │   ├── cmd_tools.go           预制命令: tools (TODO)
    │   ├── cmd_providers.go       预制命令: providers (TODO)
    │   ├── cmd_sessions.go        预制命令: sessions (TODO)
    │   ├── cmd_memory.go          预制命令: memory (TODO)
    │   └── cmd_sandbox.go         预制命令: sandbox (TODO)
    └── pkg/app/builder.go         新增 WithCLICommand() BuildOption

v0.4.3 — TUI 实现
    ├── pkg/iface/tui.go            DefaultTUI (bubbletea)
    └── 依赖: github.com/charmbracelet/bubbletea + bubbles
    └── ⚠ deferred — 条件不成熟，待 Provider/AgentCore 核心流程稳定后再实现

v0.4.4 — HTTP 默认实现
    ├── pkg/iface/http.go           DefaultHTTP (net/http)
    └── 默认: iface.type 未配置时回退为 http
    └── ⚠ deferred — 条件不成熟，待 Provider/AgentCore 核心流程稳定后再实现
```

### CLI 命令体系设计 (v0.4.2)

**定位**: CLI 命令服务于**构建出的 agent 应用**的用户，非框架自身的 CLI。

**子包结构**: CLI 从 `pkg/iface/cli.go` 单文件重构为 `pkg/iface/cli/` 子包。

**默认命令** (3 个，DefaultCLI 内置，用户可替换/卸载):

| 命令 | 用法 | 场景 |
|------|------|------|
| `chat` | 无参数进入交互 REPL；root 默认子命令 | 交互式对话 |
| `run` | `run -p "..."` / `run --prompt "..."` | 脚本/CI 非交互调用 |
| `version` | `version` | 生产排障，确认版本信息 |

**预制命令** (框架提供实现，默认不注册，用户按需引入):

| 命令 | 依赖组件 | 说明 |
|------|---------|------|
| `config` / `config.validate` | 无 | 展示配置/校验 YAML |
| `tools` / `tools.call` | ToolManager | 列出工具/直接调用 |
| `providers` / `providers.test` | Provider | 列出 provider/探活 |
| `sessions` / `sessions.show` / `sessions.clear` | ContextManager | 会话管理 |
| `memory` / `memory.search` / `memory.clear` | Memory | 记忆管理 |
| `sandbox` / `sandbox.exec` | Sandbox | 沙箱管理 |

**命令存储: 扁平 map + 懒构建**:

```go
type DefaultCLI struct {
    commands     map[string]*cobra.Command    // "config.show" → *cobra.Command
    unregistered map[string]bool              // 记录显式移除的 path
}
```

- `Register(path, cmd)`: 写入 map，同名自动替换
- `Unregister(path)`: 删除 map 中 `path` 及其所有前缀匹配项 (`strings.HasPrefix(key, path+".") || key == path`)
- `buildRoot()`: Run 时从扁平 map 懒构建 cobra 命令树（O(n)，毫秒级）
- `ensureDefaults()`: 仅当 path 不在 map 且未被显式 unregister 时才注入默认命令

**用户扩展方式**:

```go
// Builder 注入
builder.Build(app.WithCLICommand(cli.CommandEntry{
    Path: "chat", Build: func(cli *cli.DefaultCLI) *cobra.Command { ... },
}))

// 启用预制命令
builder.Build(app.WithCLICommand(cli.ToolsCommands()...))
```

### Builder pattern

`app.Builder` reads YAML config → creates components → registers them → builds Interface → returns `*App`. Builder only handles `driver: "http"` and `driver: "process"`. Native (`driver: "native"`) components are skipped — they must be injected via `With*()` BuildOptions or provided by `componentDefaults` fallback.

**Priority chain**: `With*` injection > YAML config > `componentDefaults` fallback.

`registerDefaults()` provides out-of-box defaults for: Logger, EventBus, Memory, Sandbox.

The Interface layer follows similar priority: `WithInterface()` > YAML `interface.type` > none (falls back to `<-ctx.Done()` bare event loop).

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

All instrumentation uses direct `Logger.Log()` obtained from Registry:

- **AgentRuntime**: `Initialize`/`Start`/`Stop`/`Run` log via `log()` helper that routes to `reg.GetDefault(ComponentLogger).(logger.Logger).Log(ctx, entry)`.
- **Sub-components**: ToolManager, ProviderComponent, SandboxComponent, HookManager, MemoryComponent, ContextManagerComponent, EventBusComponent, ChannelManager — all log lifecycle (Init/Start/Stop) and key business methods with timing.
- **Transport**: `Call()` logs method + duration + success/failure via injected `client.Logger` (bridged to `logger.Logger` via `transportLogAdapter`).
- **LoggerComponent**: wraps a `logger.Logger` (default: `DefaultLogger` wrapping zap), implements `logger.Logger` interface via `Log()` delegation.
- **Remote daemon**: sends `logger/log` notification → main process `OnNotify` → routed to Logger.

`WithTraceID(ctx)` generates a per-request traceId carried in context. `DefaultLogger.Log()` auto-extracts traceId from context and appends it as a field.

### Registry Transparent Access (v0.3.0)

AgentRuntime no longer holds fixed fields for each subsystem. All dependency access goes through the Registry:

```go
type AgentRuntime struct {
    component.BasicComponent
    Agent IAgentCore
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

6. **Interface layer is NOT a Component**: `Interface` implementations do not go through Registry, do not have `Initialize`/`Start`/`Stop` lifecycle, and are not declared in YAML `components[]`. They are a top-level application concern, started by `App.Run()` after all Components are ready.

7. **Interface types are mutually exclusive**: Only one Interface implementation (CLI, TUI, or HTTP) runs per application instance. No multi-interface per instance.

8. **CLI commands via flat map + lazy tree**: `DefaultCLI` stores all commands (default + user-injected + prebuilt) in a flat `map[string]*cobra.Command` keyed by dot-separated path (`"config.show"`). The cobra command tree is lazily built via `buildRoot()` on each `Run()` call. Unregister uses prefix match on the flat map. `ensureDefaults()` skips paths already in the map or explicitly unregistered.
