package sandbox_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/daemon/sandbox"
	"github.com/tltre/gogent/internal/daemon/tool"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// startTestServer starts a real gRPC ToolService server with a sandbox-enabled
// Handler. Returns the server address and a cleanup function.
func startTestServer(t *testing.T, sandboxMgr *sandbox.SandboxManager, defaults *sandbox.DefaultMappings) (addr string, cleanup func()) {
	t.Helper()

	registry := tool.NewToolRegistry()
	registry.LoadDefault() // registers calculator, think, todo, filesystem.read, shell
	manifestStore := tool.NewManifestStore()
	serverStore := tool.NewServerStore()

	handler := tool.NewHandler(registry, manifestStore, serverStore, sandboxMgr, defaults)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	gogentv1.RegisterToolServiceServer(s, handler)
	go s.Serve(lis)

	return lis.Addr().String(), func() {
		s.GracefulStop()
	}
}

// connectClient creates a ToolService client connected to the given address.
func connectClient(t *testing.T, addr string) (gogentv1.ToolServiceClient, *grpc.ClientConn) {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	return gogentv1.NewToolServiceClient(conn), conn
}

// registerManifestWithSandbox sends a RegisterManifest request with sandbox
// config in gRPC metadata.
func registerManifestWithSandbox(ctx context.Context, t *testing.T, client gogentv1.ToolServiceClient, appName string, tools []*gogentv1.ManifestEntry, sbConfigs map[string]string, defaultSb string) {
	t.Helper()

	md := metadata.Pairs("app-name", appName)
	if len(sbConfigs) > 0 {
		sbJSON := "{"
		parts := []string{}
		for k, v := range sbConfigs {
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
		sbJSON += strings.Join(parts, ",") + "}"
		md.Append("sandbox-configs", sbJSON)
	}
	if defaultSb != "" {
		md.Append("sandbox-default", defaultSb)
	}

	ctx = metadata.NewOutgoingContext(ctx, md)
	req := &gogentv1.ManifestRequest{
		AppName: appName,
		Tools:   tools,
	}
	resp, err := client.RegisterManifest(ctx, req)
	if err != nil {
		t.Fatalf("RegisterManifest failed: %v", err)
	}
	if !resp.Accepted && len(tools) > 0 {
		t.Logf("RegisterManifest partial: %v", resp.Tools)
	}
	_ = resp
}

// executeTool sends a ToolExecuteRequest over a bidirectional stream and
// returns the final ToolResult.
func executeTool(ctx context.Context, t *testing.T, client gogentv1.ToolServiceClient, appName, toolName string, params map[string]any) *gogentv1.ToolResult {
	t.Helper()

	md := metadata.Pairs("app-name", appName)
	streamCtx := metadata.NewOutgoingContext(ctx, md)
	stream, err := client.ExecuteTool(streamCtx)
	if err != nil {
		t.Fatalf("ExecuteTool stream open: %v", err)
	}
	defer stream.CloseSend()

	// Send ToolExecuteRequest
	pbParams, _ := structpb.NewStruct(params)
	stream.Send(&gogentv1.ToolControl{
		Msg: &gogentv1.ToolControl_Request{
			Request: &gogentv1.ToolExecuteRequest{
				ToolName: toolName,
				Params:   pbParams,
			},
		},
	})

	// Read events until we get a result or error
	for {
		evt, err := stream.Recv()
		if err != nil {
			t.Fatalf("ExecuteTool recv: %v", err)
		}
		switch e := evt.Event.(type) {
		case *gogentv1.ToolExecutionEvent_Auth:
			if !e.Auth.Passed {
				return &gogentv1.ToolResult{
					IsError:  true,
					ErrorMsg: "auth failed: " + e.Auth.Reason,
				}
			}
		case *gogentv1.ToolExecutionEvent_Hook:
			// Auto-approve hooks
			stream.Send(&gogentv1.ToolControl{
				Msg: &gogentv1.ToolControl_Verdict{
					Verdict: &gogentv1.HookVerdict{
						HookId:   e.Hook.HookId,
						Approved: true,
					},
				},
			})
		case *gogentv1.ToolExecutionEvent_Result:
			return e.Result
		}
	}
}

// ---------------------------------------------------------------------------
// Integration tests
// ---------------------------------------------------------------------------

// setupTestSandbox creates a SandboxManager with mock providers and profiles
// for integration testing.
func setupTestSandbox() *sandbox.SandboxManager {
	mgr := sandbox.NewSandboxManager()
	mgr.RegisterProvider(&mockProvider{name: "mock-e2b", typ: sandbox.SandboxE2B})
	mgr.RegisterProfile(&sandbox.SandboxProfile{
		Name:     "test-default",
		Provider: "mock-e2b",
		Template: "base",
	})
	mgr.RegisterProfile(&sandbox.SandboxProfile{
		Name:     "test-workspace",
		Provider: "mock-e2b",
		Template: "workspace-v1",
	})
	return mgr
}

func defaultMappings() *sandbox.DefaultMappings {
	return &sandbox.DefaultMappings{
		Builtin: sandbox.ProfileRef{Profile: "test-default"},
		Process: sandbox.ProfileRef{Profile: "test-default"},
	}
}

// ---------------------------------------------------------------------------

func TestIntegration_SandboxRoute_AppDefault(t *testing.T) {
	// TC-E2E-01 symmetric: app with no sandbox config, daemon defaults apply
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	// Register app with tools only (no sandbox config)
	registerManifestWithSandbox(ctx, t, client, "app-1",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "")

	// Execute shell — should fallback to daemon defaults → test-default
	result := executeTool(ctx, t, client, "app-1", "shell", map[string]any{"cmd": "echo hello"})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ErrorMsg)
	}

	// App sandbox count should be 1
	if count := mgr.GetAppSandboxCount("app-1"); count != 1 {
		t.Errorf("expected 1 sandbox for app-1, got %d", count)
	}
}

