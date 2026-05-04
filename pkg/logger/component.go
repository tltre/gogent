package logger

import (
	"context"

	"github.com/tltre/gagent/pkg/component"
)

var defaultLogger Logger = NewDefault()

func Default() Logger { return defaultLogger }

func SetDefault(l Logger) { defaultLogger = l }

func SyncDefault() error {
	if zl, ok := defaultLogger.(*DefaultLogger); ok {
		return zl.Sync()
	}
	return nil
}

type LoggerComponent struct {
	component.BasicComponent
	impl Logger
}

func NewComponent(name string, l Logger) *LoggerComponent {
	return &LoggerComponent{
		BasicComponent: component.NewBasicComponent(name),
		impl:           l,
	}
}

func (c *LoggerComponent) GetType() component.ComponentType { return component.ComponentLogger }

func (c *LoggerComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	return nil
}

func (c *LoggerComponent) Start(_ context.Context) error {
	return nil
}

func (c *LoggerComponent) Stop(_ context.Context) error {
	SyncDefault()
	return nil
}

func (c *LoggerComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *LoggerComponent) Log(ctx context.Context, entry LogEntry) {
	c.impl.Log(ctx, entry)
}

func (c *LoggerComponent) Logger() Logger {
	return c.impl
}
