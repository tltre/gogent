# Gogent — Go Agent Infrastructure

基于 Go 语言的 Agent 基础设施框架，提供模块化、可扩展的多模态 Agent 构建能力。

## 特性

- **模块化设计**: 9 个子系统以组件形式实现，可独立替换
- **三种驱动**: native（代码级注入）、process（stdio 子进程）、http（远程 HTTP），对外接口统一
- **依赖注入**: 自动管理组件依赖关系和拓扑排序初始化
- **配置驱动**: YAML 配置快速组装应用，BuildOption 注入自定义实现
- **Transport 抽象**: 统一 JSON-RPC 协议，基于 MCP transport 层，方法自定义

## 子系统

| 子系统 | 职责 | 驱动 |
|--------|------|------|
| channel | 用户消息接入渠道 | native / http / process |
| agentcore | Agent 执行逻辑与协调 | native / http / process |
| provider | LLM 提供者交互 | native / http / process |
| tool | 工具定义与管理 | native / http / process |
| hook | 钩子拦截与管理 | native / http / process |
| eventbus | 跨子系统异步消息 | native / http / process |
| contextmanager | 对话与执行上下文 | native / http / process |
| memory | 长期记忆存储 | native / http / process |
| sandbox | 安全沙箱执行 | native / http / process |

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
    "github.com/tltre/gagent/pkg/provider"
)

func main() {
    builder, err := app.NewBuilder("config.yaml")
    if err != nil {
        panic(err)
    }

    // 注入自定义 provider
    myProvider := &MyOpenAIProvider{}
    app, err := builder.Build(
        app.WithProvider("provider-main", myProvider),
    )
    if err != nil {
        panic(err)
    }

    ctx := context.Background()
    if err := app.Run(ctx); err != nil {
        panic(err)
    }
}
```

### 配置文件示例

```yaml
name: my-agent
version: "1.0.0"

components:
  # Provider — 本地注入
  - name: "provider-main"
    type: "provider"
    driver: "native"

  # Tool — 通过 stdio 连接远程 daemon
  - name: "tools-main"
    type: "tool"
    driver: "process"
    config:
      command: "./tool-daemon"
      env: ["LOG_LEVEL=debug"]

  # Memory — 通过 HTTP 连接远程服务
  - name: "memory-main"
    type: "memory"
    driver: "http"
    config:
      endpoint: "http://localhost:8080"
      timeout: "30s"

  # AgentCore
  - name: "agent-main"
    type: "agentcore"
    driver: "native"

defaults:
  provider: "provider-main"
  tool: "tools-main"
  memory: "memory-main"
  agentcore: "agent-main"
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
├── memory/              # IMemory
├── provider/            # IProvider
├── sandbox/             # ISandbox
└── tool/                # ITool + ToolManager
tests/                   # native / stdio / http / integration
```

## 许可证

MIT
