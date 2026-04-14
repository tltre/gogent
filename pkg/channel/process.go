package channel

import (
	"context"

	"github.com/yourorg/gagent/pkg/protocol/mcp"
)

type ProcessChannel struct {
	name     string
	client   *mcp.Client
	msgChan  chan Message
	stopChan chan struct{}
}

type ProcessChannelConfig struct {
	Name       string
	Command    []string
	BufferSize int
}

func NewProcessChannel(cfg *ProcessChannelConfig) (*ProcessChannel, error) {
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 100
	}

	client, err := mcp.NewClient(cfg.Command...)
	if err != nil {
		return nil, err
	}

	return &ProcessChannel{
		name:     cfg.Name,
		client:   client,
		msgChan:  make(chan Message, cfg.BufferSize),
		stopChan: make(chan struct{}),
	}, nil
}

func (c *ProcessChannel) Name() string {
	return c.name
}

func (c *ProcessChannel) Start(ctx context.Context) error {
	return c.client.Start(ctx)
}

func (c *ProcessChannel) Stop(ctx context.Context) error {
	close(c.stopChan)
	return c.client.Stop(ctx)
}

func (c *ProcessChannel) Receive(ctx context.Context) (<-chan Message, error) {
	return c.msgChan, nil
}

func (c *ProcessChannel) Send(ctx context.Context, msg Message) error {
	params := map[string]any{
		"id":        msg.ID,
		"sessionId": msg.SessionID,
		"type":      msg.Type,
		"content":   msg.Content,
		"metadata":  msg.Metadata,
	}

	_, err := c.client.CallTool(ctx, "send_message", params)
	return err
}
