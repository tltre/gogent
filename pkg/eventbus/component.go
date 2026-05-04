package eventbus

import (
	"context"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/logger"
)

type EventBusComponent struct {
	component.BasicComponent
	eventBus IEventBus
}

func NewComponent(name string, eventBus IEventBus) *EventBusComponent {
	return &EventBusComponent{
		BasicComponent: component.NewBasicComponent(name),
		eventBus:       eventBus,
	}
}

func (c *EventBusComponent) GetType() component.ComponentType {
	return component.ComponentEventBus
}

func (c *EventBusComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	c.log(ctx, logger.InfoLevel, "eventbus initialized")
	return nil
}

func (c *EventBusComponent) Start(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "eventbus started")
	return nil
}

func (c *EventBusComponent) Stop(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "eventbus stopped")
	return nil
}

func (c *EventBusComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *EventBusComponent) Publish(ctx context.Context, topic Topic, event Event) error {
	c.log(ctx, logger.DebugLevel, "eventbus publish",
		logger.Field{Key: "topic", Value: string(topic)},
		logger.Field{Key: "key", Value: event.Key},
	)
	return c.eventBus.Publish(ctx, topic, event)
}

func (c *EventBusComponent) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	c.log(ctx, logger.DebugLevel, "eventbus subscribe",
		logger.Field{Key: "topic", Value: string(topic)},
	)
	return c.eventBus.Subscribe(ctx, topic)
}

func (c *EventBusComponent) Unsubscribe(sub Subscription) error {
	return c.eventBus.Unsubscribe(sub)
}

func (c *EventBusComponent) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
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
