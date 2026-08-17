package native_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/channel"
	"github.com/tltre/gogent/pkg/component"
)

type mockChannel struct {
	name    string
	msgChan chan channel.Message
}

func (c *mockChannel) Name() string        { return c.name }
func (c *mockChannel) Start(_ context.Context) error { return nil }
func (c *mockChannel) Stop(_ context.Context) error  { return nil }
func (c *mockChannel) Receive(_ context.Context) (<-chan channel.Message, error) {
	return c.msgChan, nil
}
func (c *mockChannel) Send(ctx context.Context, msg channel.Message) error {
	c.msgChan <- msg
	return nil
}

func TestChannelManager(t *testing.T) {
	ch := &mockChannel{
		name:    "mock-chan",
		msgChan: make(chan channel.Message, 10),
	}
	cm := channel.NewComponent("test-channel", ch)
	ctx := context.Background()

	if got := cm.GetName(); got != "test-channel" {
		t.Errorf("GetName() = %q, want %q", got, "test-channel")
	}
	if got := cm.GetType(); got != component.ComponentChannel {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentChannel)
	}

	if err := cm.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	if err := cm.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}

	msg := channel.Message{
		ID:        "1",
		SessionID: "s1",
		Type:      channel.MessageTypeText,
		Content:   "hello",
	}
	if err := ch.Send(ctx, msg); err != nil {
		t.Fatalf("Send() = %v", err)
	}

	recvCh, err := ch.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive() = %v", err)
	}
	received := <-recvCh
	if received.Content != "hello" {
		t.Errorf("Receive().Content = %q", received.Content)
	}

	if err := cm.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}
