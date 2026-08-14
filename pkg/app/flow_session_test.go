package app

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
)

// ---------------------------------------------------------------------------
// v0.15.4: cross-turn conversation history through the full Builder→App→
// ReactAgent chain (default ContextManager).
// ---------------------------------------------------------------------------

func TestFlowMultiTurnHistory(t *testing.T) {
	registerFlowMocks(t)

	app, runtime := buildFlowApp(t, `
name: flow-session
components:
  - name: "provider-main"
    type: "provider"
    driver: "native"
    config:
      exclude: ["openai", "deepseek"]
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)
	_ = app

	cm := runtime.Reg().GetDefault(component.ComponentContextManager).(*contextmanager.ContextManagerComponent)
	sid := cm.NewSession()

	// Turn 1.
	if _, err := runtime.Run(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "first question"}},
		SessionID:    sid,
	}); err != nil {
		t.Fatalf("Run(turn1) = %v", err)
	}

	// Turn 2 with the same session: the provider must see turn 1's history.
	if _, err := runtime.Run(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "second question"}},
		SessionID:    sid,
	}); err != nil {
		t.Fatalf("Run(turn2) = %v", err)
	}

	// History persisted after two turns: user1 + assistant1 + user2 + assistant2.
	history := cm.GetMessages(sid)
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4 (user1+assistant1+user2+assistant2), got %+v", len(history), history)
	}
	if history[0].Role != "user" || history[0].Content != "first question" {
		t.Errorf("history[0] = %+v, want user first question", history[0])
	}
	if history[1].Role != "assistant" {
		t.Errorf("history[1] = %+v, want assistant reply", history[1])
	}
	if history[2].Role != "user" || history[2].Content != "second question" {
		t.Errorf("history[2] = %+v, want user second question", history[2])
	}
	if history[3].Role != "assistant" {
		t.Errorf("history[3] = %+v, want assistant reply", history[3])
	}
}

func TestFlowNoSessionNoPersistence(t *testing.T) {
	registerFlowMocks(t)

	app, runtime := buildFlowApp(t, `
name: flow-nosession
components:
  - name: "provider-main"
    type: "provider"
    driver: "native"
    config:
      exclude: ["openai", "deepseek"]
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)
	_ = app

	// No SessionID → stateless: nothing persisted.
	if _, err := runtime.Run(context.Background(), agentcore.Input{
		Messages: []agentcore.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}

	cm := runtime.Reg().GetDefault(component.ComponentContextManager).(*contextmanager.ContextManagerComponent)
	if sessions := cm.ListSessions(); len(sessions) != 0 {
		t.Errorf("sessions created without SessionID: %v", sessions)
	}
}
