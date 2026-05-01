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
	name      string
	transport client.Transport
	msgChan   chan Message
	stopChan  chan struct{}
}

func NewProcessChannel(cfg *ProcessChannelConfig) *ProcessChannel {
	return &ProcessChannel{
		name:      cfg.Name,
		transport: cfg.Transport,
		msgChan:   make(chan Message, 100),
		stopChan:  make(chan struct{}),
	}
}

func (c *ProcessChannel) Name() string {
	return c.name
}

func (c *ProcessChannel) Start(ctx context.Context) error {
	return c.transport.Start(ctx)
}

func (c *ProcessChannel) Stop(ctx context.Context) error {
	close(c.stopChan)
	return c.transport.Close()
}

func (c *ProcessChannel) Receive(ctx context.Context) (<-chan Message, error) {
	return c.msgChan, nil
}

func (c *ProcessChannel) Send(ctx context.Context, msg Message) error {
	return c.transport.Call(ctx, "channel/send", msg, nil)
}
