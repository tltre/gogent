package channel

import (
	"context"
)

type NativeChannel struct {
	name     string
	msgChan  chan Message
	stopChan chan struct{}
}

func NewNativeChannel(name string, bufferSize int) *NativeChannel {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	return &NativeChannel{
		name:     name,
		msgChan:  make(chan Message, bufferSize),
		stopChan: make(chan struct{}),
	}
}

func (c *NativeChannel) Name() string {
	return c.name
}

func (c *NativeChannel) Start(ctx context.Context) error {
	return nil
}

func (c *NativeChannel) Stop(ctx context.Context) error {
	close(c.stopChan)
	return nil
}

func (c *NativeChannel) Receive(ctx context.Context) (<-chan Message, error) {
	return c.msgChan, nil
}

func (c *NativeChannel) Send(ctx context.Context, msg Message) error {
	select {
	case c.msgChan <- msg:
		return nil
	case <-c.stopChan:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
