# Gogent — Go Agent Infrastructure

**Gogent 是一个基于 Go 的 Agent 应用运行基础设施（harness）。** 它不替你写 agent 业务逻辑，而是提供一套可插拔的组件系统、多引擎 LLM Provider、ReAct Agent 循环、多应用守护进程管理、中心化工具注册表与沙箱隔离，让你用一份 YAML 声明式地组装、运行和运维 agent 应用。

```
go get github.com/tltre/gogent
```

## 特性

- **模块化组件架构**：9 个子系统以组件形式实现（channel / agentcore / provider / hook / eventbus / contextmanager / memory / sandbox / logger），YAML 声明式组装，每个组件可插拔、可替换、可多实例
- **多引擎 LLM Provider**：内置 OpenAI、DeepSeek 原生引擎，OpenAI 兼容格式（Groq / Mistral / Ollama / vLLM 等 10+ 家）通过统一基类接入；运行时 `/provider` `/model` 自由切换，模型列表动态拉取、永不硬编码
- **React Agent**：完整 ReAct 循环（推理 → 工具调用 → 观察 → 迭代），工具声明自动注入、结果回填、错误自纠正、迭代上限保护、上下文与记忆持久化
- **多应用守护进程管理**：独立 OS 子进程隔离，daemon 统一分配端口、监控生命周期、崩溃自动检测与恢复、daemon 崩溃后代理进程不受影响
- **中心化 Tool Registry**：工具统一由 daemon 注册与鉴权，App 通过 manifest 声明式使用（最小权限）；应用级 securityLevel + 全局规则双重管控
- **MCP 工具生态**：process / http 双 MCP server，子工具自动展开（`server.tool` 命名），协议级 Ping 健康检查 + 连续失败自动重启
- **沙箱隔离工具执行**：E2B（MicroVM）/ shell / filesystem 路由，三层 fallback（per-tool → app 默认 → daemon 默认），工具执行与凭证解析在 daemon 侧完成
- **OpenTelemetry 可观测**：`agent.run` → `agent.llm.generate` → `tool.exec` 三级 span，工具执行全流程（鉴权/钩子/结果）事件化记录，OTLP gRPC 导出到 Jaeger/Tempo
- **凭证隔离**：app-scoped `credentials.yaml`（`~/.gogent/apps/<name>/`），REPL 内 `/key` 交互式配置，密钥脱敏显示，设置即生效无需重启

## 架构总览

```mermaid
flowchart TB
    subgraph CLI["用户 CLI"]
        C1["gogent run / serve / stop / list<br/>status / doctor / logs / tool / sandbox"]
    end

    subgraph DAEMON["Daemon 进程（localhost 守护进程）"]
        direction TB
        D1["AppStore / ComponentStore<br/>端口分配 · 健康检查 · 崩溃恢复"]
        D2["ToolRegistry + MCP Runner<br/>builtin · process · http + Lifecycle"]
        D3["SandboxManager<br/>E2B provider + profiles"]
        D1 --- D2
        D1 --- D3
    end

    subgraph APP["Agent App 进程（每应用独立 OS 子进程）"]
        direction TB
        R["Registry（组件拓扑排序初始化）"]
        AC["AgentCore（react）<br/>ReAct 循环"]
        PM["ProviderManager<br/>openai / deepseek / openai-compat 10+"]
        TS["tool.Service<br/>（List/Execute 薄客户端）"]
        CS["CredentialStore<br/>（app-scoped 凭证）"]
        IF["Interface（CLI REPL）<br/>chat · /key · /provider · /model"]
        R --> AC
        R --> PM
        R --> TS
        R --> CS
        AC --> TS
        AC --> PM
        IF --> AC
    end

    CLI -->|"HTTP API (:9090)"| DAEMON
    DAEMON -->|"fork 子进程 / 监控"| APP
    APP -->|"gRPC ExecuteTool (tool.exec span)"| D2
    APP -->|"OTLP gRPC"| OTEL["Jaeger / Tempo"]
```

## 子系统

| 子系统 | 职责 | 驱动 |
|--------|------|------|
| channel | 用户消息接入渠道 | native / process / http |
| agentcore | Agent 执行逻辑与协调（react ReAct 循环） | native / process / http |
| provider | LLM 提供者交互（ProviderManager 统一管理） | native（引擎注册表） / process / http |
| hook | 钩子拦截与管理（before/after LLM、工具执行） | native / process / http |
| eventbus | 跨子系统异步消息 | native / process / http |
| contextmanager | 会话上下文（历史 + 记忆组装唯一输入源） | native / process / http |
| memory | 长期记忆存储 | native / process / http |
| sandbox | 沙箱执行（app 侧资源限制声明） | native / process / http |
| logger | 结构化日志（zap） | native |

