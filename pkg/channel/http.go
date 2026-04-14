package channel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpChannel struct {
	name     string
	endpoint string
	client   *http.Client
	msgChan  chan Message
	stopChan chan struct{}
}

type HttpChannelConfig struct {
	Name       string
	Endpoint   string
	Timeout    time.Duration
	BufferSize int
}

func NewHttpChannel(cfg *HttpChannelConfig) *HttpChannel {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 100
	}
	return &HttpChannel{
		name:     cfg.Name,
		endpoint: cfg.Endpoint,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
		msgChan:  make(chan Message, cfg.BufferSize),
		stopChan: make(chan struct{}),
	}
}

func (c *HttpChannel) Name() string {
	return c.name
}

func (c *HttpChannel) Start(ctx context.Context) error {
	go c.poll(ctx)
	return nil
}

func (c *HttpChannel) Stop(ctx context.Context) error {
	close(c.stopChan)
	return nil
}

func (c *HttpChannel) Receive(ctx context.Context) (<-chan Message, error) {
	return c.msgChan, nil
}

func (c *HttpChannel) Send(ctx context.Context, msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http error: %d - %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *HttpChannel) poll(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			messages, err := c.fetchMessages(ctx)
			if err != nil {
				continue
			}
			for _, msg := range messages {
				select {
				case c.msgChan <- msg:
				case <-c.stopChan:
					return
				case <-ctx.Done():
					return
				}
			}
		case <-c.stopChan:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (c *HttpChannel) fetchMessages(ctx context.Context) ([]Message, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/receive", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}

	var messages []Message
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, err
	}

	return messages, nil
}
