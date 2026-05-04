package provider

import (
	"context"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/logger"
)

type ProviderComponent struct {
	component.BasicComponent
	provider IProvider
}

func NewComponent(name string, provider IProvider) *ProviderComponent {
	return &ProviderComponent{
		BasicComponent: component.NewBasicComponent(name),
		provider:       provider,
	}
}

func (c *ProviderComponent) GetType() component.ComponentType {
	return component.ComponentProvider
}

func (c *ProviderComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	c.log(ctx, logger.InfoLevel, "provider initialized")
	return nil
}

func (c *ProviderComponent) Start(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "provider started")
	return nil
}

func (c *ProviderComponent) Stop(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "provider stopped")
	return nil
}

func (c *ProviderComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *ProviderComponent) Generate(ctx context.Context, messages []ProviderMessage) (Response, error) {
	c.log(ctx, logger.DebugLevel, "generate started",
		logger.Field{Key: "messages", Value: len(messages)},
	)
	start := time.Now()
	resp, err := c.provider.Generate(ctx, messages)
	dur := time.Since(start)
	if err != nil {
		c.log(ctx, logger.ErrorLevel, "generate failed",
			logger.Field{Key: "error", Value: err.Error()},
			logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		)
		return resp, err
	}
	c.log(ctx, logger.InfoLevel, "generate completed",
		logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
	)
	return resp, nil
}

func (c *ProviderComponent) Stream(ctx context.Context, messages []ProviderMessage) (<-chan StreamChunk, error) {
	return c.provider.Stream(ctx, messages)
}

func (c *ProviderComponent) ModelInfo() ModelInfo {
	return c.provider.ModelInfo()
}

func (c *ProviderComponent) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := c.Registry()
	if r == nil {
		return
	}
	lc := r.GetDefault(component.ComponentLogger)
	if lc == nil {
		return
	}
	l, ok := lc.(logger.Logger)
	if !ok {
		return
	}
	l.Log(ctx, logger.LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Module:    c.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
