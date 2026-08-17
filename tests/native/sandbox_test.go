package native_test

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/sandbox"
)

type mockSandbox struct {
	limits sandbox.ResourceLimits
	execFn func(ctx context.Context, sandboxID string, req sandbox.ExecRequest) (sandbox.ExecResult, error)
}

func (s *mockSandbox) Create(_ context.Context) (string, error) { return "sb-1", nil }
func (s *mockSandbox) Destroy(_ context.Context, id string) error { return nil }
func (s *mockSandbox) Execute(ctx context.Context, sandboxID string, req sandbox.ExecRequest) (sandbox.ExecResult, error) {
	if s.execFn != nil {
		return s.execFn(ctx, sandboxID, req)
	}
	return sandbox.ExecResult{Stdout: "output", ExitCode: 0}, nil
}
func (s *mockSandbox) SetLimits(limits sandbox.ResourceLimits) { s.limits = limits }
func (s *mockSandbox) GetLimits() sandbox.ResourceLimits       { return s.limits }

func TestSandboxComponent(t *testing.T) {
	s := &mockSandbox{}
	limits := sandbox.ResourceLimits{MaxMemoryMB: 256, NetworkAccess: false}
	comp := sandbox.NewComponent("test-sandbox", s, limits)
	ctx := context.Background()

	if got := comp.GetName(); got != "test-sandbox" {
		t.Errorf("GetName() = %q, want %q", got, "test-sandbox")
	}
	if got := comp.GetType(); got != component.ComponentSandbox {
		t.Errorf("GetType() = %q, want %q", got, component.ComponentSandbox)
	}

	if err := comp.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	id, err := comp.Create(ctx)
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if id != "sb-1" {
		t.Errorf("Create() = %q, want sb-1", id)
	}

	req := sandbox.ExecRequest{Code: "1+1", Language: "python"}
	result, err := comp.Execute(ctx, id, req)
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if result.Stdout != "output" {
		t.Errorf("Execute().Stdout = %q, want output", result.Stdout)
	}

	if err := comp.Destroy(ctx, id); err != nil {
		t.Fatalf("Destroy() = %v", err)
	}
}

func TestSandboxSimpleExecute(t *testing.T) {
	limits := sandbox.ResourceLimits{
		MaxMemoryMB:  512,
		NetworkAccess: false,
	}
	s := sandbox.NewDefaultSandbox(limits)
	comp := sandbox.NewComponent("simple-sb", s, limits)
	ctx := context.Background()

	if err := comp.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	id, err := comp.Create(ctx)
	if err != nil {
		t.Fatalf("Create() = %v", err)
	}
	if id == "" {
		t.Fatal("Create() returned empty id")
	}

	req := sandbox.ExecRequest{Code: "echo hello", Language: "sh"}
	result, err := comp.Execute(ctx, id, req)
	if err != nil {
		t.Fatalf("Execute() = %v", err)
	}
	if result.Stdout != "hello\n" && result.Stdout != "hello\r\n" {
		t.Errorf("Execute().Stdout = %q, want hello", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Errorf("Execute().ExitCode = %d, want 0", result.ExitCode)
	}

	if err := comp.Destroy(ctx, id); err != nil {
		t.Errorf("Destroy() = %v", err)
	}
}

func TestSandboxAllowedCommands(t *testing.T) {
	limits := sandbox.ResourceLimits{
		MaxMemoryMB:     512,
		NetworkAccess:   false,
		AllowedCommands: []string{"echo", "ls"},
	}
	s := sandbox.NewDefaultSandbox(limits)
	comp := sandbox.NewComponent("restricted-sb", s, limits)
	ctx := context.Background()

	if err := comp.Initialize(ctx, component.NewRegistry()); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	id, _ := comp.Create(ctx)

	result, err := comp.Execute(ctx, id, sandbox.ExecRequest{Code: "echo ok", Language: "sh"})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("allowed command failed: exit=%d err=%v", result.ExitCode, err)
	}

	result, _ = comp.Execute(ctx, id, sandbox.ExecRequest{Code: "cat /etc/passwd", Language: "sh"})
	if result.ExitCode == 0 && result.Error == nil {
		t.Error("blocked command 'cat' should have been rejected")
	}

	comp.Destroy(ctx, id)
}
