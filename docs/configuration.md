# 配置参考

本文档逐项说明 Gogent 应用 YAML 配置的结构与字段含义。完整可运行示例见 [`config/example.yaml`](../config/example.yaml)。

## 顶层结构

```yaml
name: my-agent           # 应用名（必填；daemon 用它注册、查端口、停进程）
version: "1.0.0"         # 应用版本
interface:               # 用户交互界面
  type: cli              # 目前实现：cli；tui/http 预留
  cli:
    banner: "..."        # 启动时打印的横幅
    prompt: "> "         # REPL 提示符
provider:                # LLM 供应商（可选，见"provider 域"节）
  exclude: ["deepseek"]  # 排除内置引擎
  servers:               # 外部供应商（gRPC endpoint 直连）
    - name: my-gateway
      endpoint: "localhost:9092"
observability:           # OpenTelemetry 观测（可选）
  otel: { ... }
tools:                   # 工具声明（可选，见"工具声明"节）
  - name: calculator
    securityLevel: 0
sandboxes:               # 沙箱实例声明（可选，见"沙箱"节）
  - name: workspace
    profile: restricted-shell
default:                 # App 级默认沙箱（可选）
  sandbox: workspace
components:              # 组件列表（不含 provider 类型，见下）
  - name: agent-main
    type: agentcore
    driver: native
defaults:                # 各组件类型的默认实例名
  agentcore: agent-main
```

## provider 域（LLM 供应商）

v0.15.x 起 provider 不再出现在 `components[]` 中，由顶级 `provider:` 域统一配置。**内置引擎始终注册**（缺省 = 全部可用），`exclude` 排除，`servers` 追加外部供应商：

```yaml
provider:
  exclude: ["deepseek"]        # 可选：排除内置引擎（openai、deepseek、OpenAI 兼容引擎随版本扩展）
  servers:                     # 可选：外部供应商列表
    - name: "my-gateway"       # 注册名（/provider 中显示；不能与内置引擎名冲突）
      endpoint: "localhost:9092"   # 外部已运行的 ProviderService gRPC 地址
```

| 字段 | 说明 |
|------|------|
| `exclude` | 黑名单：排除不需要的内置引擎；留空 = 全部启用 |
| `servers[].name` | 外部供应商注册名。**内置引擎名为保留名**（openai/deepseek 不能用作 server 名，避免 `/provider openai` 静默路由到远端） |
| `servers[].endpoint` | 外部 ProviderService 的 gRPC target（`host:port`），App 直连，无需 daemon 介入 |

> provider 类型的 `defaults` 条目已被废弃——ProviderManager 是唯一的 provider 组件且自动成为默认。配置中残留 `defaults.provider` 会被忽略（stderr 提示），不再报错。

## components（组件）

每个组件条目：

```yaml
- name: "组件实例名"          # 唯一；defaults 中引用
  type: "组件类型"            # channel/agentcore/hook/eventbus/contextmanager/memory/sandbox/logger
  driver: "native"           # native（进程内）| process（daemon 子进程）| http（远程 gRPC）
  config: { ... }            # 类型相关配置
  dependencies: { ... }      # 可选，声明依赖的具名组件
```

> `type: provider` 不再属于 components[]——请在顶级 `provider:` 域配置（见上节）。

### agentcore

```yaml
- name: "agent-main"
  type: "agentcore"
  driver: "native"
  config:
    type: "react"            # agent 类型，目前实现 react（完整 ReAct 循环）；缺省为 react
```

### 其他组件

| 类型 | 关键 config 字段 |
|------|----------------|
| channel | `bufferSize` |
| hook | `hooks`（列表：name / driver / events：beforeRun/afterRun/beforeTool/afterTool/beforeLLM/afterLLM/error） |
| logger | `level`（info/debug/...）、`format`（console/json）、`output` |
| sandbox | `maxMemoryMB`、`networkAccess`、`allowedCommands`、`readOnlyRoot` |
| contextmanager / memory / eventbus | 进程内实现无需配置；process/http 驱动需 `endpoint` |

