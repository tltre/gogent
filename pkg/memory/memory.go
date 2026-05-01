package memory

import (
	"context"
	"time"
)

type MemoryItem struct {
	ID        string
	Content   string
	Extra     map[string]any
	Embedding []float64
	CreatedAt time.Time
	Score     float64
}

type Query struct {
	Text     string
	TopK     int
	Filter   map[string]any
	MinScore float64
}

type IMemory interface {
	Add(ctx context.Context, item MemoryItem) error
	AddBatch(ctx context.Context, items []MemoryItem) error
	Query(ctx context.Context, q Query) ([]MemoryItem, error)
	Get(ctx context.Context, id string) (MemoryItem, error)
	Delete(ctx context.Context, id string) error
	Clear(ctx context.Context) error
	Count(ctx context.Context) (int64, error)
}

type EmbeddingFunc func(ctx context.Context, text string) ([]float64, error)
