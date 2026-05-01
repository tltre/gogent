package hook

import "context"

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
	Name    string
	Payload any
	Error   error
}

type IHook interface {
	OnEvent(ctx context.Context, event Event) (context.Context, error)
	Events() []EventType
}

//type HookManager interface {
//	Register(hook IHook) error
//	Trigger(ctx context.Context, event Event) (context.Context, error)
//	GetHooks(eventType EventType) []IHook
//}
