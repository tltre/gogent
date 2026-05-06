package contextmanager

import (
	"context"

	"github.com/tltre/gogent/internal/client"
)

type ProcessContextManagerConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessContextManager struct {
	tr *client.LazyTransport
}

func NewProcessContextManager(cfg *ProcessContextManagerConfig) *ProcessContextManager {
	return &ProcessContextManager{tr: client.WrapLazy(cfg.Transport)}
}

func (c *ProcessContextManager) NewSession() string {
	var id string
	_ = c.tr.Call(context.Background(), "context/newSession", nil, &id)
	return id
}

func (c *ProcessContextManager) AddMessage(sessionId string, msg ContextMessage) error {
	return c.tr.Call(context.Background(), "context/addMessage", map[string]any{
		"sessionId": sessionId,
		"message":   msg,
	}, nil)
}

func (c *ProcessContextManager) GetMessages(sessionId string) []ContextMessage {
	var result []ContextMessage
	_ = c.tr.Call(context.Background(), "context/getMessages", sessionId, &result)
	return result
}

func (c *ProcessContextManager) GetSummary(sessionId string) (Summary, error) {
	var result Summary
	if err := c.tr.Call(context.Background(), "context/getSummary", sessionId, &result); err != nil {
		return Summary{}, err
	}
	return result, nil
}

func (c *ProcessContextManager) BuildSystemPrompt(sessionId string) string {
	var result string
	_ = c.tr.Call(context.Background(), "context/buildSystemPrompt", sessionId, &result)
	return result
}

func (c *ProcessContextManager) Clear(sessionId string) error {
	return c.tr.Call(context.Background(), "context/clear", sessionId, nil)
}

func (c *ProcessContextManager) DeleteSession(sessionId string) {
	_ = c.tr.Call(context.Background(), "context/deleteSession", sessionId, nil)
}
