package memory

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	"github.com/tltre/gogent/internal/otel"
)

// ProcessMemoryConfig holds the configuration for a gRPC-based ProcessMemory.
type ProcessMemoryConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessMemory is a memory implementation that communicates with a remote
// memory service over gRPC.
type ProcessMemory struct {
	cfg    *ProcessMemoryConfig
	client gogentv1.MemoryServiceClient
}

// NewProcessMemory creates a new ProcessMemory. The gRPC client is lazily
// initialized on the first method call.
func NewProcessMemory(cfg *ProcessMemoryConfig) *ProcessMemory {
	return &ProcessMemory{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC MemoryServiceClient.
func (m *ProcessMemory) getClient() (gogentv1.MemoryServiceClient, error) {
	if m.client != nil {
		return m.client, nil
	}
	conn, err := m.cfg.Pool.Get(m.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("memory: get connection: %w", err)
	}
	m.client = gogentv1.NewMemoryServiceClient(conn.ClientConn())
	return m.client, nil
}

// Add stores a memory item via gRPC.
func (m *ProcessMemory) Add(ctx context.Context, item MemoryItem) error {
	client, err := m.getClient()
	if err != nil {
		return err
	}
	_, err = client.Add(ctx, &gogentv1.AddEntryRequest{
		Item: memoryItemToProto(&item),
	})
	return err
}

// AddBatch stores multiple memory items by calling Add for each.
func (m *ProcessMemory) AddBatch(ctx context.Context, items []MemoryItem) error {
	for _, item := range items {
		if err := m.Add(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// Query searches memory via gRPC.
func (m *ProcessMemory) Query(ctx context.Context, q Query) ([]MemoryItem, error) {
	tracer := otel.Tracer("gogent.memory")
	ctx, span := tracer.Start(ctx, "memory.query",
		trace.WithAttributes(
			attribute.String("query", q.Text),
		),
	)
	defer span.End()

	client, err := m.getClient()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	resp, err := client.Query(ctx, &gogentv1.QueryRequest{
		Query: queryToProto(&q),
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return memoryItemsFromProto(resp.Items), nil
}

// Get retrieves a single memory item by id.
// Not supported by gRPC MemoryService; returns an error.
func (m *ProcessMemory) Get(ctx context.Context, id string) (MemoryItem, error) {
	return MemoryItem{}, fmt.Errorf("memory: Get not supported via gRPC")
}

// Delete removes a memory item by id.
// Not supported by gRPC MemoryService; returns an error.
func (m *ProcessMemory) Delete(ctx context.Context, id string) error {
	return fmt.Errorf("memory: Delete not supported via gRPC")
}

// Clear removes all memory entries via gRPC.
func (m *ProcessMemory) Clear(ctx context.Context) error {
	client, err := m.getClient()
	if err != nil {
		return err
	}
	_, err = client.Clear(ctx, &gogentv1.MemoryClearRequest{})
	return err
}

// Count returns the number of entries.
// Not supported by gRPC MemoryService; returns an error.
func (m *ProcessMemory) Count(ctx context.Context) (int64, error) {
	return 0, fmt.Errorf("memory: Count not supported via gRPC")
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func memoryItemToProto(item *MemoryItem) *gogentv1.MemoryItem {
	if item == nil {
		return nil
	}
	var extra *structpb.Struct
	if item.Extra != nil {
		extra, _ = structpb.NewStruct(item.Extra)
	}
	return &gogentv1.MemoryItem{
		Id:        item.ID,
		Content:   item.Content,
		Extra:     extra,
		Embedding: item.Embedding,
		CreatedAt: timestamppb.New(item.CreatedAt),
		Score:     item.Score,
	}
}

func queryToProto(q *Query) *gogentv1.MemoryQuery {
	if q == nil {
		return nil
	}
	var filter *structpb.Struct
	if q.Filter != nil {
		filter, _ = structpb.NewStruct(q.Filter)
	}
	return &gogentv1.MemoryQuery{
		Text:     q.Text,
		TopK:     int32(q.TopK),
		Filter:   filter,
		MinScore: q.MinScore,
	}
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func memoryItemFromProto(item *gogentv1.MemoryItem) MemoryItem {
	if item == nil {
		return MemoryItem{}
	}
	mi := MemoryItem{
		ID:        item.Id,
		Content:   item.Content,
		Embedding: item.Embedding,
		Score:     item.Score,
	}
	if item.CreatedAt != nil {
		mi.CreatedAt = item.CreatedAt.AsTime()
	}
	if item.Extra != nil {
		mi.Extra = item.Extra.AsMap()
	}
	return mi
}

func memoryItemsFromProto(items []*gogentv1.MemoryItem) []MemoryItem {
	result := make([]MemoryItem, len(items))
	for i, item := range items {
		result[i] = memoryItemFromProto(item)
	}
	return result
}
