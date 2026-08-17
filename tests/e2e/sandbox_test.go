// Package e2e_test contains end-to-end tests for the sandbox system.
//
// These tests read the E2B API key from the E2B_API_KEY environment variable
// (via a temporary credentials.yaml file, matching the production
// configuration flow). When the variable is unset, the tests are skipped.
//
// Run: E2B_API_KEY=... go test ./tests/e2e/ -v -run TestE2E -timeout 180s
package e2e_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/credentials"
	"github.com/tltre/gogent/internal/daemon/sandbox"
	"github.com/tltre/gogent/internal/daemon/tool"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ---------------------------------------------------------------------------
// Test configuration
// ---------------------------------------------------------------------------

// testE2BAPIKey returns the E2B API key from the environment. Tests that
// require a real E2B backend skip when it is unset.
func testE2BAPIKey() string {
	return os.Getenv("E2B_API_KEY")
}

// e2bSandboxYAML is the daemon-side sandbox config used in E2E tests.
const e2bSandboxYAML = `
sandbox-providers:
  test-e2b:
    type: e2b
    apiKey: "${E2B_API_KEY}"

sandbox-profiles:
  default-shell:
    provider: test-e2b
    template: "code-interpreter-v1"
    network: false

  workspace:
    provider: test-e2b
    template: "code-interpreter-v1"
    network: true
    lifecycle:
      mode: persistent

defaults:
  builtin:
    profile: default-shell
  process:
    profile: default-shell
`

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setupE2ETest creates a temporary credentials.yaml, loads it into a Resolver,
// builds a full Daemon-side stack (SandboxManager + gRPC server), and returns
// the server address and cleanup function.
func setupE2ETest(t *testing.T) (addr string, cleanup func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gogent-e2e-*")
	if err != nil {
		t.Fatalf("mk temp: %v", err)
	}

	apiKey := testE2BAPIKey()
	if apiKey == "" {
		os.RemoveAll(tmpDir)
		t.Skip("E2B_API_KEY not set; skipping sandbox e2e test")
	}

	credPath := filepath.Join(tmpDir, "credentials.yaml")
	if err := os.WriteFile(credPath, []byte("E2B_API_KEY: "+apiKey+"\n"), 0600); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("write credentials: %v", err)
	}

	creds, err := credentials.LoadCredentials(credPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("load credentials: %v", err)
	}

	sf, err := sandbox.ParseSandboxFile([]byte(e2bSandboxYAML))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("parse sandbox.yaml: %v", err)
	}

	mgr := sandbox.NewSandboxManager()
	defaults, err := sandbox.ApplySandboxFile(mgr, sf, credentials.NewResolver(creds))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("apply sandbox.yaml: %v", err)
	}

	registry := tool.NewToolRegistry()
	registry.LoadDefault()
	handler := tool.NewHandler(registry, tool.NewManifestStore(), tool.NewServerStore(), mgr, defaults)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("listen: %v", err)
	}
	s := grpc.NewServer()
	gogentv1.RegisterToolServiceServer(s, handler)
	go s.Serve(lis)

	cleanup = func() {
		s.GracefulStop()
		os.RemoveAll(tmpDir)
	}
	return lis.Addr().String(), cleanup
}

