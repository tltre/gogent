package provider

import (
	"context"

	"github.com/yourorg/gagent/pkg/component"
)

type Component struct {
	name     string
	provider Provider
}

func NewComponent(name string, provider Provider) *Component {
	return &Component{
		name:     name,
		provider: provider,
	}
}

func (c *Component) Name() string {
	return c.name
}

func (c *Component) Type() component.ComponentType {
	return component.ComponentProvider
}

func (c *Component) Initialize(ctx context.Context, deps component.Dependencies) error {
	return nil
}

func (c *Component) Start(ctx context.Context) error {
	return nil
}

func (c *Component) Stop(ctx context.Context) error {
	return nil
}

func (c *Component) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *Component) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	return c.provider.Generate(ctx, messages)
}

func (c *Component) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	return c.provider.Stream(ctx, messages)
}

func (c *Component) ModelInfo() ModelInfo {
	return c.provider.ModelInfo()
}
