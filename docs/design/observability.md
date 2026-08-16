# 可观测性设计

> **文档状态**：本文档记录 Gogent 在 v0.14-v0.15 的架构演进与决策过程，包括方案对比、取舍理由与后续演进方向。代码与本文档如有出入，以代码为准。

本文档描述 Gogent 的可观测性体系：OpenTelemetry 插桩、span 结构、配置方式与网页观测。

## 一、架构总览

```
┌─ App 进程（gogent run，yaml 配置 otel）──────────────────────┐
│                                                             │
│  ReactAgent.Run                                             │
│    ├─ agent.run（ReAct 总流程 span）                         │
│    ├─ agent.llm.generate（每次 LLM 调用 span）               │
│    └─ tool.exec（每次工具调用 span）                         │
│        ├─ event: tool.auth      ← 鉴权阶段                   │
│        ├─ event: tool.hook      ← pre/post 钩子（可多轮）     │
│        └─ event: tool.result    ← 执行结果                   │
│                                                             │
│  ToolManager.Execute → gRPC → daemon 执行（otelgrpc 传播）    │
└──────────────────────────────────┬──────────────────────────┘
                                   │ OTLP gRPC (127.0.0.1:4317)
                                   ▼
                        Jaeger / Tempo / otel-collector
                                   │
                                   ▼
                        Jaeger 网页 (http://localhost:16686)
```

**核心设计决策**：工具调用观测完全由 **App 侧**承担——daemon 进程不导出 trace。原因：多 agent 守护架构下，多个 app 共享同一 daemon，每个 app 可能配置不同的 otel 后端；若 daemon 导出，其 span 只能发给单一 collector，导致部分 app 的 trace 断链。App 侧观测保证每个 app 的 trace 完整独立到自己的后端。

## 二、插桩点与 span 结构

### 2.1 Span 清单

| Span | 位置 | 属性 | 说明 |
|------|------|------|------|
| `agent.run` | `ReactAgent.Run` | provider / model / session / dur_ms | ReAct 总流程（根 span） |
| `agent.llm.generate` | ReAct 循环内 Generate | msg_count / dur_ms / tool_calls | 每次 LLM 调用 |
| `tool.exec` | `ToolManager.Execute` | tool / result.is_error / result.error / dur_ms | 每次工具调用（含完整双向交流） |

### 2.2 tool.exec 事件（双向交流审计）

工具执行是**双向多次交流**（`ExecuteTool` gRPC 流），每个阶段记录为 span 事件：

| 事件 | 阶段 | 属性 |
|------|------|------|
| `tool.auth` | 鉴权 | passed / reason |
| `tool.hook` | pre/post 钩子（可多轮） | stage / approved / reason |
| `tool.result` | 最终结果 | is_error / error_msg |

**观测效果**（Jaeger 中一条完整 trace）：

```
agent.run
├── agent.llm.generate  model=deepseek-v4-flash  tool_calls=1
├── tool.exec  tool=calculator  dur_ms=0.5
│   ├── event: tool.auth    passed=true
│   ├── event: tool.hook   stage=pre_execute  approved=true
│   ├── event: tool.hook   stage=post_execute approved=true
│   └── event: tool.result is_error=false
└── agent.llm.generate  tool_calls=0   ← 模型基于工具结果给出最终回答
```

一条 trace 即可证明：**工具被调用 → 鉴权/钩子通过 → daemon 实际执行 → 结果回填 → 模型继续推理**。

### 2.3 gRPC 传播

App → daemon 的 gRPC 调用通过 `otelgrpc` 自动传播 trace context：

- 客户端：`grpctransport/conn.go` — `otelgrpc.NewClientHandler()`
- 生成 `gogent.v1.ToolService/ExecuteTool` gRPC span（otelgrpc 自动），挂到 `tool.exec` 之下

daemon 侧**不导出** span：daemon 进程无 otel exporter，其内部执行细节不产生独立 trace；工具执行信息通过 gRPC 返回，由 App 侧 `tool.result` 事件记录。

## 三、配置

### 3.1 应用配置（yaml）

```yaml
observability:
  otel:
    enabled: true
    endpoint: "127.0.0.1:4317"   # OTLP gRPC，或 "console" 输出到 stdout
    service_name: "verify-agent"
    service_version: "0.15.x"    # 可选
    environment: "dev"           # 可选
    sample_rate: 1.0             # 可选，0.0-1.0，默认全采样
```

| 字段 | 默认 | 说明 |
|------|------|------|
| `enabled` | false | 关闭时插桩零开销（no-op tracer） |
| `endpoint` | — | OTLP gRPC 地址，或 `"console"`（stdout 调试） |
| `service_name` | — | Jaeger 中展示的服务名 |
| `service_version` | — | 可选版本标签 |
| `environment` | — | 部署环境标签 |
| `sample_rate` | 1.0 | 采样率（0.0-1.0） |

### 3.2 配置来源（多进程）

| 进程 | 配置来源 | 说明 |
|------|---------|------|
| **App** | yaml `observability.otel` | Builder 初始化（`InitFromConfig`） |
| **Daemon** | 无 | 不导出，无需配置 |

## 四、网页观测（Jaeger）

### 4.1 启动 Jaeger