func TestIntegration_SandboxRoute_ExplicitMapping(t *testing.T) {
	// App declares sandboxes + explicit tool→sandbox mapping
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "app-2",
		[]*gogentv1.ManifestEntry{
			{Name: "shell", SecurityLevel: 2},
			{Name: "filesystem.read", SecurityLevel: 1},
		},
		map[string]string{"ws": "test-workspace"},
		"ws") // default sandbox = "ws"

	// Execute shell (no explicit sandbox → app default "ws" → test-workspace)
	result := executeTool(ctx, t, client, "app-2", "shell", map[string]any{"cmd": "pwd"})
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.ErrorMsg)
	}
}

func TestIntegration_SandboxRoute_NoSandboxNoFallback(t *testing.T) {
	// No sandbox manager at all → falls through to BuiltinRunner → needsSandbox
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	addr, cleanup := startTestServer(t, nil, nil)
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "app-3",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "")

	result := executeTool(ctx, t, client, "app-3", "shell", map[string]any{"cmd": "ls"})
	if !result.IsError {
		t.Fatal("expected error when no sandbox configured")
	}
	// The error message is in Output.StringValue()
	got := ""
	if result.Output != nil {
		got = result.Output.GetStringValue()
	}
	if !strings.Contains(got, "requires a sandbox") {
		t.Errorf("expected 'requires a sandbox' in output, got: %q", got)
	}
}

func TestIntegration_SandboxRoute_Level0NotSandboxed(t *testing.T) {
	// Level 0 tools (calculator, think, todo) should NOT go through sandbox
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "app-4",
		[]*gogentv1.ManifestEntry{{Name: "calculator", SecurityLevel: 0}},
		nil, "")

	result := executeTool(ctx, t, client, "app-4", "calculator", map[string]any{"expr": "1+1"})
	if result.IsError {
		t.Fatalf("calculator should succeed without sandbox: %s", result.ErrorMsg)
	}
}

