package contextmanager

import (
	"context"
	"strings"
	"testing"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/memory"
)

func TestDefaultContextManagerSessionLifecycle(t *testing.T) {
	cm := NewDefaultContextManager()

	sid := cm.NewSession()
	if sid == "" {
		t.Fatal("NewSession() = empty id")
	}
	if len(cm.ListSessions()) != 1 {
		t.Errorf("ListSessions() = %v, want 1 session", cm.ListSessions())
	}

	if err := cm.AddMessage(sid, ContextMessage{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("AddMessage() = %v", err)
	}
	msgs := cm.GetMessages(sid)
	if len(msgs) != 1 || msgs[0].Content != "hello" {
		t.Errorf("GetMessages() = %v, want [hello]", msgs)
	}

	if err := cm.Clear(sid); err != nil {
		t.Fatalf("Clear() = %v", err)
	}
	if len(cm.GetMessages(sid)) != 0 {
		t.Error("messages not cleared")
	}
}

func TestDefaultContextManagerUniqueSessions(t *testing.T) {
	cm := NewDefaultContextManager()
	s1 := cm.NewSession()
	s2 := cm.NewSession()
	if s1 == s2 {
		t.Errorf("sessions collide: %q == %q", s1, s2)
	}
}

// memoryAware registers a real DefaultMemory in a registry so BuildInput can
// resolve it through the component graph.
func newCMWithMemory(t *testing.T) *ContextManagerComponent {
	t.Helper()

	mem := memory.NewComponent("memory-default", memory.NewDefaultMemory())
	cm := NewComponent("contextmanager-default", NewDefaultContextManager())

	reg := component.NewRegistry()
	if err := reg.Register(mem); err != nil {
		t.Fatalf("Register memory = %v", err)
	}
	if err := reg.Register(cm); err != nil {
		t.Fatalf("Register cm = %v", err)
	}
	reg.SetDefault(component.ComponentMemory, mem.GetName())
	reg.SetDefault(component.ComponentContextManager, cm.GetName())

	if err := reg.InitializeAll(context.Background()); err != nil {
		t.Fatalf("InitializeAll() = %v", err)
	}
	return cm
}

func TestBuildInputAssembly(t *testing.T) {
	cm := newCMWithMemory(t)

	sid := cm.NewSession()
	if err := cm.AddMessage(sid, ContextMessage{Role: "user", Content: "previous turn"}); err != nil {
		t.Fatalf("AddMessage() = %v", err)
	}
	if err := cm.AddMessage(sid, ContextMessage{Role: "assistant", Content: "previous reply"}); err != nil {
		t.Fatalf("AddMessage() = %v", err)
	}

	input := cm.BuildInput(sid, []ContextMessage{
		{Role: "user", Content: "current question"},
	})

	// Expect: [system, ...history, user]
	if len(input) != 4 {
		t.Fatalf("BuildInput len = %d, want 4 (system + 2 history + user), got %+v", len(input), input)
	}
	if input[0].Role != "system" {
		t.Errorf("input[0].Role = %q, want system", input[0].Role)
	}
	if input[1].Content != "previous turn" || input[2].Content != "previous reply" {
		t.Errorf("history not assembled: %+v", input[1:3])
	}
	if input[3].Content != "current question" {
		t.Errorf("user message not appended: %+v", input[3])
	}
}

func TestBuildInputMemoryInjection(t *testing.T) {
	cm := newCMWithMemory(t)

	// Seed memory with a relevant item.
	memComp := cm.Registry().GetDefault(component.ComponentMemory).(*memory.MemoryComponent)
	if err := memComp.Add(context.Background(), memory.MemoryItem{
		ID:      "m1",
		Content: "user prefers concise answers",
		Extra:   map[string]any{"kind": "preference"},
	}); err != nil {
		t.Fatalf("memory.Add() = %v", err)
	}

	sid := cm.NewSession()
	input := cm.BuildInput(sid, []ContextMessage{
		{Role: "user", Content: "how should you answer"},
	})

	sys := input[0].Content
	if !strings.Contains(sys, "Relevant memory") || !strings.Contains(sys, "prefers concise") {
		t.Errorf("system prompt missing memory injection:\n%s", sys)
	}
}

func TestProcessContextManagerBuildInputDegrades(t *testing.T) {
	// Remote unreachable (no daemon on target): BuildInput must not panic and
	// must return the user messages at least (system prompt fails → empty).
	pc := NewProcessContextManager(&ProcessContextManagerConfig{
		Name:   "cm",
		Pool:   grpctransport.NewPool(),
		Target: "127.0.0.1:1", // nothing listening
	})
	input := pc.BuildInput("sid", []ContextMessage{{Role: "user", Content: "hi"}})
	if len(input) == 0 || input[len(input)-1].Content != "hi" {
		t.Errorf("BuildInput degrade = %+v, want last user message present", input)
	}
}

func TestContextManagerComponentDependenciesMemory(t *testing.T) {
	cm := NewComponent("cm", NewDefaultContextManager())
	deps := cm.Dependencies()
	spec, ok := deps["Memory"]
	if !ok {
		t.Fatal("Dependencies() missing Memory")
	}
	if spec.Type != component.ComponentMemory {
		t.Errorf("Memory dep type = %v, want ComponentMemory", spec.Type)
	}
}
