package provider

import (
	"context"

	"github.com/tltre/gogent/internal/client"
)

type ProcessProviderConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessProvider struct {
	tr *client.LazyTransport
}

func NewProcessProvider(cfg *ProcessProviderConfig) *ProcessProvider {
	return &ProcessProvider{tr: client.WrapLazy(cfg.Transport)}
}

func (p *ProcessProvider) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	var result Response
	if err := p.tr.Call(ctx, "provider/generate", messages, &result); err != nil {
		return Response{}, err
	}
	return result, nil
}

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

func (p *ProcessProvider) ModelInfo() ModelInfo {
	var result ModelInfo
	_ = p.tr.Call(context.Background(), "provider/modelInfo", nil, &result)
	return result
}
