# Gogent - Go IAgentCore Infrastructure

基于 Go 语言的 IAgentCore 基础设施框架，提供模块化、可扩展的 IAgentCore 构建能力。

## 特性

- **模块化设计**: 所有子系统以组件形式实现，可独立替换
- **多种驱动**: 支持 native（代码实现）、HTTP（远程调用）、Process（MCP 协议）
- **依赖注入**: 自动管理组件依赖关系和初始化顺序
- **配置驱动**: 通过 YAML 配置快速组装应用
- **横向扩展**: 预留多实例支持，支持组合和故障转移

## 子系统

| 子系统 | 职责 | 驱动支持 |
|--------|------|----------|
| channel | 用户消息接入渠道 | native, http, process |
| agentcore | IAgentCore 执行逻辑与协调 | native, http |
| provider | LLM 提供者交互 | native, http |
| tool | 工具定义与管理 | native, http, process |
| hook | 钩子拦截与管理 | native, http, process |
| eventbus | 跨子系统异步消息 | native, http |
| contextmanager | 对话与执行上下文 | native, http |
| IMemory | 长期记忆存储 | native, http |
| sandbox | 安全沙箱执行 | native, http |

## 快速开始

### 安装

```bash
go get github.com/yourorg/gagent
```

### 基本使用

```go
package main

import (
    "context"
    "github.com/yourorg/gagent/pkg/app"
)

func main() {
    // 从配置构建应用
    builder, err := app.NewBuilder("config.yaml")
    if err != nil {
        panic(err)
    }
    
    application, err := builder.Build()
    if err != nil {
        panic(err)
    }
    
    // 启动应用
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
  # LLM IProvider
  - name: "provider-openai"
    type: "provider"
    driver: "http"
    config:
      endpoint: "https://api.openai.com/v1"
      apiKey: "${OPENAI_API_KEY}"
      model: "gpt-4"

  # ITool ChannelManager
  - name: "tools-main"
    type: "tool"
    driver: "native"
    config:
      tools:
        - name: "calculator"
          description: "Calculate expressions"
          driver: "native"

  # IAgentCore Core
  - name: "agent-main"
    type: "agentcore"
    driver: "native"

defaults:
  provider: "provider-openai"
  tool: "tools-main"
  agentcore: "agent-main"
```

## 自定义组件

### Native 实现

```go
package main

import (
    "context"
    "github.com/yourorg/gagent/pkg/tool"
)

// 自定义 ITool
func main() {
    myTool := tool.NewNativeTool(
        tool.ToolInfo{
            Name:        "my-tool",
            Description: "My custom tool",
        },
        func(ctx context.Context, params map[string]any) (tool.Result, error) {
            // 实现工具逻辑
            return tool.Result{Output: "result"}, nil
        },
    )
}
```

### HTTP 远程实现

```go
package main

import (
    "github.com/yourorg/gagent/pkg/tool"
)

func main() {
    // 远程 HTTP ITool
    remoteTool := tool.NewHttpTool(&tool.HttpToolConfig{
        Name:        "remote-tool",
        Description: "Remote tool via HTTP",
        Endpoint:    "http://localhost:8080/api/tools/my-tool",
    })
}
```

### Process (MCP) 实现

```go
package main

import (
    "github.com/yourorg/gagent/pkg/tool"
)

func main() {
    // MCP 协议 ITool
    processTool, _ := tool.NewProcessTool(&tool.ProcessToolConfig{
        Name:        "mcp-tool",
        Description: "ITool via MCP protocol",
        Command:     []string{"python", "-m", "mcp_server"},
        ToolName:    "search",
    })
}
```

## 架构设计

### 组件模式

所有子系统实现 `AgentRuntime` 接口：

```go
type AgentRuntime interface {
    Name() string
    Type() ComponentType
    Initialize(ctx context.Context, registry Dependencies) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Dependencies() map[string]DependencySpec
}
```

### 依赖管理

组件依赖自动解析和拓扑排序：

```go
func (c *AgentRuntime) Dependencies() map[string]DependencySpec {
    return map[string]DependencySpec{
        "provider": {
            Type:     component.ComponentProvider,
            Required: true,
        },
        "toolManager": {
            Type:     component.ComponentTool,
            Required: false,
        },
    }
}
```

## 扩展性

### 新增子系统

1. 创建 `pkg/newsubsystem/` 目录
2. 定义接口和类型
3. 实现 `AgentRuntime` 接口
4. 在 `app/builder.go` 中添加构建逻辑
5. 在配置中使用

### 横向扩展（未来）

配置支持多实例和组合：

```yaml
components:
  - name: "eventbus-mq"
    type: "eventbus"
    driver: "http"
    config:
      endpoint: "http://mq-gateway:8080"
      
  - name: "eventbus-local"
    type: "eventbus"
    driver: "native"
    
  - name: "eventbus-primary"
    type: "eventbus"
    driver: "composite"
    config:
      instances: ["eventbus-mq", "eventbus-local"]
      strategy: "failover"
```

## 项目结构

```
pkg/
├── component/          # 组件基础设施
├── channel/            # IChannel 子系统
├── agentcore/          # AgentCore 子系统
├── provider/           # IProvider 子系统
├── tool/               # ITool 子系统
├── hook/               # IHook 子系统
├── eventbus/           # IEventBus 子系统
├── contextmanager/     # IContextManager 子系统
├── IMemory/             # IMemory 子系统
├── sandbox/            # ISandbox 子系统
├── app/                # 应用组装
└── protocol/           # 通信协议
    ├── mcp/            # MCP 协议
    └── rest/           # REST 规范
```

## 许可证

MIT License