```bash
docker run --rm -d --name jaeger \
  -e COLLECTOR_OTLP_ENABLED=true \
  -p 16686:16686 -p 4317:4317 \
  jaegertracing/all-in-one:latest
```

`COLLECTOR_OTLP_ENABLED=true` 必须设置，否则 Jaeger 不启用 OTLP 端口（4317）。

### 4.2 观测步骤

1. 启动 Jaeger（如上）
2. 启动应用：`./gogent.exe run config/provider-verify.yaml`
3. 打开 http://localhost:16686 → Search
4. 按 `service: verify-agent` 搜索，查看完整 trace

**验证点**（工具调用是否真实执行）：

| 观测项 | 预期 |
|--------|------|
| `agent.run` | 存在（ReAct 总流程） |
| `agent.llm.generate` | 多次（迭代轮数） |
| `tool.exec` + `gogent.v1.ToolService/ExecuteTool` | 存在（gRPC span 证明 daemon 侧实际执行） |
| `tool.auth` / `tool.hook` / `tool.result` 事件 | 完整（双向交流审计） |

### 4.3 本地调试（console）

```yaml
observability:
  otel:
    enabled: true
    endpoint: "console"   # span 输出到 stderr
```

无需 Jaeger，直接看 stdout 的 span 树。

## 五、多进程导出方案（App 侧观测）

### 5.1 约束

```
Daemon（单一进程）──────────► collector-A？
  ▲ gRPC                        ↑
App-A ──► collector-A          （daemon 只能发一个 collector）
App-B ──► collector-B          （App-B 的 trace 会断链！）
```

daemon 是单一进程、单一 TracerProvider——无法为不同 app 的请求动态切换 exporter。

### 5.2 决策

**采用 App 侧观测**：daemon 不导出，工具执行信息返回 App，由 `tool.exec` 记录。

| 方案 | 描述 | 结论 |
|------|------|------|
| A. 统一 collector | 所有进程指向同一 collector，trace 完整串联 | 可接受，但限制多 app 独立后端 |
| B. App 侧观测（采用） | daemon 只做 gRPC 传播；工具执行信息返回 App，由 `tool.exec` 记录 | 每 app trace 完整独立，天然支持多后端 |
| C. daemon 动态路由 exporter | daemon 按 app 切换 exporter | 复杂度高，不现实 |

### 5.3 收益

- **多 app 独立观测**：每个 app 的 trace 完整到自己的后端
- **审计完整**：`tool.exec` 事件覆盖鉴权、钩子、结果全流程
- **零 daemon 改动**：不引入 daemon 侧 exporter 生命周期管理

## 六、日志与 trace 关联

框架日志（`pkg/logger`）自带独立 traceId 机制：

```go
// pkg/logger — 每条日志自动携带 traceId
ctx = logger.WithTraceID(ctx)
// {"module": "agent-main", "traceId": "4c49366a...", "dur_ms": 91}
```

- **日志 traceId**：`logger.WithTraceID` 生成，随 context 传播，写入日志字段
- **otel trace**：OTLP span 体系
- 两者**独立但并行**：日志用于排障定位，span 用于链路观测；均随 context 传播

日志 traceId 与 otel traceID 目前**不关联**（两套 ID 生成）。后续可通过 `logger.WithTraceID` 从 otel span 提取 traceID 统一。

## 七、已知问题与排障

| 问题 | 原因 | 解决 |
|------|------|------|
| `traces export: context deadline exceeded` | Windows 上 `grpc.NewClient("localhost:...")` 双栈解析挂起 | endpoint 用 `127.0.0.1:4317`（显式 IPv4） |
| Jaeger 收不到 trace | 未设 `COLLECTOR_OTLP_ENABLED=true` | 启动容器时加该 env |
| `Tool names must be unique`（400） | YAML `tools:` 声明重复 | 检查配置去重（见 tool.md 命名冲突保护） |
| serviceName 显示为空（查询脚本） | Jaeger API 的 span.process 是 processID 引用 | 用 `processes[processID].serviceName` 查询 |

## 八、未来扩展

| 项 | 说明 |
|----|------|
| daemon 侧插桩 | 如需 daemon 内部细粒度 span（runner 内部步骤），需统一 collector（方案 A）或 daemon 独立 exporter |
| 审计日志 | 基于 `tool.exec` 事件可扩展：持久化工具调用审计 |
| 日志 trace 关联 | `logger.WithTraceID` 从 otel span 提取 traceID，统一两套 ID |
| 指标（metrics） | 目前仅 tracing；可扩展 OTLP metrics（调用次数、延迟分布、错误率） |

## 九、参考

- OTel Go SDK：`go.opentelemetry.io/otel`（tracing）、`otlptracegrpc`（导出）、`otelgrpc`（gRPC 传播）
- 内部基础设施：`internal/otel/` — `InitFromConfig` / `Tracer` / `SetTracerProvider`
- 插桩实现：`pkg/agentcore/react.go`（agent.run/agent.llm.generate）、`pkg/tool/manager.go`（tool.exec + 事件）
- 配置示例：`config/provider-verify.yaml`（仓库根目录）
- 关联设计：Agent Core（[agent.md](./agent.md)）、工具生态（[tool.md](./tool.md)）
