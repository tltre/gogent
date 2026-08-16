package tool

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	internal_otel "github.com/tltre/gogent/internal/otel"
	"github.com/tltre/gogent/pkg/hook"
)

// ManifestEntry represents a single tool declaration from the App's YAML
// or WithTool BuildOption.
type ManifestEntry struct {
	Name    string
	Level   int    // security level (0-2)
	Sandbox string // v0.13.4: per-tool sandbox instance name (from tools[].sandbox)
}

// SandboxDecl represents a sandbox instance declaration from the app config.
type SandboxDecl struct {
	Name    string // sandbox instance name (e.g. "workspace")
	Profile string // references daemon's sandbox-profiles entry
}

// ToolManager manages tool execution through the daemon's ToolService.
// It connects to the daemon gRPC server, registers the app's manifest,
// and proxies all tool execution requests.
//
// v0.12.2: No local ITool registration. Daemon is the only tool source.
type ToolManager struct {
	mu sync.RWMutex

	appName     string                       // application name (for RegisterManifest)
	manifest    []ManifestEntry              // tool declarations (from YAML / WithTool)
	cache       []ToolInfo                   // accepted tools after RegisterManifest
	connected   bool                         // true after successful RegisterManifest
	grpcConn    *grpctransport.Conn          // gRPC connection to daemon
	client      gogentv1.ToolServiceClient   // ToolService gRPC client
	hookManager *hook.HookManager            // v0.12.9: for handling hook invocations

	// v0.13.3: Sandbox configuration
	sandboxes      []SandboxDecl // sandbox instance declarations
	defaultSandbox string        // app-level default sandbox name

	dialFn func() (*grpctransport.Conn, error) // dial override for testing; nil = real dial
}

// NewToolManager creates a ToolManager with the given app name.
// It does not connect to the daemon until Start() is called.
func NewToolManager(appName string) *ToolManager {
	return &ToolManager{
		appName:  appName,
		cache:    nil,
	}
}

// SetHookManager sets the hook manager for processing hook invocations (v0.12.9).
func (tm *ToolManager) SetHookManager(hm *hook.HookManager) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.hookManager = hm
}

// SetManifest sets the tool declarations. Called by Builder during App construction.
func (tm *ToolManager) SetManifest(appName string, entries []ManifestEntry) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.appName = appName
	tm.manifest = entries
}

// SetSandboxConfig sets sandbox declarations and the default sandbox name.
// Called by Builder during App construction (v0.13.3).
func (tm *ToolManager) SetSandboxConfig(sandboxes []SandboxDecl, defaultSb string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.sandboxes = sandboxes
	tm.defaultSandbox = defaultSb
}

// Start connects to the daemon gRPC server and registers the tool manifest.
// If the daemon is unreachable, it logs a warning and continues without tools.
// This is called from App.Run(), not from the component registry.
func (tm *ToolManager) Start(ctx context.Context) error {
	if err := tm.dialDaemon(); err != nil {
		// Daemon unavailable — continue without tools.
		tm.mu.Lock()
		tm.connected = false
		tm.mu.Unlock()
		return nil
	}

	if len(tm.manifest) > 0 {
		if err := tm.registerManifest(ctx); err != nil {
			// Manifest rejected — continue, Execute() will fail gracefully.
			tm.mu.Lock()
			tm.connected = false
			tm.mu.Unlock()
			return nil
		}
	}

	return nil
}

// Stop closes the gRPC connection to the daemon.
func (tm *ToolManager) Stop(_ context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.grpcConn != nil {
		tm.grpcConn.Close()
		tm.grpcConn = nil
	}
	tm.client = nil
	tm.connected = false
	return nil
}

// List returns the cached list of accepted tools from RegisterManifest.
// Returns nil if no manifest has been registered.
func (tm *ToolManager) List() []ToolInfo {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if len(tm.cache) == 0 {
		return nil
	}
	result := make([]ToolInfo, len(tm.cache))
	copy(result, tm.cache)
	return result
}

// ListByServer returns all tools belonging to a specific MCP server.
func (tm *ToolManager) ListByServer(serverName string) []ToolInfo {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	var result []ToolInfo
	prefix := serverName + "."
	for _, t := range tm.cache {
		if len(t.Name) > len(prefix) && t.Name[:len(prefix)] == prefix {
			result = append(result, t)
		}
	}
	return result
}

// ListServers returns the names of all MCP servers (plus standalone tools).
func (tm *ToolManager) ListServers() []string {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	seen := make(map[string]bool)
	var servers []string
	for _, t := range tm.cache {
		// Extract server name from "<server>.<tool>" or use the name itself
		serverName := t.Name
		for i := len(t.Name) - 1; i >= 0; i-- {
			if t.Name[i] == '.' {
				serverName = t.Name[:i]
				break
			}
		}
		if !seen[serverName] {
			seen[serverName] = true
			servers = append(servers, serverName)
		}
	}
	return servers
}

