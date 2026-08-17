# Agents

Gogent hosts agent execution logic in the `agentcore` component. The agent type is chosen via the component's `config.type`; **`react`** (full ReAct loop) is currently implemented and is the default.

## react agent (ReAct Loop)

```yaml
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"       # default if omitted
```

### Behavior

Every `Run` executes the full ReAct paradigm — **reason → tool call → observe → iterate**:

```
Run(input):
 ① Declare tools: tell the model which tools the app has authorized (tool.Service.List())
 ② Assemble input: ContextManager.BuildInput (system prompt + memory + session history + user messages)
 ③ Loop (cap 10 iterations):
    a. LLM reasoning (provider.Generate)
    b. no tool calls → return the final answer
    c. tool calls present → execute each, feed results back, back to a
 ④ Persist: user + final assistant messages into session history; tool interactions archived to Memory
```

### Stop Conditions

| Condition | Behavior |
|-----------|----------|
| model stops requesting tools | normal return of the final answer |
| iteration cap reached (10) | partial result returned, Metadata marks `maxIterationsReached` |
| context cancelled | error returned |
| tool execution error | error fed back as a result; the model can self-correct and continue |

### Tool Result Protection

Tool results are truncated to 8KB before feedback (head 4KB + tail 4KB, middle elided) to keep huge results from blowing up the context window.

## Context and Memory

### Session History (ContextManager)

The ContextManager is the agent's **single input source**, assembling the complete input for every run:

```
BuildInput(sessionID, userMessages):
  ① query relevant memory (Memory.Query)
  ② build the system prompt (with memory injection)
  ③ read the session's history messages
  ④ return  system + memory + history + user messages
```

- **Sessions**: `chat` calls `NewSession()` at startup; every message then carries the `SessionID`
- **Persistence**: each turn's user message + final assistant answer are written to history
- **Effect**: across turns the model remembers context ("my name is Xiao Ming" → "what's my name?" gets the right answer)

### Long-Term Memory

- **Tool interaction archive**: every tool call inside the ReAct loop (tool name, params, result) is written to Memory for cross-session retrieval
- **No history pollution**: process data goes to Memory, conversation content goes to the ContextManager — two clearly separated responsibilities

### Listing Sessions

```bash
curl http://localhost:<agent-port>/api/v1/app/sessions   # list active sessions
```

## Tool Calling

Agents access tools through `tool.Service` (a framework-injected thin client) — **only List and Execute**; lifecycle is owned by the framework (App):

```go
// the tool-calling surface visible to agents (pkg/tool/service.go)
type Service interface {
    List() []ToolInfo                                             // tools declared to the model
    Execute(ctx context.Context, name string, params map[string]any) (Result, error)
}
```

- Tools **must be declared in the app config first** to be usable (see [Configuration](configuration.md#tools-tool-declarations))
- The execution pipeline runs daemon-side: **auth → hook → execute → hook → result**; the app only forwards
- Every tool execution records an OTel `tool.exec` span (with auth/hook/result events)

## Custom Agent Types

`IAgentCore` is the agent logic interface (`Run` / `Stream` / `SetAgentRuntime`); developers can implement a custom agent and register it through the type registry:

```go
type IAgentCore interface {
    Run(ctx context.Context, input Input) (Output, error)
    Stream(ctx context.Context, input Input) (<-chan Event, error)
    SetAgentRuntime(runtime *AgentRuntime)
}
```

```go
// register a custom type (see the init registration pattern in pkg/agentcore/react.go)
agentcore.RegisterAgentType("my-agent", func() IAgentCore { return &MyAgent{} })
```

```yaml
# then it becomes available in YAML
config:
  type: "my-agent"
```

Custom agents reach tools via `runtime.ToolService()` and other components via `runtime.Reg()` (ProviderManager, ContextManager, Memory, ...).

## Hooks

The ReAct loop fires hook events at key points, usable for logging, auditing, parameter modification, or blocking:

| Event | Fires |
|-------|-------|
| `beforeLLM` / `afterLLM` | around every LLM call |
| `beforeTool` / `afterTool` | around every tool execution |

Hooks are registered through the `hook` component (see the hook type in [Configuration](configuration.md)).
