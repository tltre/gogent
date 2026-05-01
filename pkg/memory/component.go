package memory

import (
	"context"

	"github.com/tltre/gagent/pkg/component"
)

type MemoryComponent struct {
	name   string
	memory IMemory
}

func NewComponent(name string, memory IMemory) *MemoryComponent {
	return &MemoryComponent{
		name:   name,
		memory: memory,
	}
}

func (c *MemoryComponent) GetName() string {
	return c.name
}

func (c *MemoryComponent) GetType() component.ComponentType {
	return component.ComponentMemory
}

func (c *MemoryComponent) Initialize(ctx context.Context, deps *component.Registry) error {
	return nil
}

func (c *MemoryComponent) Start(ctx context.Context) error {
	return nil
}

func (c *MemoryComponent) Stop(ctx context.Context) error {
	return nil
}

func (c *MemoryComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *MemoryComponent) Add(ctx context.Context, item MemoryItem) error {
	return c.memory.Add(ctx, item)
}

func (c *MemoryComponent) AddBatch(ctx context.Context, items []MemoryItem) error {
	return c.memory.AddBatch(ctx, items)
}

func (c *MemoryComponent) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	return c.memory.Query(ctx, q)
}

func (c *MemoryComponent) Get(ctx context.Context, id string) (MemoryItem, error) {
	return c.memory.Get(ctx, id)
}

func (c *MemoryComponent) Delete(ctx context.Context, id string) error {
	return c.memory.Delete(ctx, id)
}

func (c *MemoryComponent) Clear(ctx context.Context) error {
	return c.memory.Clear(ctx)
}

func (c *MemoryComponent) Count(ctx context.Context) (int64, error) {
	return c.memory.Count(ctx)
}
