# Agent 类型

Gogent 通过 `agentcore` 组件承载 agent 执行逻辑。agent 类型由组件 `config.type` 指定，目前实现 **`react`**（完整 ReAct 循环），是默认类型。

## react agent（ReAct 循环）

```yaml
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"       # 缺省即 react
```

### 行为

每次 `Run` 执行完整的 ReAct 范式——**推理 → 工具调用 → 观察 → 迭代**：

```
Run(input):
 ① 工具声明：向模型声明 App 已授权的工具（tool.Service.List()）
 ② 组装输入：ContextManager.BuildInput（系统提示 + 记忆 + 会话历史 + 用户消息）
 ③ 循环（上限 10 轮）：
    a. LLM 推理（provider.Generate）
    b. 无工具调用 → 返回最终回答
    c. 有工具调用 → 逐条执行 → 结果回填 → 回到 a
 ④ 持久化：user + 最终 assistant 消息入会话历史；工具交互存档入 Memory
```

### 停止条件

| 条件 | 行为 |
|------|------|
| 模型不再请求工具 | 正常返回最终回答 |
| 迭代达上限（10 轮） | 返回部分结果，Metadata 标记 `maxIterationsReached` |
| 上下文取消 | 返回错误 |
| 工具执行出错 | 错误作为结果回填，模型可自纠正继续 |

### 工具结果保护

工具结果回填前截断到 8KB（头尾各保留 4KB，中间省略），防止超大结果撑爆上下文窗口。

## 上下文与记忆

### 会话历史（ContextManager）

ContextManager 是 agent 的**唯一输入来源**，负责组装每次运行的完整输入：

```
BuildInput(sessionID, userMessages):
  ① 查询相关记忆（Memory.Query）
  ② 构造系统提示（含记忆注入）
  ③ 读取该会话的历史消息
  ④ 返回 系统 + 记忆 + 历史 + 用户消息
```

- **会话**：`chat` 启动时 `NewSession()` 创建会话，之后每条消息携带 `SessionID`
- **持久化**：每轮对话的 user 消息 + 最终 assistant 回答写入历史
- **效果**：跨轮次对话模型能记住上下文（"我叫小明" → "我叫什么名字？" 能答出）

### 长期记忆（Memory）

- **工具交互存档**：ReAct 循环内的每次工具调用（工具名、参数、结果）写入 Memory，供跨会话检索
- **不污染对话历史**：过程量进 Memory，对话内容进 ContextManager，两者职责分离

### 查看会话

```bash
curl http://localhost:<agent-port>/api/v1/app/sessions   # 列出活跃会话
```

## 工具调用

Agent 通过 `tool.Service`（框架注入的薄客户端）访问工具，**只能 List 和 Execute**——生命周期由框架（App）管理：

```go
// agent 可见的工具调用面（pkg/tool/service.go）
type Service interface {
    List() []ToolInfo                                             // 声明给模型的工具
    Execute(ctx context.Context, name string, params map[string]any) (Result, error)
}
```

- 工具**必须先在 App 配置中声明**才可用（见 [配置参考](configuration.md#tools工具声明)）
- 执行管线在 daemon 侧：**鉴权 → 钩子 → 执行 → 钩子 → 结果**，App 侧只做转发
- 工具执行全程记录 OTel `tool.exec` span（含鉴权/钩子/结果事件）

## 自定义 agent 类型

`IAgentCore` 是 agent 逻辑接口（`Run` / `Stream` / `SetAgentRuntime`），开发者可实现自定义 agent 并通过类型注册表接入：

```go
type IAgentCore interface {
    Run(ctx context.Context, input Input) (Output, error)
    Stream(ctx context.Context, input Input) (<-chan Event, error)
    SetAgentRuntime(runtime *AgentRuntime)
}
```

```go
// 注册自定义类型（参考 pkg/agentcore/react.go 的 init 注册模式）
agentcore.RegisterAgentType("my-agent", func() IAgentCore { return &MyAgent{} })
```

```yaml
# 然后 YAML 中即可使用
config:
  type: "my-agent"
```

自定义 agent 通过 `runtime.ToolService()` 访问工具、`runtime.Reg()` 访问 Registry 中的其他组件（ProviderManager、ContextManager、Memory 等）。

## 钩子（Hook）

ReAct 循环在关键节点触发钩子事件，可用于日志、审计、参数修改或阻断：

| 事件 | 触发时机 |
|------|---------|
| `beforeLLM` / `afterLLM` | 每次 LLM 调用前后 |
| `beforeTool` / `afterTool` | 每次工具执行前后 |

钩子通过 `hook` 组件注册（见 [配置参考](configuration.md) 的 hook 类型说明）。
