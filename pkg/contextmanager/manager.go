package contextmanager

import (
	"context"
	"sync"

	"github.com/yourorg/gagent/pkg/component"
)

type Component struct {
	name    string
	manager *Manager
}

func NewComponent(name string) *Component {
	return &Component{
		name:    name,
		manager: NewManager(),
	}
}

func (c *Component) Name() string {
	return c.name
}

func (c *Component) Type() component.ComponentType {
	return component.ComponentContextManager
}

func (c *Component) Initialize(ctx context.Context, deps component.Dependencies) error {
	return nil
}

func (c *Component) Start(ctx context.Context) error {
	return nil
}

func (c *Component) Stop(ctx context.Context) error {
	return nil
}

func (c *Component) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *Component) Manager() *Manager {
	return c.manager
}

func (c *Component) Create(sessionID string) (Context, error) {
	return c.manager.Create(sessionID)
}

func (c *Component) Get(sessionID string) (Context, bool) {
	return c.manager.Get(sessionID)
}

func (c *Component) Delete(sessionID string) error {
	return c.manager.Delete(sessionID)
}

func (c *Component) List() []string {
	return c.manager.List()
}

type Manager struct {
	mu               sync.RWMutex
	contexts         map[string]*nativeContext
	summaryGenerator SummaryGenerator
}

func NewManager() *Manager {
	return &Manager{
		contexts: make(map[string]*nativeContext),
	}
}

func (m *Manager) Create(sessionID string) (Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.contexts[sessionID]; exists {
		return m.contexts[sessionID], nil
	}

	ctx := &nativeContext{
		sessionID:    sessionID,
		messages:     make([]ContextMessage, 0),
		variables:    make(map[string]any),
		systemPrompt: "",
	}
	m.contexts[sessionID] = ctx
	return ctx, nil
}

func (m *Manager) Get(sessionID string) (Context, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ctx, ok := m.contexts[sessionID]
	return ctx, ok
}

func (m *Manager) Delete(sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.contexts, sessionID)
	return nil
}

func (m *Manager) SetSummaryGenerator(gen SummaryGenerator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.summaryGenerator = gen
}

func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]string, 0, len(m.contexts))
	for id := range m.contexts {
		list = append(list, id)
	}
	return list
}

type nativeContext struct {
	sessionID    string
	messages     []ContextMessage
	variables    map[string]any
	systemPrompt string
	mu           sync.RWMutex
}

func (c *nativeContext) SessionID() string {
	return c.sessionID
}

func (c *nativeContext) AddMessage(msg ContextMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msg)
	return nil
}

func (c *nativeContext) GetMessages() []ContextMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.messages
}

func (c *nativeContext) GetSummary() (Summary, error) {
	return Summary{Content: ""}, nil
}

func (c *nativeContext) GetSystemPrompt() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.systemPrompt
}

func (c *nativeContext) SetVariable(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.variables[key] = value
	return nil
}

func (c *nativeContext) GetVariable(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.variables[key]
	return v, ok
}

func (c *nativeContext) GetAllVariables() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]any, len(c.variables))
	for k, v := range c.variables {
		result[k] = v
	}
	return result
}

func (c *nativeContext) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = make([]ContextMessage, 0)
	c.variables = make(map[string]any)
	return nil
}

func (c *nativeContext) SetSystemPrompt(prompt string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.systemPrompt = prompt
}
