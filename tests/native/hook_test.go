package native_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/hook"
)

type mockHook struct {
	name     string
	events   []hook.EventType
	onEvent  func(ctx context.Context, event hook.Event) (context.Context, error)
}

func (h *mockHook) OnEvent(ctx context.Context, event hook.Event) (context.Context, error) {
	if h.onEvent != nil {
		return h.onEvent(ctx, event)
	}
	return ctx, nil
}

func (h *mockHook) Events() []hook.EventType { return h.events }

func TestHookManager(t *testing.T) {
	var triggered bool
	h := &mockHook{
		name:   "test-hook",
		events: []hook.EventType{hook.EventBeforeRun, hook.EventError},
		onEvent: func(ctx context.Context, event hook.Event) (context.Context, error) {
			triggered = true
			return ctx, nil
		},
	}

	hm := hook.NewComponent("hooks-main")
	ctx := context.Background()

	if got := hm.GetName(); got != "hooks-main" {
		t.Errorf("GetName() = %q, want %q", got, "hooks-main")
	}
	if got := hm.GetType(); got != component.ComponentHook {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentHook)
	}

	if err := hm.Register(h); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	ev := hook.Event{Type: hook.EventBeforeRun, Name: "test"}
	if _, err := hm.Trigger(ctx, ev); err != nil {
		t.Fatalf("Trigger() = %v", err)
	}
	if !triggered {
		t.Error("hook was not triggered")
	}

	triggered = false
	ev2 := hook.Event{Type: hook.EventAfterRun, Name: "test"}
	if _, err := hm.Trigger(ctx, ev2); err != nil {
		t.Fatalf("Trigger(AfterRun) = %v", err)
	}
	if triggered {
		t.Error("hook should not be triggered for unregistered event type")
	}
}
