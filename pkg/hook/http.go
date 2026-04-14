package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpHookConfig struct {
	Name     string
	Endpoint string
	Events   []EventType
	Timeout  time.Duration
}

type HttpHook struct {
	config *HttpHookConfig
	client *http.Client
}

func NewHttpHook(cfg *HttpHookConfig) *HttpHook {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HttpHook{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (h *HttpHook) OnEvent(ctx context.Context, event Event) (context.Context, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return ctx, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.config.Endpoint+"/hook", bytes.NewReader(body))
	if err != nil {
		return ctx, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return ctx, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return ctx, fmt.Errorf("http error: %d - %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		ContextValues map[string]any `json:"context_values"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ctx, err
	}

	for k, v := range result.ContextValues {
		ctx = context.WithValue(ctx, k, v)
	}
	return ctx, nil
}

func (h *HttpHook) Events() []EventType {
	return h.config.Events
}
