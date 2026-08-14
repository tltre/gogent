package agentcore

import (
	"context"
)

type Input struct {
	Messages []Message
	Context  map[string]any

	// ProviderName selects the provider engine to use for this run
	// (engine name, e.g. "openai", "deepseek"). Empty means the
	// AgentCore's default routing (v0.14.7).
	ProviderName string

	// ModelName selects the model within the chosen provider.
	// Empty means the provider's default model (v0.14.7).
	ModelName string

	// SessionID identifies the conversation session (v0.15.2). Non-empty
	// enables cross-turn history via ContextManager; empty = stateless single
	// call. The CLI chat maintains it across user turns.
	SessionID string
}

type Output struct {
	Response Message
	Actions  []Action
	Metadata map[string]any
}

type Message struct {
	Role    string
	Content string
	Tools   []ToolInfo
}

type ToolInfo struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Action struct {
	ToolName string
	Params   map[string]any
	Result   any
}

type EventType int

const (
	EventBeforeRun EventType = iota
	EventAfterRun
	EventBeforeTool
	EventAfterTool
	EventBeforeLLM
	EventAfterLLM
	EventError
	EventStreaming
)

type Event struct {
	Type    EventType
	Payload any
	Error   error
}

type IAgentCore interface {
	Run(ctx context.Context, input Input) (Output, error)
	Stream(ctx context.Context, input Input) (<-chan Event, error)
	SetAgentRuntime(runtime *AgentRuntime)
}

//type AgentRunner interface {
//	SetProvider(provider any) AgentRunner
//	SetToolManager(toolManager any) AgentRunner
//	SetHookManager(HookManager any) AgentRunner
//	SetContextManager(contextManager any) AgentRunner
//	SetMemory(Memory any) AgentRunner
//	SetEventBus(EventBus any) AgentRunner
//	Build() (IAgentCore, error)
//}

//type Executor interface {
//	Execute(ctx context.Context, input Input) (Output, error)
//}
