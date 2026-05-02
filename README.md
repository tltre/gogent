# Gogent — Go Agent Infrastructure

基于 Go 语言的 Agent 基础设施框架，提供模块化、可扩展的 Agent 构建能力。

## 特性

- **模块化设计**: 10 个子系统以组件形式实现，可独立替换
- **驱动分离**: process（stdio 子进程）、http（远程 HTTP），对外接口统一；native 由用户注入
- **依赖注入**: 自动管理组件依赖关系和拓扑排序初始化
- **配置驱动**: YAML 配置快速组装应用，BuildOption 注入自定义实现
- **Transport 抽象**: 统一 JSON-RPC 协议，基于 MCP transport 层，方法自定义
- **可观测性**: EventBus 日志管道 + zap 结构化日志，traceId 追踪
- **Fallback 机制**: builder 优先用户注入 → YAML 配置 → 框架默认实现

## 子系统

| 子系统 | 职责 | 驱动 |
|--------|------|------|
| channel | 用户消息接入渠道 | http / process |
| agentcore | Agent 执行逻辑与协调 | http / process |
| provider | LLM 提供者交互 | http / process |
| tool | 工具定义与管理 | http / process |
| hook | 钩子拦截与管理 | http / process |
| eventbus | 跨子系统异步消息 | http / process |
| contextmanager | 对话与执行上下文 | http / process |
| memory | 长期记忆存储 | http / process |
| sandbox | 安全沙箱执行 | http / process |
| logger | 日志组件 | 内置 default |

## 快速开始

### 安装

```bash
go get github.com/tltre/gagent
```

### 基本使用

```go
package main

import (
    "context"
    "github.com/tltre/gagent/pkg/app"
)

func main() {
    builder, err := app.NewBuilder("config.yaml")
    if err != nil {
        panic(err)
    }

    application, err := builder.Build()
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    if err := application.Run(ctx); err != nil {
        panic(err)
    }
}
```

### 配置文件示例

```yaml
name: my-agent
version: "1.0.0"

components:
  # Provider — 通过 stdio 连接远程 daemon
  - name: "provider-main"
    type: "provider"
    driver: "process"
    config:
      command: "./provider-daemon"
      env: ["KEY=value"]

  # Memory — 通过 HTTP 连接远程服务
  - name: "memory-main"
    type: "memory"
    driver: "http"
    config:
      endpoint: "http://localhost:8080"
      timeout: "30s"

  # Sandbox — 安全沙箱
  - name: "sandbox-main"
    type: "sandbox"
    driver: "process"
    config:
      command: "./sandbox-daemon"
      allowedCommands: ["python3", "node"]
      readOnlyRoot: true

  # Logger — 框架自带 default，也可通过 YAML 定制
  - name: "logger-main"
    type: "logger"
    config:
      level: "info"
      format: "json"

defaults:
  provider: "provider-main"
  memory: "memory-main"
  sandbox: "sandbox-main"
  logger: "logger-main"
```

## 项目结构

```
cmd/gagent/              # 入口
config/example.yaml      # 配置示例
internal/client/         # Transport 接口 + StdioTransport + HTTPTransport + LazyTransport
pkg/
├── component/           # Component 接口 + Registry + 拓扑排序
├── app/                 # Builder + Config + App 生命周期
├── agentcore/           # IAgentCore + AgentRuntime 组件包装器
├── channel/             # IChannel + ChannelManager
├── contextmanager/      # IContextManager
├── eventbus/            # IEventBus
├── hook/                # IHook + HookManager
├── logger/              # Logger 接口 + DefaultLogger (zap)
├── memory/              # IMemory
├── provider/            # IProvider
├── sandbox/             # ISandbox + DefaultSandbox
└── tool/                # ITool + ToolManager
tests/                   # native / stdio / http / integration
```

## 许可证

MIT
