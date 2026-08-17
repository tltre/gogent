package native_test

import (
	"context"
	"testing"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/eventbus"
)

type mockSubscription struct {
	events chan eventbus.Event
}

func (s *mockSubscription) Events() <-chan eventbus.Event { return s.events }
func (s *mockSubscription) Close() error                  { close(s.events); return nil }

type mockEventBus struct {
	subs map[eventbus.Topic][]*mockSubscription
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{subs: make(map[eventbus.Topic][]*mockSubscription)}
}

func (b *mockEventBus) Publish(_ context.Context, topic eventbus.Topic, ev eventbus.Event) error {
	for _, sub := range b.subs[topic] {
		sub.events <- ev
	}
	return nil
}

func (b *mockEventBus) Subscribe(_ context.Context, topic eventbus.Topic) (eventbus.Subscription, error) {
	sub := &mockSubscription{events: make(chan eventbus.Event, 10)}
	b.subs[topic] = append(b.subs[topic], sub)
	return sub, nil
}

func (b *mockEventBus) Unsubscribe(sub eventbus.Subscription) error {
	sub.Close()
	return nil
}

func TestEventBusComponent(t *testing.T) {
	bus := newMockEventBus()
	comp := eventbus.NewComponent("test-eventbus", bus)
	ctx := context.Background()

	if got := comp.GetName(); got != "test-eventbus" {
		t.Errorf("GetName() = %q, want %q", got, "test-eventbus")
	}
	if got := comp.GetType(); got != component.ComponentEventBus {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentEventBus)
	}

	if err := comp.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	topic := eventbus.Topic("agent.notification")
	sub, err := comp.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Subscribe() = %v", err)
	}

	ev := eventbus.Event{Topic: topic, Key: "key1", Payload: "data"}
	if err := comp.Publish(ctx, topic, ev); err != nil {
		t.Fatalf("Publish() = %v", err)
	}

	select {
	case received := <-sub.Events():
		if received.Payload != "data" {
			t.Errorf("Event.Payload = %v, want data", received.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}

	if err := comp.Unsubscribe(sub); err != nil {
		t.Fatalf("Unsubscribe() = %v", err)
	}
}
