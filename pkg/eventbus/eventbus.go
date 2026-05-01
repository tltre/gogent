package eventbus

import "context"

type Topic string

type Event struct {
	Topic   Topic
	Key     string
	Payload any
}

type Subscription interface {
	Events() <-chan Event
	Close() error
}

type IEventBus interface {
	Publish(ctx context.Context, topic Topic, event Event) error
	Subscribe(ctx context.Context, topic Topic) (Subscription, error)
	Unsubscribe(sub Subscription) error
}
