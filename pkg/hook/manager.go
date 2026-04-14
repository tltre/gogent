package hook

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
	return component.ComponentHook
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

func (c *Component) Register(hook Hook) error {
	return c.manager.Register(hook)
}

func (c *Component) Trigger(ctx context.Context, event Event) (context.Context, error) {
	return c.manager.Trigger(ctx, event)
}

func (c *Component) GetHooks(eventType EventType) []Hook {
	return c.manager.GetHooks(eventType)
}

type Manager struct {
	mu     sync.RWMutex
	hooks  []Hook
	byType map[EventType][]Hook
}

func NewManager() *Manager {
	return &Manager{
		hooks:  make([]Hook, 0),
		byType: make(map[EventType][]Hook),
	}
}

func (m *Manager) Register(hook Hook) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hooks = append(m.hooks, hook)
	for _, eventType := range hook.Events() {
		m.byType[eventType] = append(m.byType[eventType], hook)
	}
	return nil
}

func (m *Manager) Trigger(ctx context.Context, event Event) (context.Context, error) {
	m.mu.RLock()
	hooks := m.byType[event.Type]
	m.mu.RUnlock()

	for _, hook := range hooks {
		newCtx, err := hook.OnEvent(ctx, event)
		if err != nil {
			return ctx, err
		}
		ctx = newCtx
	}
	return ctx, nil
}

func (m *Manager) GetHooks(eventType EventType) []Hook {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.byType[eventType]
}
