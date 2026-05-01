package contextmanager

import (
	"context"
	"sync"

	"github.com/tltre/gagent/pkg/component"
)

type ContextManagerComponent struct {
	name    string
	mu      sync.RWMutex
	manager IContextManager
}

func NewComponent(name string, contextManager IContextManager) *ContextManagerComponent {
	return &ContextManagerComponent{
		name:    name,
		manager: contextManager,
	}
}

func (c *ContextManagerComponent) GetName() string {
	return c.name
}

func (c *ContextManagerComponent) GetType() component.ComponentType {
	return component.ComponentContextManager
}

func (c *ContextManagerComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	return nil
}

func (c *ContextManagerComponent) Start(ctx context.Context) error {
	return nil
}

func (c *ContextManagerComponent) Stop(ctx context.Context) error {
	return nil
}

func (c *ContextManagerComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *ContextManagerComponent) NewSession() string {
	return c.manager.NewSession()
}

func (c *ContextManagerComponent) AddMessage(sessionId string, msg ContextMessage) error {
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
	return c.manager.Clear(sessionId)
}

func (c *ContextManagerComponent) DeleteSession(sessionId string) {
	c.manager.DeleteSession(sessionId)
}
