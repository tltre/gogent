package eventbus

import (
	"context"
	"sync"
)

type NativeEventBus struct {
	mu          sync.RWMutex
	subscribers map[Topic][]*nativeSubscription
}

func NewNativeEventBus() *NativeEventBus {
	return &NativeEventBus{
		subscribers: make(map[Topic][]*nativeSubscription),
	}
}

type nativeSubscription struct {
	topic Topic
	ch    chan Event
	bus   *NativeEventBus
}

func (s *nativeSubscription) Events() <-chan Event {
	return s.ch
}

func (s *nativeSubscription) Close() error {
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

func (b *NativeEventBus) Publish(ctx context.Context, topic Topic, event Event) error {
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

func (b *NativeEventBus) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &nativeSubscription{
		topic: topic,
		ch:    make(chan Event, 100),
		bus:   b,
	}
	b.subscribers[topic] = append(b.subscribers[topic], sub)
	return sub, nil
}

func (b *NativeEventBus) Unsubscribe(sub Subscription) error {
	return sub.Close()
}
