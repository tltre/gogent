package logger

import (
	"context"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/eventbus"
)

var defaultLogger Logger = NewDefault()

func Default() Logger { return defaultLogger }

func SetDefault(l Logger) { defaultLogger = l }

func SyncDefault() error {
	if zl, ok := defaultLogger.(*ZapLogger); ok {
		return zl.Sync()
	}
	return nil
}

type LoggerComponent struct {
	name     string
	impl     Logger
	eventBus eventbus.IEventBus
	sub      eventbus.Subscription
}

func NewComponent(name string, l Logger) *LoggerComponent {
	return &LoggerComponent{name: name, impl: l}
}

func (c *LoggerComponent) GetName() string       { return c.name }
func (c *LoggerComponent) GetType() component.ComponentType { return component.ComponentLogger }

func (c *LoggerComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	if eb, ok := registry.GetDefault(component.ComponentEventBus).(eventbus.IEventBus); ok && eb != nil {
		c.eventBus = eb
	}
	return nil
}

func (c *LoggerComponent) Start(_ context.Context) error {
	if c.eventBus == nil {
		return nil
	}
	sub, err := c.eventBus.Subscribe(context.Background(), "system.log")
	if err != nil {
		return err
	}
	c.sub = sub
	go c.processEvents()
	return nil
}

func (c *LoggerComponent) Stop(_ context.Context) error {
	if c.sub != nil {
		c.sub.Close()
	}
	SyncDefault()
	return nil
}

func (c *LoggerComponent) Dependencies() map[string]component.DependencySpec {
	return map[string]component.DependencySpec{
		"eventBus": {Type: component.ComponentEventBus, Required: true},
	}
}

func (c *LoggerComponent) processEvents() {
	for ev := range c.sub.Events() {
		if logEvent, ok := ev.Payload.(LogEvent); ok {
			entry := logEvent.Entry
			if logEvent.TraceID != "" {
				entry.Fields = append(entry.Fields, Field{Key: "traceId", Value: logEvent.TraceID})
			}
			c.impl.Log(context.Background(), entry)
		}
	}
}
