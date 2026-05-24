# AGENTS.md — Gogent

## Project at a Glance

- **Module**: `github.com/tltre/gogent`
- **Go**: 1.25.5
- **Version**: v0.6.0-dev
- **Entrypoint**: `cmd/gogent/main.go` — cobra-based CLI (`gogent run/status/doctor/...`)
- **Binary**: `gogent`
- **Deps**: `gopkg.in/yaml.v3` (direct), `github.com/mark3labs/mcp-go` (indirect), `go.uber.org/zap` (indirect), `github.com/spf13/cobra` (direct)
- **CI / lint / Makefile**: none
- **Test helper**: `cmd/daemon/` — stdio daemon binary for end-to-end smoke testing

## Development Commands

```bash
go build ./...                              # build all packages
go vet ./...                                # static analysis
go test ./tests/native/ -v -timeout 30s     # native mock tests (13)
go test ./tests/stdio/ -v -timeout 60s      # stdio round-trip tests (13)
go test ./tests/http/ -v -timeout 30s       # http round-trip tests (9)
go test ./tests/e2e/ -v -timeout 60s        # full integration test (1)
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

### Management Channel (v0.5.0)

The **management channel** is a framework-internal HTTP server that runs alongside the agent application after all backend Components are started. It exposes a REST API for CLI/TUI/HTTP management tools to observe and interact with the running agent.

```go
// internal/mgmt/server.go
type Server struct { ... }
func Listen(addr string, reg *component.Registry) *Server  // goroutine
func (s *Server) Shutdown(ctx context.Context) error
```

**Key design decisions:**
- Runs inside `app.Run()` via goroutine (non-blocking alongside application Interface)
- Listens on localhost only (loopback)
- Does NOT import `pkg/iface/` — completely independent concern
- Placed in `internal/mgmt/` — not a public package, framework-internal only
- Port discovery via runtime file (`~/.gogent/<name>.port` or `./.gogent.pid`)
- `--port` flag on `gogent run` sets the listen address (default `9090`)

**HTTP API endpoints:**

| Endpoint | Description | v0.5.0 |
|----------|-------------|--------|
| `GET /api/v1/registry` | All components: name, type, status | ✅ |
| `GET /api/v1/health` | Per-component health checks | ✅ |
| `GET /api/v1/logs` | SSE real-time log stream | ✅ |
| `GET /api/v1/info` | Agent name, version, uptime | ✅ |
| `POST /api/v1/tools/exec` | Execute a tool directly | ✅ |
| `GET /api/v1/sessions` | Session list | ✅ |

### v0.5.0 Roadmap

The framework CLI (`gogent`) targets DevOps/operations workflows — managing and observing a running agent application (analogous to `k9s` for Kubernetes or `rabbitmqadmin` for RabbitMQ).

```
v0.5.0 — 框架管理与运维 CLI
    ├── cmd/gogent/                  框架二进制入口
    │   ├── main.go                  cobra root command + --port 全局 flag
    │   └── cmd/
    │       ├── root.go              root command + global flags
    │       ├── run.go               run <config> [--port] [-i]
    │       ├── serve.go             serve <config> [--port] — 后台服务模式
    │       ├── status.go            status — 组件状态表
    │       ├── doctor.go            doctor — 逐一健康检查
    │       ├── logs.go              logs [--follow] — 实时日志流
    │       ├── validate.go          validate <config> — 离线 YAML 校验
    │       ├── inspect.go           inspect <config> — 离线配置展示
    │       └── version.go           version — 框架版本信息
    └── internal/mgmt/
        ├── server.go                HTTP management server
        ├── handler.go               REST API handlers
        ├── client.go                HTTP client for CLI subcommands
        ├── types.go                 shared response types
        └── port.go                  runtime port file read/write
```

**Framework CLI command tree:**

```
gogent [--port 9090] <command> [args]

  serve     <config>               前台 server 模式（daemon）
  run       <config> [-i]         启动 agent + HTTP mgmt server
  status                           组件状态表
  doctor                           逐一健康检查
  logs      [--follow]             实时日志流 (SSE)
  validate  <config>               离线校验 YAML
  inspect   <config>               离线展示解析后配置
  version                          框架版本

--port  flag: 指定管理端口 (默认 9090)，数字格式自动加冒号前缀
                 后续 status/doctor/logs 自动读取运行时端口文件
