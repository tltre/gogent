package contextmanager

import (
	"context"
	"time"
)

type ContextMessage struct {
	Role      string
	Content   string
	Timestamp time.Time
	Metadata  map[string]any
}

type Summary struct {
	Content   string
	KeyPoints []string
	Timestamp time.Time
}

type Context interface {
	SessionID() string
	AddMessage(msg ContextMessage) error
	GetMessages() []ContextMessage
	GetSummary() (Summary, error)
	GetSystemPrompt() string
	SetVariable(key string, value any) error
	GetVariable(key string) (any, bool)
	GetAllVariables() map[string]any
	Clear() error
}

type SummaryGenerator interface {
	Generate(ctx context.Context, messages []ContextMessage) (Summary, error)
}

type ContextManager interface {
	Create(sessionID string) (Context, error)
	Get(sessionID string) (Context, bool)
	Delete(sessionID string) error
	SetSummaryGenerator(gen SummaryGenerator)
	List() []string
}
