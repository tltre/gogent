package contextmanager

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/memory"
)

// DefaultContextManager is the framework's in-memory ContextManager (v0.15.2).
//
// It embeds BasicComponent so it can hold a Registry reference: the Builder
// wraps it in a ContextManagerComponent, whose Initialize injects the registry
// (see RegistryAware). BuildInput then resolves the Memory component through
// the registry — consistent with the overall component-dependency design.
//
// Scope: process-local session storage (not persisted). Good enough to run the
// full agent flow; durable/rich context management is a follow-up.
type DefaultContextManager struct {
	component.BasicComponent
	mu        sync.RWMutex
	sessions  map[string][]ContextMessage
	summaries map[string]Summary
	counter   int64
}

var _ IContextManager = (*DefaultContextManager)(nil)

// NewDefaultContextManager creates an empty in-memory context manager.
func NewDefaultContextManager() *DefaultContextManager {
	return &DefaultContextManager{
		sessions:  make(map[string][]ContextMessage),
		summaries: make(map[string]Summary),
	}
}

// registryAware is implemented by managers that need the Registry to resolve
// their dependencies (Memory). ContextManagerComponent.Initialize injects it.
type registryAware interface {
	SetRegistry(*component.Registry)
}

// ---------------------------------------------------------------------------
// IContextManager
// ---------------------------------------------------------------------------

func (c *DefaultContextManager) NewSession() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counter++
	id := fmt.Sprintf("session-%d", c.counter)
	c.sessions[id] = nil
	return id
}

func (c *DefaultContextManager) AddMessage(sessionId string, msg ContextMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessions[sessionId] = append(c.sessions[sessionId], msg)
	return nil
}

func (c *DefaultContextManager) GetMessages(sessionId string) []ContextMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	msgs := c.sessions[sessionId]
	result := make([]ContextMessage, len(msgs))
	copy(result, msgs)
	return result
}

func (c *DefaultContextManager) GetSummary(sessionId string) (Summary, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.summaries[sessionId]
	if !ok {
		return Summary{}, fmt.Errorf("summary not found for session %s", sessionId)
	}
	return s, nil
}

// BuildSystemPrompt constructs a simple system prompt from the session history.
func (c *DefaultContextManager) BuildSystemPrompt(sessionId string) string {
	msgs := c.GetMessages(sessionId)
	if len(msgs) == 0 {
		return "You are a helpful agent. Use tools when appropriate."
	}
	var b strings.Builder
	b.WriteString("You are a helpful agent. Use tools when appropriate.\n\nSession history:\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Content)
	}
	return b.String()
}

func (c *DefaultContextManager) Clear(sessionId string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, sessionId)
	delete(c.summaries, sessionId)
	return nil
}

func (c *DefaultContextManager) DeleteSession(sessionId string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, sessionId)
	delete(c.summaries, sessionId)
}

func (c *DefaultContextManager) ListSessions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]string, 0, len(c.sessions))
	for id := range c.sessions {
		ids = append(ids, id)
	}
	return ids
}

// ---------------------------------------------------------------------------
// BuildInput (agent's single input source)
// ---------------------------------------------------------------------------

// BuildInput assembles the complete model input for an agent run:
// system prompt (with recalled memory) + session history + user messages.
func (c *DefaultContextManager) BuildInput(sessionId string, messages []ContextMessage) []ContextMessage {
	input := make([]ContextMessage, 0, len(messages)+8)

	if sys := c.buildSystemPromptWithMemory(sessionId, messages); sys != "" {
		input = append(input, ContextMessage{Role: "system", Content: sys})
	}
	input = append(input, c.GetMessages(sessionId)...)
	input = append(input, messages...)
	return input
}

// buildSystemPromptWithMemory constructs the system prompt, injecting relevant
// long-term memory recalled for the user's latest message.
func (c *DefaultContextManager) buildSystemPromptWithMemory(sessionId string, userMsgs []ContextMessage) string {
	base := c.BuildSystemPrompt(sessionId)

	// Recall relevant memory for the last user message.
	if mem := c.memory(); mem != nil && len(userMsgs) > 0 {
		last := userMsgs[len(userMsgs)-1].Content
		items, err := mem.Query(context.Background(), memory.Query{Text: last, TopK: 5})
		if err == nil && len(items) > 0 {
			var b strings.Builder
			b.WriteString(base)
			b.WriteString("\n\nRelevant memory:\n")
			for _, it := range items {
				b.WriteString("- " + it.Content + "\n")
			}
			return b.String()
		}
	}
	return base
}

// memory resolves the Memory component through the registry (injected by the
// wrapping ContextManagerComponent). Returns nil when unavailable.
func (c *DefaultContextManager) memory() memory.IMemory {
	if c.Registry() == nil {
		return nil
	}
	comp := c.Registry().GetDefault(component.ComponentMemory)
	if comp == nil {
		return nil
	}
	if mc, ok := comp.(*memory.MemoryComponent); ok {
		return mc
	}
	return nil
}
