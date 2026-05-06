package memory

import (
	"context"

	"github.com/tltre/gogent/internal/client"
)

type ProcessMemoryConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessMemory struct {
	tr *client.LazyTransport
}

func NewProcessMemory(cfg *ProcessMemoryConfig) *ProcessMemory {
	return &ProcessMemory{tr: client.WrapLazy(cfg.Transport)}
}

func (m *ProcessMemory) Add(ctx context.Context, item MemoryItem) error {
	return m.tr.Call(ctx, "memory/add", item, nil)
}

func (m *ProcessMemory) AddBatch(ctx context.Context, items []MemoryItem) error {
	return m.tr.Call(ctx, "memory/addBatch", items, nil)
}

func (m *ProcessMemory) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	var result []MemoryItem
	if err := m.tr.Call(ctx, "memory/query", q, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *ProcessMemory) Get(ctx context.Context, id string) (MemoryItem, error) {
	var result MemoryItem
	if err := m.tr.Call(ctx, "memory/get", id, &result); err != nil {
		return MemoryItem{}, err
	}
	return result, nil
}

func (m *ProcessMemory) Delete(ctx context.Context, id string) error {
	return m.tr.Call(ctx, "memory/delete", id, nil)
}

func (m *ProcessMemory) Clear(ctx context.Context) error {
	return m.tr.Call(ctx, "memory/clear", nil, nil)
}

func (m *ProcessMemory) Count(ctx context.Context) (int64, error) {
	var result int64
	if err := m.tr.Call(ctx, "memory/count", nil, &result); err != nil {
		return 0, err
	}
	return result, nil
}