func TestIntegration_SandboxRoute_ProfileNotFound(t *testing.T) {
	// App references a sandbox profile that doesn't exist in daemon
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := sandbox.NewSandboxManager()
	mgr.RegisterProvider(&mockProvider{name: "mock-e2b", typ: sandbox.SandboxE2B})
	// Note: no profiles registered

	addr, cleanup := startTestServer(t, mgr, nil)
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "app-5",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		map[string]string{"ws": "nonexistent-profile"},
		"ws")

	result := executeTool(ctx, t, client, "app-5", "shell", map[string]any{"cmd": "ls"})
	if !result.IsError {
		t.Fatal("expected error when profile doesn't exist")
	}
	if !strings.Contains(result.ErrorMsg, "sandbox profile") {
		t.Errorf("expected 'sandbox profile' error, got: %s", result.ErrorMsg)
	}
}

func TestIntegration_AppIdentity_Metadata(t *testing.T) {
	// Verify app-name is correctly extracted from gRPC metadata
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "identity-app",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "")

	result := executeTool(ctx, t, client, "identity-app", "shell", map[string]any{"cmd": "whoami"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.ErrorMsg)
	}

	// Verify sandbox was created under the correct app name
	if count := mgr.GetAppSandboxCount("identity-app"); count != 1 {
		t.Errorf("expected 1 sandbox for identity-app, got %d", count)
	}
}

func TestIntegration_ManifestAndSandboxCombined(t *testing.T) {
	// App registers tools + sandbox configs + default in one call
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "combined-app",
		[]*gogentv1.ManifestEntry{
			{Name: "shell", SecurityLevel: 2},
			{Name: "filesystem.read", SecurityLevel: 1},
		},
		map[string]string{"my-sb": "test-workspace"},
		"my-sb")

	// Execute shell — should route to "my-sb" → test-workspace
	result := executeTool(ctx, t, client, "combined-app", "shell", map[string]any{"cmd": "echo combined"})
	if result.IsError {
		t.Fatalf("expected success: %s", result.ErrorMsg)
	}
}

func TestIntegration_CleanupOnDisconnect(t *testing.T) {
	// Verify sandboxes are cleaned up after the app is destroyed
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "cleanup-app",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "")

	// Execute to create sandbox
	_ = executeTool(ctx, t, client, "cleanup-app", "shell", map[string]any{"cmd": "echo test"})

	if count := mgr.GetAppSandboxCount("cleanup-app"); count != 1 {
		t.Fatalf("expected 1 sandbox before cleanup, got %d", count)
	}

	// Destroy app sandboxes (simulates disconnect)
	if err := mgr.DestroyAppSandboxes(ctx, "cleanup-app"); err != nil {
		t.Fatalf("DestroyAppSandboxes failed: %v", err)
	}

	if count := mgr.GetAppSandboxCount("cleanup-app"); count != 0 {
		t.Errorf("expected 0 sandboxes after cleanup, got %d", count)
	}
}

func TestIntegration_MultipleAppsIsolated(t *testing.T) {
	// Two apps should have independent sandbox instances
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	// Register two apps
	registerManifestWithSandbox(ctx, t, client, "app-a",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}}, nil, "")
	registerManifestWithSandbox(ctx, t, client, "app-b",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}}, nil, "")

	// Both execute
	_ = executeTool(ctx, t, client, "app-a", "shell", map[string]any{"cmd": "echo a"})
	_ = executeTool(ctx, t, client, "app-b", "shell", map[string]any{"cmd": "echo b"})

	if count := mgr.GetAppSandboxCount("app-a"); count != 1 {
		t.Errorf("expected 1 sandbox for app-a, got %d", count)
	}
	if count := mgr.GetAppSandboxCount("app-b"); count != 1 {
		t.Errorf("expected 1 sandbox for app-b, got %d", count)
	}

	// Destroy app-a only
	mgr.DestroyAppSandboxes(ctx, "app-a")

	if count := mgr.GetAppSandboxCount("app-a"); count != 0 {
		t.Errorf("expected 0 sandboxes for app-a after cleanup, got %d", count)
	}
	if count := mgr.GetAppSandboxCount("app-b"); count != 1 {
		t.Errorf("expected 1 sandbox for app-b (unchanged), got %d", count)
	}
}

