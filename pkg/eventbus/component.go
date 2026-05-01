package eventbus

import (
	"context"

	"github.com/tltre/gagent/pkg/component"
)

type EventBusComponent struct {
	name     string
	eventBus IEventBus
}

func NewComponent(name string, eventBus IEventBus) *EventBusComponent {
	return &EventBusComponent{
		name:     name,
		eventBus: eventBus,
	}
}

func (c *EventBusComponent) GetName() string {
	return c.name
}

func (c *EventBusComponent) GetType() component.ComponentType {
	return component.ComponentEventBus
}

func (c *EventBusComponent) Initialize(ctx context.Context, registry *component.Registry) error {
	return nil
}

func (c *EventBusComponent) Start(ctx context.Context) error {
	return nil
}

func (c *EventBusComponent) Stop(ctx context.Context) error {
	return nil
}

func (c *EventBusComponent) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *EventBusComponent) Publish(ctx context.Context, topic Topic, event Event) error {
	return c.eventBus.Publish(ctx, topic, event)
}

func (c *EventBusComponent) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	return c.eventBus.Subscribe(ctx, topic)
}

func (c *EventBusComponent) Unsubscribe(sub Subscription) error {
	return c.eventBus.Unsubscribe(sub)
}
