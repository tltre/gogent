package tool

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
	return component.ComponentTool
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

func (c *Component) Register(tool Tool) error {
	return c.manager.Register(tool)
}

func (c *Component) Get(name string) Tool {
	return c.manager.Get(name)
}

func (c *Component) List() []ToolInfo {
	return c.manager.List()
}

func (c *Component) Execute(ctx context.Context, name string, params map[string]any) (Result, error) {
	return c.manager.Execute(ctx, name, params)
}

func (c *Component) Stream(ctx context.Context, name string, params map[string]any) (<-chan StreamChunk, error) {
	return c.manager.Stream(ctx, name, params)
}

type Manager struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

func NewManager() *Manager {
	return &Manager{
		tools: make(map[string]Tool),
	}
}

func (m *Manager) Register(tool Tool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	info := tool.Info()
	m.tools[info.Name] = tool
	return nil
}

func (m *Manager) Get(name string) Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools[name]
}

func (m *Manager) List() []ToolInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	list := make([]ToolInfo, 0, len(m.tools))
	for _, t := range m.tools {
		list = append(list, t.Info())
	}
	return list
}

func (m *Manager) Execute(ctx context.Context, name string, params map[string]any) (Result, error) {
	m.mu.RLock()
	tool, ok := m.tools[name]
	m.mu.RUnlock()
	if !ok {
		return Result{IsError: true, ErrorMsg: "tool not found: " + name}, nil
	}
	return tool.Execute(ctx, params)
}

func (m *Manager) Stream(ctx context.Context, name string, params map[string]any) (<-chan StreamChunk, error) {
	m.mu.RLock()
	tool, ok := m.tools[name]
	m.mu.RUnlock()
	if !ok {
		ch := make(chan StreamChunk)
		go func() {
			ch <- StreamChunk{Error: nil, Done: true}
			close(ch)
		}()
		return ch, nil
	}
	return tool.Stream(ctx, params)
}