> v0.12.2 起 `tool` 不再是 registry 组件——工具统一由 daemon 的 ToolRegistry 管理，App 侧通过 `tool.Service` 薄客户端访问。

## 已实现功能

| 功能 | 能力说明 | 如何验证 |
|------|---------|---------|
| 组件系统 | 9 类组件、Registry 拓扑排序、`With*` 注入 > YAML > 默认实现三级优先级 | `gogent run config/example.yaml` 后 `gogent status` 查看组件状态表 |
| 多引擎 Provider | `openai` / `deepseek` 原生引擎 + OpenAI 兼容基类；模型列表动态拉取（10 分钟缓存） | REPL 内 `/provider` 列出全部引擎与模型，`/provider deepseek` 即时切换 |
| ReAct Agent | 工具声明注入 → 迭代（上限 10 轮）→ 结果回填 → 错误自纠正 → 工具交互存档至 Memory | 在 chat 中提问需要工具计算的问题，观察多轮迭代与最终回答 |
| 多应用管理 | daemon fork 独立子进程、端口分配、崩溃检测、daemon 崩溃恢复（PID 探测） | `gogent serve <config>` + `gogent list`；kill 掉 agent 进程后观察 `gogent doctor` |
| 工具注册表 | builtin（calculator/think/todo）+ MCP server 子工具展开；manifest 声明式授权；命名冲突保护 | `gogent tool list` 查看所有工具与状态；App 未声明的工具执行被拒绝 |
| MCP 生态 | process（stdio）/ http（streamable）双驱动；MCP Ping 30s 探活；连续 3 次失败自动重启 | `gogent tool status <server>` 观察 ACTIVE/UNHEALTHY 状态迁移 |
| 沙箱隔离 | E2B MicroVM + profiles + per-tool 路由；凭证经 `internal/credentials` 统一解析 | `gogent sandbox status`；配置 E2B key 后执行 shell 工具观察路由到沙箱 |
| OTel 可观测 | 三级 span + 工具执行审计事件；OTLP gRPC 导出 | 启动 Jaeger（`docker run -p 16686:16686 -p 4317:4317 jaegertracing/all-in-one`）后按 `service: example-agent` 搜索 trace |
| 凭证管理 | app 作用域 `credentials.yaml`、`/key` 交互配置、密钥脱敏、CredentialStore 可替换（Vault/keyring） | REPL 内 `/key openai sk-...` 保存后直接使用，`/key` 查看脱敏状态 |

## 快速开始

### 安装

```bash
go get github.com/tltre/gogent
```

### 基本使用

```go
package main

import (
    "context"
    "fmt"

    "github.com/tltre/gogent/pkg/agentcore"
    "github.com/tltre/gogent/pkg/app"
    "github.com/tltre/gogent/pkg/provider"
)

func main() {
    builder, err := app.NewBuilder("config/example.yaml")
    if err != nil {
        panic(err)
    }

    // 可选：以 BuildOption 注入自定义实现（优先级高于 YAML 配置）
    application, err := builder.Build(
        app.WithProvider("openai", provider.NewOpenAI(provider.OpenAIConfig{})),
        app.WithAgentCore("agent-main", agentcore.NewReactAgent()),
    )
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    if err := application.Run(ctx); err != nil {
        fmt.Println("run:", err)
    }
}
```

### 配置文件示例

完整的可运行示例见 [`config/example.yaml`](config/example.yaml)（OpenAI + DeepSeek 双引擎、react agent、OTel 观测）：

```yaml
name: example-agent
version: "1.0.0"

interface:
  type: cli
  cli:
    banner: "Gogent Example Agent — use /provider and /model to switch"
    prompt: "> "

observability:
  otel:
    enabled: true
    endpoint: "127.0.0.1:4317"   # OTLP gRPC（Jaeger/Tempo 默认端口）
    service_name: "example-agent"
    environment: "dev"

tools:
  - name: calculator
    securityLevel: 3
  - name: think
    securityLevel: 0
  - name: todo
    securityLevel: 0

components:
  # native provider：默认注册全部内置引擎，exclude 排除不需要的
  - name: "provider-main"
    type: "provider"
    driver: "native"

  # react agentcore：完整 ReAct 循环（推理 → 工具调用 → 观察 → 迭代）
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
```

## 快速体验

```bash
# 构建框架 CLI
go build -o gogent.exe ./cmd/gogent

# 启动 agent（自动拉起 daemon，进入交互式 chat REPL）
./gogent.exe run config/example.yaml
```

进入 REPL 后：