-i   flag: 启动 TUI 模式 (v0.5.0 占位)
```

**三层体系:**

```
┌──────────────────────────────────────────────┐
│ 框架 CLI (v0.5.0)                            │
│ gogent run | serve | status | doctor | logs   │
│ 管理运行中的 agent，通过 internal/mgmt 交互    │
├──────────────────────────────────────────────┤
│ 管理通道 (v0.5.0)                            │
│ internal/mgmt/ — localhost HTTP server        │
│ /api/v1/registry | health | logs | info       │
│ /api/v1/tools/exec | sessions                 │
├──────────────────────────────────────────────┤
│ 应用接口 (v0.4.2)                            │
│ pkg/iface/ — 面向终端用户的 CLI/TUI/HTTP      │
└──────────────────────────────────────────────┘
```

### v0.6.0 Roadmap — 多 Agent 守护进程管理

The framework CLI expands from single-agent management to multi-agent lifecycle management. A central **daemon process** tracks all agent instances, assigns ports, and monitors child process health. All CLI commands (serve/run/stop/list/status) interact with the daemon.

**Architecture:**

```
┌──────────────────────────────────────────────┐
│ gogent daemon (守护进程，:9090)                │
│ Daemon 端点 (仅 agent 生命周期)                │
│ POST /api/v1/agents         加载 agent        │
│ GET  /api/v1/agents         列出所有 agent     │
│ DELETE /api/v1/agents/{n}   停止 agent         │
│                                              │
│ agent registry (in-memory)                    │
│ ┌──────────┬──────────┐                      │
│ │ agent-a  │ agent-b  │  独立 OS 子进程        │
│ │ :9091    │ :9092    │  各自的 Registry/iface │
│ └──────────┴──────────┘                      │
└──────────────────────────────────────────────┘
```

**Key design decisions:**
- Daemon auto-starts on first `serve`/`run` call — transparent to user
- Agents are independent OS sub-processes, not goroutines — full process isolation
- Daemon crash does NOT affect running agents — recoverable via PID probing
- Per-agent mgmt endpoints (registry/health/logs) remain on each agent's own port
- Port allocation: daemon assigns from a free-port pool, `--port` overrides

**Two mgmt endpoint layers:**

| Layer | Endpoints | Provider |
|-------|-----------|----------|
| Daemon | `POST/GET /api/v1/agents`, `DELETE /api/v1/agents/{name}` | daemon process |
| Agent | `/api/v1/registry`, `/health`, `/logs`, `/info`, `/tools/exec`, `/sessions` | each agent process |

**CLI command tree:**

```
gogent daemon  [--port 9090]               显式启动守护进程
gogent serve   <config> [--port 9090]      启动 agent（自动启 daemon），立即返回
gogent run     <config> [--port 9090]      启动 agent + attach REPL（前台）
gogent stop    <name>                      停止指定 agent
gogent restart <name>                      stop + serve
gogent list                                列出所有 agent（NAME/PORT/PID/STATUS）
gogent status  [name]                      无参=list；有参=查 agent 组件表
gogent doctor  [name]                      agent 健康检查
gogent logs    [name] [--follow]           agent 日志流
gogent validate/inspect/version            不变
```

**Key flows:**

```
gogent serve agent-a.yaml
  → check daemon alive → if not: fork gogent daemon --port 9090
  → POST /api/v1/agents {config:"agent-a.yaml"}
  → daemon: allocate port → fork gogent agent --config ... --port 9091
  → wait for agent ready → register in memory → return {name, port, pid}

gogent stop agent-a
  → DELETE /api/v1/agents/agent-a
  → daemon: SIGTERM child process → remove from registry

gogent status agent-a
  → GET /api/v1/agents → find agent-a port=9091
  → GET :9091/api/v1/registry → print component table
