package tool

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ManifestEntry represents a single tool declaration from the App's YAML
// or WithTool BuildOption.
type ManifestEntry struct {
	Name  string
	Level int // security level (0-2)
}

// ToolManager manages tool execution through the daemon's ToolService.
// It connects to the daemon gRPC server, registers the app's manifest,
// and proxies all tool execution requests.
//
// v0.12.2: No local ITool registration. Daemon is the only tool source.
type ToolManager struct {
	mu sync.RWMutex

	appName   string                       // application name (for RegisterManifest)
	manifest  []ManifestEntry              // tool declarations (from YAML / WithTool)
	cache     []ToolInfo                   // accepted tools after RegisterManifest
	connected bool                         // true after successful RegisterManifest
	grpcConn  *grpctransport.Conn          // gRPC connection to daemon
	client    gogentv1.ToolServiceClient   // ToolService gRPC client

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

// SetManifest sets the tool declarations. Called by Builder during App construction.
func (tm *ToolManager) SetManifest(appName string, entries []ManifestEntry) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.appName = appName
	tm.manifest = entries
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

// Execute sends a tool execution request to the daemon via gRPC bidirectional stream.
// Returns an error if the daemon is not connected.
func (tm *ToolManager) Execute(ctx context.Context, name string, params map[string]any) (Result, error) {
	tm.mu.RLock()
	client := tm.client
	connected := tm.connected
	tm.mu.RUnlock()

	if !connected || client == nil {
		return Result{IsError: true, ErrorMsg: "daemon unavailable"}, fmt.Errorf("daemon unavailable")
	}

	return tm.executeRemote(ctx, client, name, params)
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
			if !e.Auth.Passed {
				return Result{IsError: true, ErrorMsg: e.Auth.Reason}, nil
			}
		case *gogentv1.ToolExecutionEvent_Result:
			result = resultFromProto(e.Result)
		}
	}

	return result, nil
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
