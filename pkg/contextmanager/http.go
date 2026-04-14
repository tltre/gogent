package contextmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type HttpContextManagerConfig struct {
	Name     string
	Endpoint string
	Timeout  time.Duration
}

type HttpContextManager struct {
	config *HttpContextManagerConfig
	client *http.Client
}

func NewHttpContextManager(cfg *HttpContextManagerConfig) *HttpContextManager {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &HttpContextManager{
		config: cfg,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

func (m *HttpContextManager) Create(sessionID string) (Context, error) {
	return &httpContext{
		sessionID: sessionID,
		config:    m.config,
		client:    m.client,
	}, nil
}

func (m *HttpContextManager) Get(sessionID string) (Context, bool) {
	return &httpContext{
		sessionID: sessionID,
		config:    m.config,
		client:    m.client,
	}, true
}

func (m *HttpContextManager) Delete(sessionID string) error {
	ctx := context.Background()
	url := fmt.Sprintf("%s/context/%s", m.config.Endpoint, sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := m.client.Do(req)
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

func (m *HttpContextManager) SetSummaryGenerator(gen SummaryGenerator) {}

func (m *HttpContextManager) List() []string {
	return []string{}
}

type httpContext struct {
	sessionID string
	config    *HttpContextManagerConfig
	client    *http.Client
}

func (c *httpContext) SessionID() string {
	return c.sessionID
}

func (c *httpContext) AddMessage(msg ContextMessage) error {
	ctx := context.Background()
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/context/%s/messages", c.config.Endpoint, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
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

func (c *httpContext) GetMessages() []ContextMessage {
	ctx := context.Background()
	url := fmt.Sprintf("%s/context/%s/messages", c.config.Endpoint, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return []ContextMessage{}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return []ContextMessage{}
	}
	defer resp.Body.Close()

	var messages []ContextMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return []ContextMessage{}
	}

	return messages
}

func (c *httpContext) GetSummary() (Summary, error) {
	return Summary{}, nil
}

func (c *httpContext) GetSystemPrompt() string {
	return ""
}

func (c *httpContext) SetVariable(key string, value any) error {
	ctx := context.Background()
	body, err := json.Marshal(map[string]any{key: value})
	if err != nil {
		return err
	}

	url := fmt.Sprintf("%s/context/%s/variables", c.config.Endpoint, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}

func (c *httpContext) GetVariable(key string) (any, bool) {
	return nil, false
}

func (c *httpContext) GetAllVariables() map[string]any {
	return map[string]any{}
}

func (c *httpContext) Clear() error {
	ctx := context.Background()
	url := fmt.Sprintf("%s/context/%s/clear", c.config.Endpoint, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
