# Configuration Reference

This document explains the structure and fields of the Gogent application YAML configuration. A fully runnable example lives at [`config/example.yaml`](../../config/example.yaml).

## Top-Level Structure

```yaml
name: my-agent           # app name (required; the daemon uses it to register, look up ports, stop processes)
version: "1.0.0"         # app version
interface:               # user-facing interface
  type: cli              # currently implemented: cli; tui/http reserved
  cli:
    banner: "..."        # banner printed at startup
    prompt: "> "         # REPL prompt
provider:                # LLM providers (optional; see "provider section")
  exclude: ["deepseek"]  # exclude built-in engines
  servers:               # external suppliers (gRPC endpoint direct connection)
    - name: my-gateway
      endpoint: "localhost:9092"
observability:           # OpenTelemetry (optional)
  otel: { ... }
tools:                   # tool declarations (optional; see "tool declarations")
  - name: calculator
    securityLevel: 0
sandboxes:               # sandbox instance declarations (optional; see "sandboxes")
  - name: workspace
    profile: restricted-shell
default:                 # app-level default sandbox (optional)
  sandbox: workspace
components:              # component list (no provider type; see below)
  - name: agent-main
    type: agentcore
    driver: native
defaults:                # default instance name per component type
  agentcore: agent-main
```

## provider Section (LLM Providers)

Since v0.15.x providers no longer live in `components[]`; they are configured via the top-level `provider:` section. **Built-in engines are always registered** (default = all available), `exclude` removes some, and `servers` adds external suppliers:

```yaml
provider:
  exclude: ["deepseek"]        # optional: exclude built-in engines (openai, deepseek, OpenAI-compatible engines grow with releases)
  servers:                     # optional: external supplier list
    - name: "my-gateway"       # registration name (shown in /provider; must not collide with built-in engine names)
      endpoint: "localhost:9092"   # gRPC address of an externally running ProviderService
```

| Field | Description |
|-------|-------------|
| `exclude` | Blacklist: remove unwanted built-in engines; empty = all enabled |
| `servers[].name` | External supplier registration name. **Built-in engine names are reserved** (openai/deepseek cannot be used as server names, preventing `/provider openai` from silently routing to a remote) |
| `servers[].endpoint` | gRPC target (`host:port`) of the external ProviderService; the app connects directly, no daemon involvement |

> A `defaults` entry for the provider type is deprecated — the ProviderManager is the only provider component and auto-becomes the default. A leftover `defaults.provider` entry is ignored (with a stderr notice), not an error.

## components

Each component entry:

```yaml
- name: "component instance name"   # unique; referenced in defaults
  type: "component type"            # channel/agentcore/hook/eventbus/contextmanager/memory/sandbox/logger
  driver: "native"                  # native (in-process) | process (daemon subprocess) | http (remote gRPC)
  config: { ... }                   # type-specific configuration
  dependencies: { ... }             # optional: named component dependencies
```

> `type: provider` no longer belongs in components[] — configure it in the top-level `provider:` section (see above).

### agentcore

```yaml
- name: "agent-main"
  type: "agentcore"
  driver: "native"
  config:
    type: "react"            # agent type; react (full ReAct loop) is currently implemented; default = react
```

### Other Components

| Type | Key config fields |
|------|-------------------|
| channel | `bufferSize` |
| hook | `hooks` (list: name / driver / events: beforeRun/afterRun/beforeTool/afterTool/beforeLLM/afterLLM/error) |
| logger | `level` (info/debug/...), `format` (console/json), `output` |
| sandbox | `maxMemoryMB`, `networkAccess`, `allowedCommands`, `readOnlyRoot` |
| contextmanager / memory / eventbus | in-process implementations need no config; process/http drivers need `endpoint` |

## defaults (Default Instances)

```yaml
defaults:
  eventbus: eventbus-main
  logger: logger-main
  contextmanager: context-main
  memory: memory-main
  sandbox: sandbox-main
  agentcore: agent-main
```

Used by the Registry to pick the default instance per type. **Undeclared types** (e.g. logger/eventbus/memory/sandbox/contextmanager) get out-of-the-box framework defaults.

> **provider needs no defaults**: the ProviderManager is the only provider component and auto-becomes the default on registration. A leftover `defaults.provider` entry is ignored with a notice.

## tools (Tool Declarations)

The app declares which tools it wants — **declarative authorization; undeclared tools have no permission to execute** (least privilege):

```yaml
tools:
  - name: calculator        # tool name: built-in tool (calculator/think/todo) or MCP server name
    securityLevel: 0        # security level (0-2); risky/sensitive tools need a higher level
    sandbox: workspace      # optional: sandbox for execution (see "sandbox routing")
```

- Declaring a **builtin tool name** → directly usable (e.g. calculator)
- Declaring an **MCP server name** → the daemon expands it into `<server>.<tool>` sub-tools (e.g. `github-mcp.pull`)
- Undeclared tools are rejected at execution time (checked by `ManifestStore.IsAuthorized`)

## sandboxes

```yaml
sandboxes:
  - name: workspace              # sandbox instance name (referenced by tools[].sandbox)
    profile: restricted-shell    # references a profile from the daemon-side ~/.gogent/sandbox.yaml
    workDir: "/workspace/my-agent"
    allowedCommands: [ls, cat]

default:
  sandbox: workspace             # app-level default sandbox
```

### Sandbox Routing (Three-Level Fallback)

Priority when selecting a sandbox for a tool execution:

```
① tools[].sandbox explicit      →  use directly
② default.sandbox              →  app-level default
③ daemon defaults              →  default profile per tool driver (builtin/process)
```

## observability (OTel)

```yaml
observability:
  otel:
    enabled: true
    endpoint: "127.0.0.1:4317"   # OTLP gRPC address, or "console" (stdout debugging)
    service_name: "example-agent" # service name shown in Jaeger
    service_version: "0.15.x"    # optional
    environment: "dev"           # optional
    sample_rate: 1.0             # optional, 0.0-1.0, default full sampling
```

| Field | Default | Description |
|-------|---------|-------------|
| enabled | false | zero overhead when disabled (no-op tracer) |
| endpoint | — | OTLP gRPC address; `"console"` prints to stdout |
| service_name | — | service name in the observability backend |
| sample_rate | 1.0 | sampling rate |

> On Windows prefer `127.0.0.1` over `localhost`: gRPC dual-stack resolution can hang and cause export timeouts.

## interface (User Interface)

```yaml
interface:
  type: cli                    # currently implemented: cli; tui/http reserved
  cli:
    banner: "Welcome"          # startup banner
    prompt: "> "               # REPL prompt
```

Without `interface`, the app starts into a blocking event loop (no interactive UI), suitable for being hosted as a service by the daemon (`gogent serve`).

## Drivers

| driver | Meaning | Use Case |
|--------|---------|----------|
| native | in-process implementation | default recommended: react agent, logger, etc. |
| process | daemon-forked subprocess (gRPC communication) | when a component needs process isolation / its own failure domain |
| http | connects a remote gRPC service (via `endpoint`) | connecting a deployed remote component |

Daemon-managed component types (memory/contextmanager/agentcore) are shared across apps: the first forked instance is reused by later apps and reclaimed when the last app stops. External provider suppliers (`provider.servers`) connect directly via gRPC endpoint and bypass daemon forking.