// Execute sends a tool execution request to the daemon via gRPC bidirectional stream.
// Returns an error if the daemon is not connected.
//
// v0.15.x: the full execution is observed under a "tool.exec" span. The
// bidirectional exchange (auth → hooks → result) is recorded as span events in
// executeRemote, so the app side keeps a complete audit of the tool call
// without requiring the daemon to export traces itself (multi-app backends).
func (tm *ToolManager) Execute(ctx context.Context, name string, params map[string]any) (Result, error) {
	tm.mu.RLock()
	client := tm.client
	connected := tm.connected
	tm.mu.RUnlock()

	if !connected || client == nil {
		return Result{IsError: true, ErrorMsg: "daemon unavailable"}, fmt.Errorf("daemon unavailable")
	}

	tracer := internal_otel.Tracer("gogent.tool")
	ctx, span := tracer.Start(ctx, "tool.exec",
		trace.WithAttributes(attribute.String("tool", name)))
	defer span.End()

	start := time.Now()
	result, err := tm.executeRemote(ctx, client, name, params)

	span.SetAttributes(
		attribute.Bool("result.is_error", result.IsError),
		attribute.String("result.error", result.ErrorMsg),
		attribute.Int64("dur_ms", time.Since(start).Milliseconds()),
	)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return result, err
	}
	span.SetStatus(codes.Ok, "")
	return result, nil
}

// executeRemote opens a bidirectional ExecuteTool stream, sends the request,
// and reads events until ToolResult.
func (tm *ToolManager) executeRemote(ctx context.Context, client gogentv1.ToolServiceClient, name string, params map[string]any) (Result, error) {
	stream, err := client.ExecuteTool(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("open execute stream: %w", err)
	}
	defer stream.CloseSend()

	// Convert params to protobuf Struct
	structParams, err := structpb.NewStruct(params)
	if err != nil {
		return Result{}, fmt.Errorf("convert params: %w", err)
	}

	// Send ExecuteRequest (first message in the bidirectional stream)
	if err := stream.Send(&gogentv1.ToolControl{
		Msg: &gogentv1.ToolControl_Request{
			Request: &gogentv1.ToolExecuteRequest{
				ToolName: name,
				Params:   structParams,
			},
		},
	}); err != nil {
		return Result{}, fmt.Errorf("send execute request: %w", err)
	}

	// Read events until ToolResult or EOF
	var result Result
	for {
		evt, err := stream.Recv()
		if err != nil {
			break
		}
		switch e := evt.Event.(type) {
		case *gogentv1.ToolExecutionEvent_Auth:
			// v0.15.x: record the authorization stage of the exchange.
			trace.SpanFromContext(ctx).AddEvent("tool.auth",
				trace.WithAttributes(
					attribute.Bool("passed", e.Auth.Passed),
					attribute.String("reason", e.Auth.Reason),
				))
			if !e.Auth.Passed {
				return Result{IsError: true, ErrorMsg: e.Auth.Reason}, nil
			}

		case *gogentv1.ToolExecutionEvent_Hook:
			// App-side: handle hook invocation and reply with verdict
			stage := e.Hook.Stage
			verdict := tm.HandleHookInvocation(ctx, e.Hook)
			trace.SpanFromContext(ctx).AddEvent("tool.hook",
				trace.WithAttributes(
					attribute.String("stage", stage),
					attribute.Bool("approved", verdict.Approved),
					attribute.String("reason", verdict.Reason),
				))
			if err := stream.Send(&gogentv1.ToolControl{
				Msg: &gogentv1.ToolControl_Verdict{
					Verdict: verdict,
				},
			}); err != nil {
				return Result{}, fmt.Errorf("send hook verdict: %w", err)
			}

		case *gogentv1.ToolExecutionEvent_Result:
			result = resultFromProto(e.Result)
			trace.SpanFromContext(ctx).AddEvent("tool.result",
				trace.WithAttributes(
					attribute.Bool("is_error", result.IsError),
					attribute.String("error_msg", result.ErrorMsg),
				))
		}
	}

	return result, nil
}

// HandleHookInvocation processes a HookInvocation from the daemon and returns a verdict.
// If no hook manager is configured, auto-approves.
func (tm *ToolManager) HandleHookInvocation(ctx context.Context, inv *gogentv1.HookInvocation) *gogentv1.HookVerdict {
	verdict := &gogentv1.HookVerdict{
		HookId:   inv.HookId,
		Approved: true,
	}

	if tm.hookManager == nil {
		return verdict
	}

	eventType := hook.EventBeforeTool
	if inv.Stage == "post_execute" {
		eventType = hook.EventAfterTool
	}

	for _, h := range tm.hookManager.GetHooks(eventType) {
		event := hook.Event{Type: eventType, Payload: inv.Payload}
		_, err := h.OnEvent(ctx, event)
		if err != nil {
			verdict.Approved = false
			verdict.Reason = err.Error()
			return verdict
		}
	}

	// If post-execute and hook output was provided, send it back
	if inv.Stage == "post_execute" && inv.Payload["output"] != nil {
		verdict.Output = inv.Payload["output"]
	}

	return verdict
}

// resultFromProto converts a protobuf ToolResult to a domain Result.
func resultFromProto(pb *gogentv1.ToolResult) Result {
	if pb == nil {
		return Result{}
	}
	r := Result{
		IsError:  pb.IsError,
		ErrorMsg: pb.ErrorMsg,
	}
	if pb.Output != nil {
		r.Output = pb.Output.AsInterface()
	}
	return r
}
