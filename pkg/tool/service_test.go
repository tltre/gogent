package tool

import (
	"context"
	"testing"
)

// Compile-time: ToolManager satisfies Service.
var _ Service = (*ToolManager)(nil)

func TestToolManagerImplementsService(t *testing.T) {
	tm := NewToolManager("test-app")
	var s Service = tm
	if s == nil {
		t.Fatal("ToolManager does not implement Service")
	}
}

// mockService is a minimal Service implementation used by agent-core tests.
type mockService struct {
	Tools   []ToolInfo
	Exec    func(ctx context.Context, name string, params map[string]any) (Result, error)
}

func (m *mockService) List() []ToolInfo { return m.Tools }

func (m *mockService) Execute(ctx context.Context, name string, params map[string]any) (Result, error) {
	if m.Exec != nil {
		return m.Exec(ctx, name, params)
	}
	return Result{Output: "mock-result"}, nil
}

// TestServiceExecuteNotConnected verifies Execute degrades gracefully when the
// daemon is not connected (service contract: returns error, no panic).
func TestServiceExecuteNotConnected(t *testing.T) {
	tm := NewToolManager("test-app") // never Start()ed → not connected
	var s Service = tm
	_, err := s.Execute(context.Background(), "calc", map[string]any{})
	if err == nil {
		t.Fatal("Execute() on unconnected manager = nil error, want error")
	}
}
