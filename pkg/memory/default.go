package memory

import (
	"context"
	"sync"
)

type DefaultMemory struct {
	mu    sync.RWMutex
	items map[string]MemoryItem
}

func NewDefaultMemory() *DefaultMemory {
	return &DefaultMemory{
		items: make(map[string]MemoryItem),
	}
}

func (m *DefaultMemory) Add(ctx context.Context, item MemoryItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[item.ID] = item
	return nil
}

func (m *DefaultMemory) AddBatch(ctx context.Context, items []MemoryItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range items {
		m.items[item.ID] = item
	}
	return nil
}

func (m *DefaultMemory) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]MemoryItem, 0)
	for _, item := range m.items {
		if q.MinScore > 0 && item.Score < q.MinScore {
			continue
		}
		if q.Filter != nil {
			for k, v := range q.Filter {
				if item.Extra[k] != v {
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

func (m *DefaultMemory) Get(ctx context.Context, id string) (MemoryItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.items[id], nil
}

func (m *DefaultMemory) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.items, id)
	return nil
}

func (m *DefaultMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = make(map[string]MemoryItem)
	return nil
}

func (m *DefaultMemory) Count(ctx context.Context) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return int64(len(m.items)), nil
}