## defaults（默认实例）

```yaml
defaults:
  eventbus: eventbus-main
  logger: logger-main
  contextmanager: context-main
  memory: memory-main
  sandbox: sandbox-main
  agentcore: agent-main
```

Registry 按类型取默认实例时使用。**未声明的类型**（如 logger/eventbus/memory/sandbox/contextmanager）由框架提供开箱即用的默认实现。

> **provider 不需要 defaults**：ProviderManager 是唯一的 provider 组件，注册时自动成为默认。残留的 `defaults.provider` 条目会被忽略并提示。

## tools（工具声明）

App 声明要使用的工具——**声明式授权，未声明即无权执行**（最小权限）：

```yaml
tools:
  - name: calculator        # 工具名：内置工具（calculator/think/todo）或 MCP server 名
    securityLevel: 0        # 安全级别（0-2），越界/敏感工具需更高级别
    sandbox: workspace      # 可选：指定执行沙箱（见"沙箱路由"）
```

- 声明 **builtin 工具名** → 直接可用（如 calculator）
- 声明 **MCP server 名** → daemon 展开为 `<server>.<tool>` 子工具（如 `github-mcp.pull`）
- 未声明的工具在执行时被拒绝（`ManifestStore.IsAuthorized` 校验）

## sandboxes（沙箱）

```yaml
sandboxes:
  - name: workspace              # 沙箱实例名（tools[].sandbox 引用）
    profile: restricted-shell    # 引用 daemon 侧 ~/.gogent/sandbox.yaml 的 profile
    workDir: "/workspace/my-agent"
    allowedCommands: [ls, cat]

default:
  sandbox: workspace             # App 级默认沙箱
```

### 沙箱路由（三层 fallback）

工具执行时选择沙箱的优先级：

```
① tools[].sandbox 显式指定  →  直接使用
② default.sandbox          →  App 级默认
③ daemon defaults          →  按工具 driver 分类（builtin/process）的默认 profile
```

## observability（OTel）

```yaml
observability:
  otel:
    enabled: true
    endpoint: "127.0.0.1:4317"   # OTLP gRPC 地址，或 "console"（stdout 调试）
    service_name: "example-agent" # Jaeger 中显示的服务名
    service_version: "0.15.x"    # 可选
    environment: "dev"           # 可选
    sample_rate: 1.0             # 可选，0.0-1.0，默认全采样
```

| 字段 | 默认 | 说明 |
|------|------|------|
| enabled | false | 关闭时插桩零开销（no-op tracer） |
| endpoint | — | OTLP gRPC 地址；`"console"` 输出到 stdout |
| service_name | — | 观测后端中的服务名 |
| sample_rate | 1.0 | 采样率 |

> Windows 上建议用 `127.0.0.1` 而非 `localhost`：gRPC 双栈解析可能挂起导致导出超时。

## interface（用户界面）

```yaml
interface:
  type: cli                    # 目前实现 cli；tui/http 预留
  cli:
    banner: "Welcome"          # 启动横幅
    prompt: "> "               # REPL 提示符
```

未配置 `interface` 时，应用启动后进入阻塞事件循环（无交互界面），适合被 daemon 以服务方式托管（`gogent serve`）。

## 驱动（driver）说明

| driver | 含义 | 适用场景 |
|--------|------|---------|
| native | 进程内实现 | 默认推荐：react agent、logger 等 |
| process | daemon fork 的独立子进程（gRPC 通信） | 组件需进程隔离/独立故障域 |
| http | 连接远程 gRPC 服务（`endpoint` 指定） | 连接已部署的远程组件 |

daemon 管理的组件类型（memory/contextmanager/agentcore）可跨应用共享：先 fork 的实例被后续应用复用，最后一个应用停止时回收。外部 provider 供应商（`provider.servers`）为 gRPC endpoint 直连，不经过 daemon fork。
