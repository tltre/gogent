package eventbus

import (
	"context"
	"sync"
)

type DefaultEventBus struct {
	mu          sync.RWMutex
	subscribers map[Topic][]*defaultSubscription
}

func NewDefaultEventBus() *DefaultEventBus {
	return &DefaultEventBus{
		subscribers: make(map[Topic][]*defaultSubscription),
	}
}

type defaultSubscription struct {
	topic Topic
	ch    chan Event
	bus   *DefaultEventBus
}

func (s *defaultSubscription) Events() <-chan Event {
	return s.ch
}

func (s *defaultSubscription) Close() error {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()

	subs := s.bus.subscribers[s.topic]
	for i, sub := range subs {
		if sub == s {
			s.bus.subscribers[s.topic] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	close(s.ch)
	return nil
}

func (b *DefaultEventBus) Publish(ctx context.Context, topic Topic, event Event) error {
	b.mu.RLock()
	subs := b.subscribers[topic]
	b.mu.RUnlock()

	for _, sub := range subs {
		select {
		case sub.ch <- event:
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

func (b *DefaultEventBus) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &defaultSubscription{
		topic: topic,
		ch:    make(chan Event, 100),
		bus:   b,
	}
	b.subscribers[topic] = append(b.subscribers[topic], sub)
	return sub, nil
}

func (b *DefaultEventBus) Unsubscribe(sub Subscription) error {
	return sub.Close()
}
