package provider

import (
	"context"
	"sync"
)

type DefaultProvider struct {
	name        string
	generateFn  func(ctx context.Context, messages []ProviderMessage) (Response, error)
	streamFn    func(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
	modelInfoFn func() ModelInfo
	mu          sync.RWMutex
}

func NewDefaultProvider(name string) *DefaultProvider {
	return &DefaultProvider{
		name: name,
	}
}

func (p *DefaultProvider) SetGenerate(fn func(ctx context.Context, messages []ProviderMessage) (Response, error)) *DefaultProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generateFn = fn
	return p
}

func (p *DefaultProvider) SetStream(fn func(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)) *DefaultProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streamFn = fn
	return p
}

func (p *DefaultProvider) SetModelInfo(fn func() ModelInfo) *DefaultProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modelInfoFn = fn
	return p
}

func (p *DefaultProvider) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.generateFn != nil {
		return p.generateFn(ctx, messages)
	}
	return Response{}, nil
}

func (p *DefaultProvider) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.streamFn != nil {
		return p.streamFn(ctx, messages)
	}
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (p *DefaultProvider) ModelInfo() ModelInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.modelInfoFn != nil {
		return p.modelInfoFn()
	}
	return ModelInfo{Name: p.name}
}
