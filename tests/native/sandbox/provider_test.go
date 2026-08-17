package sandbox_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/internal/daemon/sandbox"
)

func TestProviderRegistry_RegisterAndGet(t *testing.T) {
	r := sandbox.NewProviderRegistry()

	p := &mockProvider{name: "test-e2b", typ: sandbox.SandboxE2B}
	if err := r.Register(p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := r.Get("test-e2b")
	if !ok {
		t.Fatal("Get returned not found")
	}
	if got.Name() != "test-e2b" {
		t.Errorf("expected name test-e2b, got %s", got.Name())
	}
}

func TestProviderRegistry_RegisterDuplicate(t *testing.T) {
	r := sandbox.NewProviderRegistry()
	p1 := &mockProvider{name: "dup", typ: sandbox.SandboxE2B}
	p2 := &mockProvider{name: "dup", typ: sandbox.SandboxBuiltin}

	if err := r.Register(p1); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	if err := r.Register(p2); err == nil {
		t.Fatal("expected error on duplicate register")
	}
}

func TestProviderRegistry_GetByType(t *testing.T) {
	r := sandbox.NewProviderRegistry()
	r.Register(&mockProvider{name: "e2b-a", typ: sandbox.SandboxE2B})
	r.Register(&mockProvider{name: "e2b-b", typ: sandbox.SandboxE2B})
	r.Register(&mockProvider{name: "builtin-a", typ: sandbox.SandboxBuiltin})

	e2bs := r.GetByType(sandbox.SandboxE2B)
	if len(e2bs) != 2 {
		t.Errorf("expected 2 E2B providers, got %d", len(e2bs))
	}

	builtins := r.GetByType(sandbox.SandboxBuiltin)
	if len(builtins) != 1 {
		t.Errorf("expected 1 builtin provider, got %d", len(builtins))
	}

	agents := r.GetByType(sandbox.SandboxAgent)
	if len(agents) != 0 {
		t.Errorf("expected 0 agent providers, got %d", len(agents))
	}
}

func TestProviderRegistry_Remove(t *testing.T) {
	r := sandbox.NewProviderRegistry()
	r.Register(&mockProvider{name: "e2b-a", typ: sandbox.SandboxE2B})
	r.Register(&mockProvider{name: "e2b-b", typ: sandbox.SandboxE2B})

	r.Remove("e2b-a")

	if _, ok := r.Get("e2b-a"); ok {
		t.Error("expected e2b-a to be removed")
	}
	if _, ok := r.Get("e2b-b"); !ok {
		t.Error("expected e2b-b to still exist")
	}

	e2bs := r.GetByType(sandbox.SandboxE2B)
	if len(e2bs) != 1 {
		t.Errorf("expected 1 E2B provider after removal, got %d", len(e2bs))
	}
}

func TestProviderRegistry_List(t *testing.T) {
	r := sandbox.NewProviderRegistry()
	r.Register(&mockProvider{name: "a", typ: sandbox.SandboxE2B})
	r.Register(&mockProvider{name: "b", typ: sandbox.SandboxBuiltin})

	all := r.List()
	if len(all) != 2 {
		t.Errorf("expected 2 providers, got %d", len(all))
	}
}

// ---------------------------------------------------------------------------
// mocks
// ---------------------------------------------------------------------------

type mockProvider struct {
	name string
	typ  sandbox.SandboxType
}

func (m *mockProvider) Name() string                                 { return m.name }
func (m *mockProvider) Type() sandbox.SandboxType                    { return m.typ }
func (m *mockProvider) Create(_ context.Context, _ *sandbox.SandboxProfile, _ *sandbox.SandboxConfig) (sandbox.ISandbox, error) {
	return &mockSandbox{name: m.name}, nil
}

type mockSandbox struct {
	name   string
	closed bool
}

func (s *mockSandbox) Execute(_ context.Context, _ sandbox.ExecRequest) (sandbox.ExecResult, error) {
	return sandbox.ExecResult{Stdout: "mock-output", ExitCode: 0}, nil
}

func (s *mockSandbox) Close(_ context.Context) error {
	s.closed = true
	return nil
}
