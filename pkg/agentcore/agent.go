package agentcore

import (
	"context"
)

type Input struct {
	Messages []Message
	Context  map[string]any
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

type Agent interface {
	Run(ctx context.Context, input Input) (Output, error)
	Stream(ctx context.Context, input Input) (<-chan Event, error)
}

type AgentRunner interface {
	SetProvider(provider any) AgentRunner
	SetToolManager(toolManager any) AgentRunner
	SetHookManager(hookManager any) AgentRunner
	SetContextManager(contextManager any) AgentRunner
	SetMemory(memory any) AgentRunner
	SetEventBus(eventBus any) AgentRunner
	Build() (Agent, error)
}

type Executor interface {
	Execute(ctx context.Context, input Input) (Output, error)
}
