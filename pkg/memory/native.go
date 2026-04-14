package memory

import (
	"context"
	"sync"

	"github.com/yourorg/gagent/pkg/component"
)

type Component struct {
	name   string
	memory Memory
}

func NewComponent(name string, memory Memory) *Component {
	return &Component{
		name:   name,
		memory: memory,
	}
}

func (c *Component) Name() string {
	return c.name
}

func (c *Component) Type() component.ComponentType {
	return component.ComponentMemory
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

func (c *Component) Add(ctx context.Context, item MemoryItem) error {
	return c.memory.Add(ctx, item)
}

func (c *Component) AddBatch(ctx context.Context, items []MemoryItem) error {
	return c.memory.AddBatch(ctx, items)
}

func (c *Component) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	return c.memory.Query(ctx, q)
}

func (c *Component) Get(ctx context.Context, id string) (MemoryItem, error) {
	return c.memory.Get(ctx, id)
}

func (c *Component) Delete(ctx context.Context, id string) error {
	return c.memory.Delete(ctx, id)
}

func (c *Component) Clear(ctx context.Context) error {
	return c.memory.Clear(ctx)
}

func (c *Component) Count(ctx context.Context) (int64, error) {
	return c.memory.Count(ctx)
}

type NativeMemory struct {
	mu    sync.RWMutex
	items map[string]MemoryItem
}

func NewNativeMemory() *NativeMemory {
	return &NativeMemory{
		items: make(map[string]MemoryItem),
	}
}

func (m *NativeMemory) Add(ctx context.Context, item MemoryItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[item.ID] = item
	return nil
}

func (m *NativeMemory) AddBatch(ctx context.Context, items []MemoryItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range items {
		m.items[item.ID] = item
	}
	return nil
}

func (m *NativeMemory) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]MemoryItem, 0)
	for _, item := range m.items {
		if q.MinScore > 0 && item.Score < q.MinScore {
			continue
		}
		if q.Filter != nil {
			for k, v := range q.Filter {
				if item.Metadata[k] != v {
					continue
				}
			}
		}
		results = append(results, item)
	}

	if q.TopK > 0 && len(results) > q.TopK {
		results = results[:q.TopK]
	}

	return results, nil
}

func (m *NativeMemory) Get(ctx context.Context, id string) (MemoryItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.items[id], nil
}

func (m *NativeMemory) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id)
	return nil
}

func (m *NativeMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = make(map[string]MemoryItem)
	return nil
}

func (m *NativeMemory) Count(ctx context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.items)), nil
}
