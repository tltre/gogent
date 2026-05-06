package eventbus

import (
	"context"

	"github.com/tltre/gogent/internal/client"
)

type ProcessEventBusConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessEventBus struct {
	tr *client.LazyTransport
}

func NewProcessEventBus(cfg *ProcessEventBusConfig) *ProcessEventBus {
	return &ProcessEventBus{tr: client.WrapLazy(cfg.Transport)}
}

func (b *ProcessEventBus) Publish(ctx context.Context, topic Topic, event Event) error {
	return b.tr.Call(ctx, "eventbus/publish", map[string]any{
		"topic": topic,
		"event": event,
	}, nil)
}

func (b *ProcessEventBus) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	return nil, nil
}

func (b *ProcessEventBus) Unsubscribe(sub Subscription) error {
	return nil
}
