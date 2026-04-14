package eventbus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpEventBusConfig struct {
	Name     string
	Endpoint string
	Timeout  time.Duration
}

type HttpEventBus struct {
	config *HttpEventBusConfig
	client *http.Client
}

func NewHttpEventBus(cfg *HttpEventBusConfig) *HttpEventBus {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HttpEventBus{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

type httpSubscription struct {
	topic    Topic
	endpoint string
	client   *http.Client
	ch       chan Event
	stopCh   chan struct{}
}

func (s *httpSubscription) Events() <-chan Event {
	return s.ch
}

func (s *httpSubscription) Close() error {
	close(s.stopCh)
	close(s.ch)
	return nil
}

func (b *HttpEventBus) Publish(ctx context.Context, topic Topic, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/publish/%s", b.config.Endpoint, topic)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
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

func (b *HttpEventBus) Subscribe(ctx context.Context, topic Topic) (Subscription, error) {
	sub := &httpSubscription{
		topic:    topic,
		endpoint: b.config.Endpoint,
		client:   b.client,
		ch:       make(chan Event, 100),
		stopCh:   make(chan struct{}),
	}

	go sub.poll(ctx)

	return sub, nil
}

func (b *HttpEventBus) Unsubscribe(sub Subscription) error {
	return sub.Close()
}

func (s *httpSubscription) poll(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			events, err := s.fetchEvents(ctx)
			if err != nil {
				continue
			}
			for _, e := range events {
				select {
				case s.ch <- e:
				case <-s.stopCh:
					return
				case <-ctx.Done():
					return
				}
			}
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

func (s *httpSubscription) fetchEvents(ctx context.Context) ([]Event, error) {
	url := fmt.Sprintf("%s/subscribe/%s", s.endpoint, s.topic)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http error: %d", resp.StatusCode)
	}

	var events []Event
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		return nil, err
	}

	return events, nil
}
