package native_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
)

type mockContextManager struct {
	sessions map[string][]contextmanager.ContextMessage
	counter  int
}

func newMockContextManager() *mockContextManager {
	return &mockContextManager{sessions: make(map[string][]contextmanager.ContextMessage)}
}

func (m *mockContextManager) NewSession() string {
	m.counter++
	return "session-" + string(rune('0'+m.counter))
}

func (m *mockContextManager) AddMessage(sessionID string, msg contextmanager.ContextMessage) error {
	m.sessions[sessionID] = append(m.sessions[sessionID], msg)
	return nil
}

func (m *mockContextManager) GetMessages(sessionID string) []contextmanager.ContextMessage {
	return m.sessions[sessionID]
}

func (m *mockContextManager) GetSummary(sessionID string) (contextmanager.Summary, error) {
	msgs := m.sessions[sessionID]
	return contextmanager.Summary{
		Content:   "summary of " + string(rune('0'+len(msgs))) + " messages",
		KeyPoints: []string{"point1"},
	}, nil
}

func (m *mockContextManager) BuildSystemPrompt(sessionID string) string {
	return "You are a helpful assistant."
}

func (m *mockContextManager) Clear(sessionID string) error {
	delete(m.sessions, sessionID)
	return nil
}

func (m *mockContextManager) DeleteSession(sessionID string) {
	delete(m.sessions, sessionID)
}

func (m *mockContextManager) ListSessions() []string {
	var ids []string
	for id := range m.sessions {
		ids = append(ids, id)
	}
	return ids
}

func (m *mockContextManager) BuildInput(sessionID string, messages []contextmanager.ContextMessage) []contextmanager.ContextMessage {
	input := make([]contextmanager.ContextMessage, 0, len(messages)+8)
	input = append(input, contextmanager.ContextMessage{Role: "system", Content: m.BuildSystemPrompt(sessionID)})
	input = append(input, m.GetMessages(sessionID)...)
	input = append(input, messages...)
	return input
}

func TestContextManagerComponent(t *testing.T) {
	cm := newMockContextManager()
	comp := contextmanager.NewComponent("test-context", cm)
	ctx := context.Background()

	if got := comp.GetName(); got != "test-context" {
		t.Errorf("GetName() = %q, want %q", got, "test-context")
	}
	if got := comp.GetType(); got != component.ComponentContextManager {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentContextManager)
	}

	if err := comp.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	sid := comp.NewSession()
	if sid == "" {
		t.Fatal("NewSession() returned empty")
	}

	msg := contextmanager.ContextMessage{Role: "user", Content: "hello"}
	if err := comp.AddMessage(sid, msg); err != nil {
		t.Fatalf("AddMessage() = %v", err)
	}

	msgs := comp.GetMessages(sid)
	if len(msgs) != 1 {
		t.Fatalf("GetMessages() length = %d, want 1", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("GetMessages()[0].Content = %q", msgs[0].Content)
	}

	systemPrompt := comp.BuildSystemPrompt(sid)
	if systemPrompt == "" {
		t.Error("BuildSystemPrompt() returned empty")
	}

	summary, err := comp.GetSummary(sid)
	if err != nil {
		t.Fatalf("GetSummary() = %v", err)
	}
	if summary.Content == "" {
		t.Error("GetSummary().Content is empty")
	}

	if err := comp.Clear(sid); err != nil {
		t.Fatalf("Clear() = %v", err)
	}
	if msgs := comp.GetMessages(sid); len(msgs) != 0 {
		t.Errorf("GetMessages() after Clear = %d, want 0", len(msgs))
	}

	comp.DeleteSession(sid)
}
