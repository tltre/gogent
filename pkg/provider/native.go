package provider

import (
	"context"
	"sync"
)

type NativeProvider struct {
	name        string
	generateFn  func(ctx context.Context, messages []ProviderMessage) (Response, error)
	streamFn    func(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)
	modelInfoFn func() ModelInfo
	mu          sync.RWMutex
}

func NewNativeProvider(name string) *NativeProvider {
	return &NativeProvider{
		name: name,
	}
}

func (p *NativeProvider) SetGenerate(fn func(ctx context.Context, messages []ProviderMessage) (Response, error)) *NativeProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.generateFn = fn
	return p
}

func (p *NativeProvider) SetStream(fn func(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error)) *NativeProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.streamFn = fn
	return p
}

func (p *NativeProvider) SetModelInfo(fn func() ModelInfo) *NativeProvider {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.modelInfoFn = fn
	return p
}

func (p *NativeProvider) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.generateFn != nil {
		return p.generateFn(ctx, messages)
	}
	return Response{}, nil
}

func (p *NativeProvider) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.streamFn != nil {
		return p.streamFn(ctx, messages)
	}
	ch := make(chan StreamChunk)
	close(ch)
	return ch, nil
}

func (p *NativeProvider) ModelInfo() ModelInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.modelInfoFn != nil {
		return p.modelInfoFn()
	}
	return ModelInfo{Name: p.name}
}
