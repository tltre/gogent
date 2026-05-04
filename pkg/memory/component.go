package memory

import (
	"context"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/logger"
)

type MemoryComponent struct {
	component.BasicComponent
	memory IMemory
}

func NewComponent(name string, memory IMemory) *MemoryComponent {
	return &MemoryComponent{
		BasicComponent: component.NewBasicComponent(name),
		memory:         memory,
	}
}

func (c *MemoryComponent) GetType() component.ComponentType {
	return component.ComponentMemory
}

func (c *MemoryComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	c.log(ctx, logger.InfoLevel, "memory initialized")
	return nil
}

func (c *MemoryComponent) Start(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "memory started")
	return nil
}

func (c *MemoryComponent) Stop(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "memory stopped")
	return nil
}

func (c *MemoryComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *MemoryComponent) Add(ctx context.Context, item MemoryItem) error {
	c.log(ctx, logger.DebugLevel, "memory add", logger.Field{Key: "id", Value: item.ID})
	return c.memory.Add(ctx, item)
}

func (c *MemoryComponent) AddBatch(ctx context.Context, items []MemoryItem) error {
	c.log(ctx, logger.DebugLevel, "memory add batch", logger.Field{Key: "count", Value: len(items)})
	return c.memory.AddBatch(ctx, items)
}

func (c *MemoryComponent) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	return c.memory.Query(ctx, q)
}

func (c *MemoryComponent) Get(ctx context.Context, id string) (MemoryItem, error) {
	return c.memory.Get(ctx, id)
}

func (c *MemoryComponent) Delete(ctx context.Context, id string) error {
	c.log(ctx, logger.DebugLevel, "memory delete", logger.Field{Key: "id", Value: id})
	return c.memory.Delete(ctx, id)
}

func (c *MemoryComponent) Clear(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "memory clear")
	return c.memory.Clear(ctx)
}

func (c *MemoryComponent) Count(ctx context.Context) (int64, error) {
	return c.memory.Count(ctx)
}

func (c *MemoryComponent) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
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
