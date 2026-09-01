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
	// v0.15.2: inject the registry into managers that need it to resolve
	// dependencies (DefaultContextManager resolves Memory via BuildInput).
	if ra, ok := c.manager.(registryAware); ok {
		ra.SetRegistry(registry)
	}
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
	return map[string]component.DependencySpec{
		// v0.15.2: DefaultContextManager.BuildInput recalls memory through the
		// registry; declare the dependency for correct topological ordering.
		"Memory": {
			Type:     component.ComponentMemory,
			Required: false,
		},
		// v0.16.x: compression resolves the ProviderManager for context-window
		// lookups and the LLM summarizer.
		"Provider": {
			Type:     component.ComponentProvider,
			Required: false,
		},
	}
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

func (c *ContextManagerComponent) BuildInput(ctx context.Context, sessionId string, messages []ContextMessage) []ContextMessage {
	return c.manager.BuildInput(ctx, sessionId, messages)
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
