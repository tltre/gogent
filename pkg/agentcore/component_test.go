package agentcore

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/tool"
)

// stubAgent is a minimal IAgentCore for runtime tests.
type stubAgent struct{}

func (s *stubAgent) Run(_ context.Context, _ Input) (Output, error) { return Output{}, nil }
func (s *stubAgent) Stream(_ context.Context, _ Input) (<-chan Event, error) {
	ch := make(chan Event)
	close(ch)
	return ch, nil
}
func (s *stubAgent) SetAgentRuntime(_ *AgentRuntime) {}

// serviceRecording implements tool.Service with a non-nil marker so the test
// can verify the exact injected instance.
type serviceRecording struct {
	tool.Service
	marker string
}

func TestAgentRuntimeToolService(t *testing.T) {
	rt := NewComponent("agent-main", &stubAgent{})

	// Before injection → nil.
	if rt.ToolService() != nil {
		t.Fatal("ToolService() before injection = non-nil, want nil")
	}

	rec := &serviceRecording{marker: "injected"}
	rt.SetToolService(rec)
	if got := rt.ToolService(); got != rec {
		t.Fatalf("ToolService() = %v, want injected instance %v", got, rec)
	}
}

func TestAgentRuntimeSetToolServiceViaRegistry(t *testing.T) {
	// AgentRuntime receives the tool service through the full Builder-less
	// assembly: Initialize resolves registry; SetToolService injects the
	// service independently (Builder does this after registry.InitializeAll).
	reg := component.NewRegistry()
	rt := NewComponent("agent-main", NewReactAgent())
	if err := reg.Register(rt); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	reg.SetDefault(component.ComponentAgentCore, rt.GetName())

	// Initialize wires the registry into the runtime (BasicComponent).
	if err := rt.Initialize(context.Background(), reg); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	tm := tool.NewToolManager("test-app")
	rt.SetToolService(tm)
	if rt.ToolService() == nil {
		t.Fatal("ToolService() = nil after SetToolService(tm)")
	}
}
