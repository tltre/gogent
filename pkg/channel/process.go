package channel

import (
	"context"

	"github.com/tltre/gagent/internal/client"
)

type ProcessChannelConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessChannel struct {
	name     string
	tr       *client.LazyTransport
	msgChan  chan Message
	stopChan chan struct{}
}

func NewProcessChannel(cfg *ProcessChannelConfig) *ProcessChannel {
	return &ProcessChannel{
		name:     cfg.Name,
		tr:       client.WrapLazy(cfg.Transport),
		msgChan:  make(chan Message, 100),
		stopChan: make(chan struct{}),
	}
}

func (c *ProcessChannel) Name() string {
	return c.name
}

func (c *ProcessChannel) Start(ctx context.Context) error { return nil }

func (c *ProcessChannel) Stop(ctx context.Context) error {
	close(c.stopChan)
	return c.tr.Close()
}

func (c *ProcessChannel) Receive(ctx context.Context) (<-chan Message, error) {
	return c.msgChan, nil
}

func (c *ProcessChannel) Send(ctx context.Context, msg Message) error {
	return c.tr.Call(ctx, "channel/send", msg, nil)
}
