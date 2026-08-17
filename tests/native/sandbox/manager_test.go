package sandbox_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/internal/daemon/sandbox"
)

func TestManager_RegisterAndGetProfile(t *testing.T) {
	mgr := sandbox.NewSandboxManager()

	p := &sandbox.SandboxProfile{Name: "test-profile", Provider: "test-provider", Template: "base"}
	if err := mgr.RegisterProfile(p); err != nil {
		t.Fatalf("RegisterProfile failed: %v", err)
	}

	got, ok := mgr.GetProfile("test-profile")
	if !ok {
		t.Fatal("GetProfile returned not found")
	}
	if got.Template != "base" {
		t.Errorf("expected template 'base', got %s", got.Template)
	}
}

func TestManager_RegisterDuplicateProfile(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	p1 := &sandbox.SandboxProfile{Name: "dup", Provider: "p1"}
	p2 := &sandbox.SandboxProfile{Name: "dup", Provider: "p2"}

	if err := mgr.RegisterProfile(p1); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	if err := mgr.RegisterProfile(p2); err == nil {
		t.Fatal("expected error on duplicate profile")
	}
}

func TestManager_ListProfiles(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	mgr.RegisterProfile(&sandbox.SandboxProfile{Name: "a"})
	mgr.RegisterProfile(&sandbox.SandboxProfile{Name: "b"})

	names := mgr.ListProfiles()
	if len(names) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(names))
	}
}

func TestManager_GetOrCreateForApp(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	ctx := context.Background()

	mgr.RegisterProvider(&mockProvider{name: "p", typ: sandbox.SandboxE2B})
	mgr.RegisterProfile(&sandbox.SandboxProfile{Name: "prof", Provider: "p", Template: "base"})

	cfg := &sandbox.SandboxConfig{Name: "ws", ProfileName: "prof"}

	sb1, err := mgr.GetOrCreateForApp(ctx, "app-1", cfg)
	if err != nil {
		t.Fatalf("GetOrCreateForApp failed: %v", err)
	}
	if sb1 == nil {
		t.Fatal("expected non-nil sandbox")
	}

	sb2, err := mgr.GetOrCreateForApp(ctx, "app-1", cfg)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if sb1 != sb2 {
		t.Error("expected same instance on second call")
	}
}

func TestManager_GetOrCreateForApp_ProfileNotFound(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	ctx := context.Background()

	_, err := mgr.GetOrCreateForApp(ctx, "app-1", &sandbox.SandboxConfig{
		Name: "ws", ProfileName: "nonexistent",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent profile")
	}
}

func TestManager_GetOrCreateForApp_ProviderNotFound(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	ctx := context.Background()
	mgr.RegisterProfile(&sandbox.SandboxProfile{Name: "orphan", Provider: "missing", Template: "base"})

	_, err := mgr.GetOrCreateForApp(ctx, "app-1", &sandbox.SandboxConfig{
		Name: "ws", ProfileName: "orphan",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent provider")
	}
}

func TestManager_DestroyAppSandboxes(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	ctx := context.Background()

	mgr.RegisterProvider(&mockProvider{name: "p", typ: sandbox.SandboxE2B})
	mgr.RegisterProfile(&sandbox.SandboxProfile{Name: "prof", Provider: "p", Template: "base"})

	mgr.GetOrCreateForApp(ctx, "app-a", &sandbox.SandboxConfig{Name: "ws", ProfileName: "prof"})
	mgr.GetOrCreateForApp(ctx, "app-b", &sandbox.SandboxConfig{Name: "ws", ProfileName: "prof"})

	if err := mgr.DestroyAppSandboxes(ctx, "app-a"); err != nil {
		t.Fatalf("DestroyAppSandboxes failed: %v", err)
	}

	if count := mgr.GetAppSandboxCount("app-a"); count != 0 {
		t.Errorf("expected 0 for app-a, got %d", count)
	}
	if count := mgr.GetAppSandboxCount("app-b"); count != 1 {
		t.Errorf("expected 1 for app-b, got %d", count)
	}
}

func TestManager_ResolveFallback(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	def := &sandbox.DefaultMappings{
		Builtin: sandbox.ProfileRef{Profile: "builtin-def"},
		Process: sandbox.ProfileRef{Profile: "process-def"},
	}

	tests := []struct {
		name     string
		appDef   string
		driver   string
		expected string
	}{
		{"app default wins", "my-app", "builtin", "my-app"},
		{"builtin fallback", "", "builtin", "builtin-def"},
		{"process fallback", "", "process", "process-def"},
		{"http fallback", "", "http", "process-def"},
		{"unknown driver", "", "unknown", ""},
		{"all empty", "", "builtin", "builtin-def"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mgr.ResolveFallbackProfile(tt.appDef, tt.driver, def)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestManager_RefreshAll(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	ctx := context.Background()

	mgr.RegisterProvider(&mockProvider{name: "p", typ: sandbox.SandboxE2B})
	mgr.RegisterProfile(&sandbox.SandboxProfile{Name: "prof", Provider: "p", Template: "base"})
	mgr.GetOrCreateForApp(ctx, "app-1", &sandbox.SandboxConfig{Name: "ws", ProfileName: "prof"})

	mgr.RefreshAll(ctx) // should not panic
}
