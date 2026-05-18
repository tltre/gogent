package provider

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	"github.com/tltre/gogent/internal/otel"
)

// ProcessProviderConfig holds the configuration for a gRPC-based ProcessProvider.
type ProcessProviderConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessProvider is a provider implementation that communicates with a remote
// provider service over gRPC.
type ProcessProvider struct {
	cfg    *ProcessProviderConfig
	client gogentv1.ProviderServiceClient
}

// NewProcessProvider creates a new ProcessProvider. The gRPC client is lazily
// initialized on the first method call.
func NewProcessProvider(cfg *ProcessProviderConfig) *ProcessProvider {
	return &ProcessProvider{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC ProviderServiceClient.
func (p *ProcessProvider) getClient() (gogentv1.ProviderServiceClient, error) {
	if p.client != nil {
		return p.client, nil
	}
	conn, err := p.cfg.Pool.Get(p.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("provider: get connection: %w", err)
	}
	p.client = gogentv1.NewProviderServiceClient(conn.ClientConn())
	return p.client, nil
}

// Generate sends a generation request to the remote provider via gRPC.
func (p *ProcessProvider) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	tracer := otel.Tracer("gogent.provider")
	ctx, span := tracer.Start(ctx, "provider.generate",
		trace.WithAttributes(
			attribute.Int("msg_count", len(messages)),
		),
	)
	defer span.End()

	client, err := p.getClient()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Response{}, err
	}
	req := &gogentv1.GenerateRequest{
		Messages: messagesToProto(messages),
	}
	resp, err := client.Generate(ctx, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return Response{}, err
	}
	return generateResponseFromProto(resp), nil
}

// Stream emulates streaming by delegating to Generate. The gRPC proto does not
// yet define a server-streaming RPC, so this falls back to single-shot generation.
func (p *ProcessProvider) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 100)
	go func() {
		defer close(ch)
		result, err := p.Generate(ctx, messages)
		ch <- StreamChunk{Delta: result.Content, Done: true}
		_ = err
	}()
	return ch, nil
}

// ModelInfo retrieves model metadata from the remote provider via gRPC.
func (p *ProcessProvider) ModelInfo() ModelInfo {
	client, err := p.getClient()
	if err != nil {
		return ModelInfo{}
	}
	resp, err := client.ModelInfo(context.Background(), &gogentv1.ModelInfoRequest{})
	if err != nil {
		return ModelInfo{}
	}
	if resp.ModelInfo == nil {
		return ModelInfo{}
	}
	return modelInfoFromProto(resp.ModelInfo)
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func messagesToProto(msgs []ProviderMessage) []*gogentv1.ProviderMessage {
	result := make([]*gogentv1.ProviderMessage, len(msgs))
	for i, m := range msgs {
		result[i] = &gogentv1.ProviderMessage{
			Role:    m.Role,
			Content: m.Content,
			Tools:   toolDefsToProto(m.Tools),
		}
	}
	return result
}

func toolDefsToProto(tools []ToolDefinition) []*gogentv1.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	result := make([]*gogentv1.ToolDefinition, len(tools))
	for i, t := range tools {
		params, _ := structpb.NewValue(t.Parameters)
		result[i] = &gogentv1.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		}
	}
	return result
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func generateResponseFromProto(resp *gogentv1.GenerateResponse) Response {
	r := Response{
		Content:      resp.Content,
		FinishReason: resp.FinishReason,
	}
	for _, tc := range resp.ToolCalls {
		r.ToolCalls = append(r.ToolCalls, toolCallFromProto(tc))
	}
	if resp.Usage != nil {
		r.Usage = usageFromProto(resp.Usage)
	}
	return r
}

func toolCallFromProto(tc *gogentv1.ToolCall) ToolCall {
	c := ToolCall{
		ID:   tc.Id,
		Name: tc.Name,
	}
	if tc.Args != nil {
		c.Args = tc.Args.AsMap()
	}
	return c
}

func usageFromProto(u *gogentv1.Usage) Usage {
	return Usage{
		PromptTokens:     int(u.PromptTokens),
		CompletionTokens: int(u.CompletionTokens),
		TotalTokens:      int(u.TotalTokens),
	}
}

func modelInfoFromProto(info *gogentv1.ModelInfo) ModelInfo {
	return ModelInfo{
		Name:           info.Name,
		Provider:       info.Provider,
		ContextSize:    int(info.ContextSize),
		SupportsTool:   info.SupportsTool,
		SupportsVision: info.SupportsVision,
	}
}