```
> /key openai sk-你的OPENAIkey     # 交互式配置凭证（写入 app 作用域 credentials.yaml）
> /key                             # 查看全部凭证状态（密钥脱敏）
> /provider                        # 列出所有可用引擎与模型
> /provider deepseek               # 运行时切换引擎
> /model deepseek-reasoner         # 切换当前引擎的模型
> 帮我计算 1234 * 5678             # 触发 ReAct 工具调用（calculator）
> quit
```

## 与同类项目对比

### vs deepseek-harness

| 维度 | Gogent | deepseek-harness |
|------|--------|------------------|
| 语言 | Go（单二进制、静态类型、天然并发） | TypeScript |
| 定位 | 多应用 agent 运维基础设施（daemon 管理 + 工具生态 + 沙箱） | 单实例 agent 插件框架 |
| 多应用 | ✅ daemon 进程隔离管理多个 agent，端口自动分配、崩溃恢复 | ❌ 单实例运行 |
| 进程模型 | agent 独立 OS 子进程，daemon 崩溃不影响运行中 agent | 进程内运行 |
| 工具生态 | MCP process/http 双驱动 + 中心化注册表 + 沙箱路由 | 插件机制 |

### vs LangGraph

| 维度 | Gogent | LangGraph |
|------|--------|-----------|
| 定位 | Agent 应用运行基础设施（harness：组件、进程、工具、沙箱、观测） | Agent 开发库（状态图、编排原语） |
| 关注层 | 应用装配与运维：YAML 声明组件、daemon 管理生命周期、中心化工具 | 图编排：节点、边、状态、checkpoint |
| LLM 供应商 | 内置多引擎 + OpenAI 兼容 10+ 家，运行时切换 | 依赖第三方 provider 库 |
| 工具执行 | daemon 侧统一鉴权/沙箱/审计，App 最小权限 | 进程内函数调用 |
| 进程管理 | daemon + 子进程隔离 | 无 |

Gogent 是"运行 agent 应用的操作系统"，LangGraph 是"编写 agent 逻辑的开发库"——两者解决不同层级的问题。

## 项目结构

```
cmd/gogent/              # 框架 CLI 入口（run/serve/stop/list/status/doctor/logs/tool/sandbox/daemon...）
config/                  # 配置示例（example.yaml）
internal/
├── api/                 # HTTP API 路径与共享类型
├── credentials/         # 统一凭证解析（credentials.yaml + ${VAR} + fsnotify 热加载）
├── daemon/              # 守护进程：AppStore/ComponentStore/端口分配/健康检查/崩溃恢复
│   ├── tool/            #   ToolRegistry + MCP Runner + Lifecycle（Ping 探活/自动重启）
│   └── sandbox/         #   SandboxManager（E2B provider + profiles + per-tool 路由）
├── grpctransport/       # gRPC 连接池 + ToolService proto（otelgrpc 传播）
├── mgmt/                # agent 级管理 HTTP server（registry/health/logs/info/tools/sessions）
└── otel/                # OpenTelemetry 初始化（InitFromConfig + Tracer）
pkg/
├── component/           # Component 接口 + Registry + 拓扑排序 + BasicComponent
├── app/                 # Builder（NewBuilder/Build/With* BuildOption）+ Config + App 生命周期
├── agentcore/           # IAgentCore + AgentRuntime 组件包装 + ReactAgent（ReAct 循环）
├── channel/             # IChannel + ChannelManager
├── contextmanager/      # IContextManager（会话历史 + BuildInput 唯一输入源）
├── eventbus/            # IEventBus + DefaultEventBus
├── hook/                # IHook + HookManager
├── logger/              # Logger 接口 + DefaultLogger（zap）
├── memory/              # IMemory + DefaultMemory
├── provider/            # IProvider + ProviderManager + 引擎注册表 + OpenAI/DeepSeek 引擎 + CredentialStore
├── sandbox/             # ISandbox + DefaultSandbox（app 侧资源限制声明）
├── iface/               # Interface 层（CLI REPL：chat/run/version + /key /provider /model）
└── tool/                # ToolManager（daemon 薄客户端）+ tool.Service 接口
tests/                   # native（单元）/ cli（CLI 测试）/ e2e（端到端，需外部服务）/ integration
docs/                    # 使用者指南（quickstart/configuration/providers/agents）+ design/ 设计文档
```

## 文档

- 使用者指南：[快速开始](docs/quickstart.md) · [配置参考](docs/configuration.md) · [供应商接入](docs/providers.md) · [Agent 类型](docs/agents.md)
- 设计文档（架构决策记录）：[docs/design/](docs/design/)

## 许可证

MIT