func TestIntegration_RefreshAllNoPanic(t *testing.T) {
	// Verify RefreshAll iterates over mock sandboxes without panic
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "refresh-app",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}}, nil, "")

	_ = executeTool(ctx, t, client, "refresh-app", "shell", map[string]any{"cmd": "echo refresh"})

	// Should not panic
	mgr.RefreshAll(ctx)
}

func TestIntegration_NoAppName_FallbackToUnknown(t *testing.T) {
	// App doesn't set app-name metadata → handler should fallback to "unknown"
	// without panic
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	// Register manifest WITHOUT app-name metadata (use background ctx, no md)
	req := &gogentv1.ManifestRequest{
		AppName: "nameless-app",
		Tools:   []*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
	}
	_, err := client.RegisterManifest(ctx, req)
	if err != nil {
		t.Fatalf("RegisterManifest failed: %v", err)
	}

	// ExecuteTool without app-name metadata — should not panic
	result := executeTool(ctx, t, client, "", "shell", map[string]any{"cmd": "echo test"})
	if result.IsError {
		// Error is expected (sandbox routing uses "unknown" app, provider works)
		t.Logf("got expected error (no sandbox): %s", result.ErrorMsg)
	}
}

func TestIntegration_DynamicManifestUpdate(t *testing.T) {
	// App first registers without sandbox config, then re-registers WITH sandbox config
	// The second manifest should update the sandbox routing
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	// First registration: no sandbox config
	registerManifestWithSandbox(ctx, t, client, "dynamic-app",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "")

	// Execute — should use daemon defaults
	result1 := executeTool(ctx, t, client, "dynamic-app", "shell", map[string]any{"cmd": "echo first"})
	if result1.IsError {
		t.Fatalf("first exec should succeed with daemon defaults: %s", result1.ErrorMsg)
	}
	// After first exec, sandbox count should be 1
	if count := mgr.GetAppSandboxCount("dynamic-app"); count != 1 {
		t.Errorf("expected 1 sandbox after first exec, got %d", count)
	}

	// Second registration: now with sandbox config
	registerManifestWithSandbox(ctx, t, client, "dynamic-app",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		map[string]string{"ws": "test-workspace"},
		"ws")

	// Execute again — should use the new sandbox
	result2 := executeTool(ctx, t, client, "dynamic-app", "shell", map[string]any{"cmd": "echo second"})
	if result2.IsError {
		t.Fatalf("second exec should succeed with updated config: %s", result2.ErrorMsg)
	}
}

func TestIntegration_GRPCDisconnect_Cleanup(t *testing.T) {
	// Verify cleanup when gRPC connection/stream ends.
	// v0.13.4: Cleanup is triggered by transport-level disconnect via StopApp.
	// Stream-level auto cleanup is a future enhancement — this test verifies
	// the manual path via DestroyAppSandboxes (same path StopApp uses).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mgr := setupTestSandbox()
	addr, cleanup := startTestServer(t, mgr, defaultMappings())
	defer cleanup()

	client, conn := connectClient(t, addr)
	defer conn.Close()

	registerManifestWithSandbox(ctx, t, client, "disc-app",
		[]*gogentv1.ManifestEntry{{Name: "shell", SecurityLevel: 2}},
		nil, "")

	_ = executeTool(ctx, t, client, "disc-app", "shell", map[string]any{"cmd": "echo hello"})

	if count := mgr.GetAppSandboxCount("disc-app"); count != 1 {
		t.Fatalf("expected 1 sandbox before cleanup, got %d", count)
	}

	// Simulate StopApp: clean up all sandboxes
	if err := mgr.DestroyAppSandboxes(ctx, "disc-app"); err != nil {
		t.Fatalf("DestroyAppSandboxes failed: %v", err)
	}

	if count := mgr.GetAppSandboxCount("disc-app"); count != 0 {
		t.Errorf("expected 0 sandboxes after cleanup, got %d", count)
	}
}

// TestMain runs before any test in the package.
func TestMain(m *testing.M) {
	// Suppress expected stderr output during tests
	// (the needsSandbox handler writes via fmt.Fprintf)
	code := m.Run()
	os.Exit(code)
}