```

**Internal subcommand: `gogent agent`** — hidden from help, used only by daemon to fork child processes. Takes `--config`, `--port`, `--daemon` flags.

```
v0.6.0 — 多 Agent 守护进程管理
    ├── cmd/gogent/cmd/
    │   ├── daemon.go              守护进程入口
    │   ├── agent.go               内部子命令（daemon fork 用）
    │   ├── list.go                列出所有 agent
    │   ├── stop.go                停止 agent
    │   ├── restart.go             重启 agent
    │   ├── serve.go               重写 — 连 daemon 加载 agent
    │   ├── run.go                 重写 — 同上 + attach REPL
    │   ├── status.go              修改 — 支持 [name] 参数
    │   ├── doctor.go              修改 — 支持 [name] 参数
    │   └── logs.go                修改 — 支持 [name] 参数
    └── internal/mgmt/
        ├── daemon.go              新建 — agent 注册表、fork、PID 监控
        ├── server.go              修改 — 新增 daemon 层 agents 端点
        ├── client.go              修改 — LoadAgent / ListAgents / StopAgent
        ├── port.go                修改 — 自动分配空闲端口
        └── types.go               修改 — AgentInfo 类型
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

9. **管理通道放在 `internal/mgmt/`**: 不是 `pkg/` 下的公共包。CLI 子命令通过 HTTP client 连接已在运行的 agent 的管理端口。端口发现通过 `--port` flag 指定，写入运行时文件供后续命令自动读取。

10. **Two distinct CLI layers**: The **application CLI** (`pkg/iface/cli/`) serves end-user interaction (chat/run/version); the **framework CLI** (`cmd/gogent/cmd/`) serves DevOps management (status/doctor/logs). They operate at different layers and do not overlap.

<!-- CODEGRAPH_START -->
## CodeGraph

This project has a CodeGraph MCP server (`codegraph_*` tools) configured. CodeGraph is a tree-sitter-parsed knowledge graph of every symbol, edge, and file. Reads are sub-millisecond and return structural information grep cannot.

### When to prefer codegraph over native search

Use codegraph for **structural** questions — what calls what, what would break, where is X defined, what is X's signature. Use native grep/read only for **literal text** queries (string contents, comments, log messages) or after you already have a specific file open.

| Question | Tool |
|---|---|
| "Where is X defined?" / "Find symbol named X" | `codegraph_search` |
| "What calls function Y?" | `codegraph_callers` |
| "What does Y call?" | `codegraph_callees` |
| "What would break if I changed Z?" | `codegraph_impact` |
| "Show me Y's signature / source / docstring" | `codegraph_node` |
| "Give me focused context for a task/area" | `codegraph_context` |
| "See several related symbols' source at once" | `codegraph_explore` |
| "What files exist under path/" | `codegraph_files` |
| "Is the index healthy?" | `codegraph_status` |

### Rules of thumb

- **Answer directly — don't delegate exploration.** For "how does X work" / architecture / trace questions, answer with 2-3 codegraph calls: `codegraph_context` first, then ONE `codegraph_explore` for the source of the symbols it surfaces. Codegraph IS the pre-built index, so spawning a separate file-reading sub-task/agent — or running a grep + read loop — repeats work codegraph already did and costs more for the same answer.
- **Trust codegraph results.** They come from a full AST parse. Do NOT re-verify them with grep — that's slower, less accurate, and wastes context.
- **Don't grep first** when looking up a symbol by name. `codegraph_search` is faster and returns kind + location + signature in one call.
- **Don't chain `codegraph_search` + `codegraph_node`** when you just want context — `codegraph_context` is one call.
- **Don't loop `codegraph_node` over many symbols** — one `codegraph_explore` call returns several symbols' source grouped in a single capped call, while each separate node/Read call re-reads the whole context and costs far more.
- **Index lag**: the file watcher debounces ~500ms behind writes; don't re-query immediately after editing a file in the same turn.

### If `.codegraph/` doesn't exist

The MCP server returns "not initialized." Ask the user: *"I notice this project doesn't have CodeGraph initialized. Want me to run `codegraph init -i` to build the index?"*
<!-- CODEGRAPH_END -->

## Commit Message Conventions

### DO NOT reference gitignored paths in commit messages

Paths excluded by `.gitignore` (e.g., `tests/`, `.sisyphus/`) are not part of the repository.
Referring to them in commit messages creates misleading history — a future developer reading
the log has no way to find those paths in the checked-out tree.

**Wrong**: `Migrate cmd/smokeotel to tests/e2e/demo`
→ `tests/e2e/demo` does not exist in git, the message refers to untracked content.

**Right**: Describe the logical change instead.
`Add e2e test restructuring, demo app` or simply omit gitignored paths from the summary.