// registerApp sends RegisterManifest with sandbox config in gRPC metadata.
func registerApp(ctx context.Context, t *testing.T, client gogentv1.ToolServiceClient, appName string, tools []*gogentv1.ManifestEntry, sbConfigs map[string]string, defaultSb string, toolMap map[string]string) {
	t.Helper()
	md := metadata.Pairs("app-name", appName)

	if len(sbConfigs) > 0 {
		parts := make([]string, 0, len(sbConfigs))
		for k, v := range sbConfigs {
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
		md.Append("sandbox-configs", "{"+strings.Join(parts, ",")+"}")
	}
	if len(toolMap) > 0 {
		parts := make([]string, 0, len(toolMap))
		for k, v := range toolMap {
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
		md.Append("sandbox-tool-map", "{"+strings.Join(parts, ",")+"}")
	}
	if defaultSb != "" {
		md.Append("sandbox-default", defaultSb)
	}

	resp, err := client.RegisterManifest(metadata.NewOutgoingContext(ctx, md), &gogentv1.ManifestRequest{AppName: appName, Tools: tools})
	if err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}
	t.Logf("manifest accepted=%v, tools=%d", resp.Accepted, len(resp.Tools))
}

// execTool executes a tool via ExecuteTool stream and returns the result.
func execTool(ctx context.Context, t *testing.T, client gogentv1.ToolServiceClient, appName, toolName string, params map[string]any) *gogentv1.ToolResult {
	t.Helper()
	md := metadata.Pairs("app-name", appName)
	stream, err := client.ExecuteTool(metadata.NewOutgoingContext(ctx, md))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer stream.CloseSend()

	pbParams, _ := structpb.NewStruct(params)
	stream.Send(&gogentv1.ToolControl{
		Msg: &gogentv1.ToolControl_Request{
			Request: &gogentv1.ToolExecuteRequest{ToolName: toolName, Params: pbParams},
		},
	})

	for {
		evt, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if auth := evt.GetAuth(); auth != nil && !auth.Passed {
			return &gogentv1.ToolResult{IsError: true, ErrorMsg: "auth: " + auth.Reason}
		}
		if hook := evt.GetHook(); hook != nil {
			stream.Send(&gogentv1.ToolControl{
				Msg: &gogentv1.ToolControl_Verdict{
					Verdict: &gogentv1.HookVerdict{HookId: hook.GetHookId(), Approved: true},
				},
			})
		}
		if result := evt.GetResult(); result != nil {
			return result
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestE2E_SimpleShell_WithDaemonDefaults(t *testing.T) {
	// TC-E2E-01: Only tools declared → daemon defaults apply
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr, cleanup := setupE2ETest(t)
	defer cleanup()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	client := gogentv1.NewToolServiceClient(conn)

	registerApp(ctx, t, client, "e2e-s",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "", nil)

	t.Log("=== echo hello ===")
	r := execTool(ctx, t, client, "e2e-s", "shell", map[string]any{"cmd": "echo hello"})
	if r.IsError {
		t.Fatalf("shell failed: %s", r.ErrorMsg)
	}
	t.Logf("output: %s", r.Output.GetStringValue())
}

func TestE2E_MultiSandboxWithToolMapping(t *testing.T) {
	// TC-E2E-02: Multiple sandboxes + per-tool route
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	addr, cleanup := setupE2ETest(t)
	defer cleanup()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	client := gogentv1.NewToolServiceClient(conn)

	registerApp(ctx, t, client, "e2e-m",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2, Sandbox: "ws"}},
		map[string]string{"ws": "workspace", "default-shell": "default-shell"},
		"default-shell",
		map[string]string{"shell": "ws"})

	t.Log("=== shell (workspace) ===")
	r := execTool(ctx, t, client, "e2e-m", "shell", map[string]any{"cmd": "echo multi && pwd"})
	if r.IsError {
		t.Fatalf("shell failed: %s", r.ErrorMsg)
	}
	t.Logf("output: %s", r.Output.GetStringValue())
}

func TestE2E_EphemeralShell(t *testing.T) {
	// TC-E2E-03: Ephemeral execution via daemon defaults
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	addr, cleanup := setupE2ETest(t)
	defer cleanup()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("conn: %v", err)
	}
	defer conn.Close()
	client := gogentv1.NewToolServiceClient(conn)

	registerApp(ctx, t, client, "e2e-e",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "", nil)

	t.Log("=== python3 -c 'print(1+1)' ===")
	r := execTool(ctx, t, client, "e2e-e", "shell", map[string]any{"cmd": "python3 -c 'print(1+1)'"})
	if r.IsError {
		t.Fatalf("shell failed: %s", r.ErrorMsg)
	}
	t.Logf("output: %s", r.Output.GetStringValue())
}

func TestE2E_InvalidAPIKey(t *testing.T) {
	// TC-E2E-05: Bad key → meaningful error
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tmpDir, _ := os.MkdirTemp("", "gogent-e2e-bad-*")
	defer os.RemoveAll(tmpDir)
	os.WriteFile(filepath.Join(tmpDir, "creds.yaml"), []byte("E2B_API_KEY: bad-key\n"), 0600)
	creds, _ := credentials.LoadCredentials(filepath.Join(tmpDir, "creds.yaml"))

	sf, _ := sandbox.ParseSandboxFile([]byte(e2bSandboxYAML))
	mgr := sandbox.NewSandboxManager()
	_, err := sandbox.ApplySandboxFile(mgr, sf, credentials.NewResolver(creds))
	if err != nil {
		t.Logf("provider creation failed (expected): %v", err)
		return
	}

	registry := tool.NewToolRegistry()
	registry.LoadDefault()
	handler := tool.NewHandler(registry, tool.NewManifestStore(), tool.NewServerStore(), mgr,
		&sandbox.DefaultMappings{Builtin: sandbox.ProfileRef{Profile: "default-shell"}, Process: sandbox.ProfileRef{Profile: "default-shell"}})

	lis, _ := net.Listen("tcp", "127.0.0.1:0")
	s := grpc.NewServer()
	gogentv1.RegisterToolServiceServer(s, handler)
	go s.Serve(lis)
	defer s.GracefulStop()

	conn, _ := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	defer conn.Close()
	client := gogentv1.NewToolServiceClient(conn)

	registerApp(ctx, t, client, "e2e-bad", []*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}}, nil, "", nil)
	r := execTool(ctx, t, client, "e2e-bad", "shell", map[string]any{"cmd": "echo x"})
	if !r.IsError {
		t.Fatal("expected error")
	}
	t.Logf("expected error: %s", r.ErrorMsg)
}

func TestE2E_MissingCredentials(t *testing.T) {
	// TC-E2E-10: No credentials at all
	sf, _ := sandbox.ParseSandboxFile([]byte(e2bSandboxYAML))
	mgr := sandbox.NewSandboxManager()
	_, err := sandbox.ApplySandboxFile(mgr, sf, credentials.NewResolver(nil))
	if err != nil {
		t.Logf("expected error (no creds): %v", err)
		return
	}
	t.Log("provider registered (will fail on use)")
}
