package channel

import (
	"context"
	"sync"

	"github.com/yourorg/gagent/pkg/component"
)

type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
}

func NewManager() *Manager {
	return &Manager{
		channels: make(map[string]Channel),
	}
}

func (m *Manager) Register(name string, ch Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = ch
}

func (m *Manager) Get(name string) Channel {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channels[name]
}

func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		if err := ch.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ch := range m.channels {
		if err := ch.Stop(ctx); err != nil {
			return err
		}
	}
	return nil
}

type Component struct {
	name     string
	manager  *Manager
	channels []Channel
}

func NewComponent(name string, channels ...Channel) *Component {
	return &Component{
		name:     name,
		manager:  NewManager(),
		channels: channels,
	}
}

func (c *Component) Name() string {
	return c.name
}

func (c *Component) Type() component.ComponentType {
	return component.ComponentChannel
}

func (c *Component) Initialize(ctx context.Context, deps component.Dependencies) error {
	for i, ch := range c.channels {
		c.manager.Register(ch.Name(), ch)
		_ = i
	}
	return nil
}

func (c *Component) Start(ctx context.Context) error {
	return c.manager.StartAll(ctx)
}

func (c *Component) Stop(ctx context.Context) error {
	return c.manager.StopAll(ctx)
}

func (c *Component) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *Component) Manager() *Manager {
	return c.manager
}
