package native_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	"github.com/tltre/gogent/pkg/hook"
	"github.com/tltre/gogent/pkg/tool"
)

func TestToolHandleHookInvocation(t *testing.T) {
	tm := tool.NewToolManager("test-app")

	// No hook manager — auto-approve
	inv := &gogentv1.HookInvocation{HookId: "test-1", Stage: "pre_execute"}
	verdict := tm.HandleHookInvocation(context.Background(), inv)
	if !verdict.Approved {
		t.Error("expected auto-approve when no hook manager")
	}

	// With hook manager, hook approves
	hm := hook.NewComponent("hooks-main")
	hm.Register(&mockHook{name: "ok", events: []hook.EventType{hook.EventBeforeTool}})
	tm.SetHookManager(hm)

	verdict = tm.HandleHookInvocation(context.Background(), inv)
	if !verdict.Approved {
		t.Error("expected approved when hook succeeds")
	}

	// Hook returns error — should reject
	hm2 := hook.NewComponent("hooks-reject")
	hm2.Register(&mockHook{
		name:   "blocker",
		events: []hook.EventType{hook.EventBeforeTool},
		onEvent: func(_ context.Context, _ hook.Event) (context.Context, error) {
			return nil, errors.New("not allowed")
		},
	})
	tm.SetHookManager(hm2)

	verdict = tm.HandleHookInvocation(context.Background(), inv)
	if verdict.Approved {
		t.Error("expected rejected when hook fails")
	}
	if verdict.Reason != "not allowed" {
		t.Errorf("expected reason 'not allowed', got %q", verdict.Reason)
	}
}

func TestToolHandlePostHookOutput(t *testing.T) {
	tm := tool.NewToolManager("test-app")

	inv := &gogentv1.HookInvocation{HookId: "post-1", Stage: "post_execute"}

	// No hook manager — verdict should have nil output
	verdict := tm.HandleHookInvocation(context.Background(), inv)
	if verdict.Output != nil {
		t.Error("expected nil output when no hook manager")
	}
}

func TestToolManagerBasics(t *testing.T) {
	tm := tool.NewToolManager("test-app")

	if tm == nil {
		t.Fatal("NewToolManager() returned nil")
	}

	// Initially: no cache, not connected, no client
	if list := tm.List(); list != nil {
		t.Errorf("List() = %v, want nil (no daemon)", list)
	}

	// SetManifest
	entries := []tool.ManifestEntry{
		{Name: "calculator", Level: 0},
		{Name: "shell", Level: 2},
	}
	tm.SetManifest("test-app", entries)

	// Execute without daemon — should fail gracefully
	ctx := context.Background()
	_, err := tm.Execute(ctx, "calculator", map[string]any{"expr": "1+1"})
	if err == nil {
		t.Error("Execute() without daemon: expected error, got nil")
	}

	// Stop without start — no panic
	if err := tm.Stop(ctx); err != nil {
		t.Errorf("Stop() = %v, want nil", err)
	}
}

func TestToolManagerStartNoDaemon(t *testing.T) {
	tm := tool.NewToolManager("test-app")
	tm.SetManifest("test-app", []tool.ManifestEntry{
		{Name: "calculator", Level: 0},
	})

	// Start without daemon — should log warn but not error
	ctx := context.Background()
	if err := tm.Start(ctx); err != nil {
		t.Errorf("Start() without daemon: %v, want nil (graceful)", err)
	}

	// After failed Start, List should still return nil
	if list := tm.List(); list != nil {
		t.Errorf("List() after failed Start = %v, want nil", list)
	}
}
