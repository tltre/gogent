package native_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/memory"
)

type mockMemory struct {
	items map[string]memory.MemoryItem
}

func newMockMemory() *mockMemory {
	return &mockMemory{items: make(map[string]memory.MemoryItem)}
}

func (m *mockMemory) Add(_ context.Context, item memory.MemoryItem) error {
	m.items[item.ID] = item
	return nil
}

func (m *mockMemory) AddBatch(_ context.Context, items []memory.MemoryItem) error {
	for _, item := range items {
		m.items[item.ID] = item
	}
	return nil
}

func (m *mockMemory) Query(_ context.Context, q memory.Query) ([]memory.MemoryItem, error) {
	var results []memory.MemoryItem
	for _, item := range m.items {
		if q.TopK > 0 && len(results) >= q.TopK {
			break
		}
		results = append(results, item)
	}
	return results, nil
}

func (m *mockMemory) Get(_ context.Context, id string) (memory.MemoryItem, error) {
	item, ok := m.items[id]
	if !ok {
		return memory.MemoryItem{}, nil
	}
	return item, nil
}

func (m *mockMemory) Delete(_ context.Context, id string) error {
	delete(m.items, id)
	return nil
}

func (m *mockMemory) Clear(_ context.Context) error {
	m.items = make(map[string]memory.MemoryItem)
	return nil
}

func (m *mockMemory) Count(_ context.Context) (int64, error) {
	return int64(len(m.items)), nil
}

func TestMemoryComponent(t *testing.T) {
	m := newMockMemory()
	comp := memory.NewComponent("test-memory", m)
	ctx := context.Background()

	if got := comp.GetName(); got != "test-memory" {
		t.Errorf("GetName() = %q, want %q", got, "test-memory")
	}
	if got := comp.GetType(); got != component.ComponentMemory {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentMemory)
	}

	reg := component.NewRegistry()
	if err := comp.Initialize(ctx, reg); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	item := memory.MemoryItem{ID: "1", Content: "remember this"}
	if err := comp.Add(ctx, item); err != nil {
		t.Fatalf("Add() = %v", err)
	}

	count, err := comp.Count(ctx)
	if err != nil {
		t.Fatalf("Count() = %v", err)
	}
	if count != 1 {
		t.Errorf("Count() = %d, want 1", count)
	}

	got, err := comp.Get(ctx, "1")
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if got.Content != "remember this" {
		t.Errorf("Get().Content = %q", got.Content)
	}

	if err := comp.Delete(ctx, "1"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if count, _ := comp.Count(ctx); count != 0 {
		t.Errorf("Count() after delete = %d, want 0", count)
	}

	if err := comp.Add(ctx, item); err != nil {
		t.Fatalf("Add() = %v", err)
	}
	if err := comp.Clear(ctx); err != nil {
		t.Fatalf("Clear() = %v", err)
	}
	if count, _ := comp.Count(ctx); count != 0 {
		t.Errorf("Count() after clear = %d, want 0", count)
	}
}
