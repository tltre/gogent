package provider

import (
	"context"

	"github.com/tltre/gagent/pkg/component"
)

type ProviderComponent struct {
	name     string
	provider IProvider
}

func NewComponent(name string, provider IProvider) *ProviderComponent {
	return &ProviderComponent{
		name:     name,
		provider: provider,
	}
}

func (c *ProviderComponent) GetName() string {
	return c.name
}

func (c *ProviderComponent) GetType() component.ComponentType {
	return component.ComponentProvider
}

func (c *ProviderComponent) Initialize(ctx context.Context, deps *component.Registry) error {
	return nil
}

func (c *ProviderComponent) Start(ctx context.Context) error {
	return nil
}

func (c *ProviderComponent) Stop(ctx context.Context) error {
	return nil
}

func (c *ProviderComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *ProviderComponent) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	return c.provider.Generate(ctx, messages)
}

func (c *ProviderComponent) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	return c.provider.Stream(ctx, messages)
}

func (c *ProviderComponent) ModelInfo() ModelInfo {
	return c.provider.ModelInfo()
}
