package contextmanager

import (
	"context"
	"sync"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/logger"
)

type ContextManagerComponent struct {
	component.BasicComponent
	mu      sync.RWMutex
	manager IContextManager
}

func NewComponent(name string, contextManager IContextManager) *ContextManagerComponent {
	return &ContextManagerComponent{
		BasicComponent: component.NewBasicComponent(name),
		manager:        contextManager,
	}
}

func (c *ContextManagerComponent) GetType() component.ComponentType {
	return component.ComponentContextManager
}

func (c *ContextManagerComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	c.log(ctx, logger.InfoLevel, "context manager initialized")
	return nil
}

func (c *ContextManagerComponent) Start(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "context manager started")
	return nil
}

func (c *ContextManagerComponent) Stop(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "context manager stopped")
	return nil
}

func (c *ContextManagerComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *ContextManagerComponent) NewSession() string {
	id := c.manager.NewSession()
	return id
}

func (c *ContextManagerComponent) AddMessage(sessionId string, msg ContextMessage) error {
	c.log(context.Background(), logger.DebugLevel, "add message",
		logger.Field{Key: "session", Value: sessionId},
	)
	return c.manager.AddMessage(sessionId, msg)
}

func (c *ContextManagerComponent) GetMessages(sessionId string) []ContextMessage {
	return c.manager.GetMessages(sessionId)
}

func (c *ContextManagerComponent) GetSummary(sessionId string) (Summary, error) {
	return c.manager.GetSummary(sessionId)
}

func (c *ContextManagerComponent) BuildSystemPrompt(sessionId string) string {
	return c.manager.BuildSystemPrompt(sessionId)
}

func (c *ContextManagerComponent) Clear(sessionId string) error {
	c.log(context.Background(), logger.DebugLevel, "clear session",
		logger.Field{Key: "session", Value: sessionId},
	)
	return c.manager.Clear(sessionId)
}

func (c *ContextManagerComponent) DeleteSession(sessionId string) {
	c.log(context.Background(), logger.DebugLevel, "delete session",
		logger.Field{Key: "session", Value: sessionId},
	)
	c.manager.DeleteSession(sessionId)
}

func (c *ContextManagerComponent) ListSessions() []string {
	return c.manager.ListSessions()
}

func (c *ContextManagerComponent) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := c.Registry()
	if r == nil {
		return
	}
	lc := r.GetDefault(component.ComponentLogger)
	if lc == nil {
		return
	}
	l, ok := lc.(logger.Logger)
	if !ok {
		return
	}
	l.Log(ctx, logger.LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Module:    c.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
