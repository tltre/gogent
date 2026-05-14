package eventbus

import (
	"context"
	"fmt"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

var subIDCounter atomic.Int64

// ProcessEventBusConfig holds the configuration for a gRPC-based ProcessEventBus.
type ProcessEventBusConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessEventBus is an event bus implementation that communicates with a
// remote event bus service over gRPC.
type ProcessEventBus struct {
	cfg    *ProcessEventBusConfig
	client gogentv1.EventBusServiceClient
}

// NewProcessEventBus creates a new ProcessEventBus. The gRPC client is lazily
// initialized on the first method call.
func NewProcessEventBus(cfg *ProcessEventBusConfig) *ProcessEventBus {
	return &ProcessEventBus{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC EventBusServiceClient.
func (b *ProcessEventBus) getClient() (gogentv1.EventBusServiceClient, error) {
	if b.client != nil {
		return b.client, nil
	}
	conn, err := b.cfg.Pool.Get(b.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("eventbus: get connection: %w", err)
	}
	b.client = gogentv1.NewEventBusServiceClient(conn.ClientConn())
	return b.client, nil
}

// Publish sends an event to the remote event bus via gRPC.
func (b *ProcessEventBus) Publish(ctx context.Context, topic Topic, event Event) error {
	client, err := b.getClient()
	if err != nil {
		return err
	}

	payload, err := structpb.NewValue(event.Payload)
	if err != nil {
		payload, _ = structpb.NewValue(nil)
	}

	_, err = client.Publish(ctx, &gogentv1.PublishRequest{
		Topic: string(topic),
		Event: &gogentv1.Event{
			Topic:   string(event.Topic),
			Key:     event.Key,
			Payload: payload,
		},
	})
	return err
}

// Subscribe establishes a server-streaming subscription to the given topic.
// The returned Subscription delivers events on its Events() channel.
func (b *ProcessEventBus) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	client, err := b.getClient()
	if err != nil {
		return nil, err
	}

	stream, err := client.Subscribe(ctx, &gogentv1.SubscribeRequest{
		Topic: string(topic),
	})
	if err != nil {
		return nil, fmt.Errorf("eventbus: subscribe %s: %w", topic, err)
	}

	subID := fmt.Sprintf("sub-%d", subIDCounter.Add(1))
	sub := &grpcEventSubscription{
		id:     subID,
		stream: stream,
		ch:     make(chan Event, 100),
		ctx:    ctx,
		bus:    b,
	}

	// Start a goroutine that reads from the gRPC stream and pushes to the channel.
	go sub.loop()
	return sub, nil
}

// Unsubscribe cancels the subscription by closing its gRPC stream.
func (b *ProcessEventBus) Unsubscribe(sub Subscription) error {
	if grpcSub, ok := sub.(*grpcEventSubscription); ok {
		return grpcSub.close()
	}
	return nil
}

// grpcEventSubscription implements the Subscription interface by wrapping a
// gRPC server-streaming client.
type grpcEventSubscription struct {
	id     string
	stream grpc.ServerStreamingClient[gogentv1.Event]
	ch     chan Event
	ctx    context.Context
	bus    *ProcessEventBus
}

func (s *grpcEventSubscription) Events() <-chan Event {
	return s.ch
}

func (s *grpcEventSubscription) Close() error {
	return s.close()
}

func (s *grpcEventSubscription) close() error {
	defer close(s.ch)
	return s.stream.CloseSend()
}

// loop reads events from the gRPC stream and sends them on the channel.
func (s *grpcEventSubscription) loop() {
	defer close(s.ch)
	for {
		evt, err := s.stream.Recv()
		if err != nil {
			return
		}
		select {
		case s.ch <- protoEventToEvent(evt):
		case <-s.ctx.Done():
			return
		}
	}
}

// --- conversion helpers ---

func protoEventToEvent(evt *gogentv1.Event) Event {
	e := Event{
		Topic: Topic(evt.Topic),
		Key:   evt.Key,
	}
	if evt.Payload != nil {
		e.Payload = evt.Payload.AsInterface()
	}
	return e
}
