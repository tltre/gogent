package tool

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// Handler implements gogentv1.ToolServiceServer.
type Handler struct {
	gogentv1.UnimplementedToolServiceServer
	registry      *ToolRegistry
	manifestStore *ManifestStore
	serverStore   *ServerStore
	runner        *McpRunner // v0.12.8: unified MCP runner (process+http)
	runners       map[string]Runner // key: driver type ("builtin"|"process"|"http")
	runnersMu     sync.RWMutex
}

// NewHandler creates a ToolService gRPC handler backed by the given registry.
// It initializes runners for builtin (BuiltinRunner) and MCP (McpRunner),
// and registers built-in handler functions.
func NewHandler(registry *ToolRegistry, manifestStore *ManifestStore, serverStore *ServerStore) *Handler {
	mcpRunner := NewMcpRunner(serverStore, registry)

	h := &Handler{
		registry:      registry,
		manifestStore: manifestStore,
		serverStore:   serverStore,
		runner:        mcpRunner,
		runners:       make(map[string]Runner),
	}

	// Register driver runners
	br := NewBuiltinRunner()
	h.RegisterRunner(string(DriverBuiltin), br)
	h.RegisterRunner(string(DriverProcess), mcpRunner)  // McpRunner handles process
	h.RegisterRunner(string(DriverHTTP), mcpRunner)      // McpRunner handles http

	// Register 5 built-in handler functions
	RegisterBuiltinHandlers(br)

	return h
}

// StartAllServers starts all registered MCP servers and discovers their tools.
// Called after Handler creation during daemon initialization.
func (h *Handler) StartAllServers(ctx context.Context) {
	for _, def := range h.registry.List() {
		if def.Driver == string(DriverProcess) || def.Driver == string(DriverHTTP) {
			if err := h.runner.StartServer(ctx, def.Name); err != nil {
				fmt.Fprintf(os.Stderr, "[mcp] start %q: %v\n", def.Name, err)
			}
		}
	}
}

// RegisterRunner registers a Runner implementation for a driver type.
// Thread-safe. Can be called at runtime to replace a stub runner with a real one.
func (h *Handler) RegisterRunner(driver string, r Runner) {
	h.runnersMu.Lock()
	defer h.runnersMu.Unlock()
	h.runners[driver] = r
}

// getRunner returns the Runner for the given driver type (read-locked).
func (h *Handler) getRunner(driver string) (Runner, bool) {
	h.runnersMu.RLock()
	defer h.runnersMu.RUnlock()
	r, ok := h.runners[driver]
	return r, ok
}

// RegisterTool registers a new tool definition.
func (h *Handler) RegisterTool(ctx context.Context, req *gogentv1.RegisterToolRequest) (*gogentv1.RegisterToolResponse, error) {
	def := &ToolDefinition{
		Name:        req.Name,
		Driver:      req.Driver,
		Command:     req.Command,
		Endpoint:    req.Endpoint,
		DefaultLvl:  int(req.DefaultLevel),
		Description: req.Description,
		Env:         req.Env,
	}
	if err := h.registry.Register(def); err != nil {
		return &gogentv1.RegisterToolResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil
	}
	return &gogentv1.RegisterToolResponse{Success: true}, nil
}

// UnregisterTool removes a tool definition.
func (h *Handler) UnregisterTool(ctx context.Context, req *gogentv1.UnregisterToolRequest) (*gogentv1.UnregisterToolResponse, error) {
	if err := h.registry.Unregister(req.Name); err != nil {
		return &gogentv1.UnregisterToolResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil
	}
	return &gogentv1.UnregisterToolResponse{Success: true}, nil
}

// GetToolStatus returns status information for a tool.
func (h *Handler) GetToolStatus(ctx context.Context, req *gogentv1.GetToolStatusRequest) (*gogentv1.GetToolStatusResponse, error) {
	def, ok := h.registry.Get(req.Name)
	if !ok {
		return &gogentv1.GetToolStatusResponse{Exists: false}, nil
	}
	status := h.registry.GetStatus(req.Name)
	stats := h.registry.GetStats(req.Name)
	return &gogentv1.GetToolStatusResponse{
		Exists:          true,
		Name:            def.Name,
		Driver:          def.Driver,
		Status:          string(status),
		Description:     def.Description,
		DefaultLevel:    int32(def.DefaultLvl),
		InvocationCount: stats.Invocations,
		FailureCount:    stats.Failures,
	}, nil
}

// ExecuteTool implements the bidirectional streaming execution RPC.
//
// Pipeline (v0.12.9):
//  1. Wait for ToolExecuteRequest (first message)
//  2. Look up tool in registry
//  3. Send AuthResult (passed/failed)
//  4. Send HookInvocation (pre_execute) → wait for HookVerdict
//  5. Dispatch to driver-specific Runner.Execute
//  6. Send HookInvocation (post_execute) → wait for HookVerdict (can modify output)
//  7. Send ToolResult
//  8. RecordInvocation(success/failure)
//  9. Close stream
func (h *Handler) ExecuteTool(stream gogentv1.ToolService_ExecuteToolServer) error {
	seq := int64(0)

	// 1. First message must be ToolExecuteRequest
	firstMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "expected ToolExecuteRequest as first message")
	}
	req := firstMsg.GetRequest()
	if req == nil {
		return status.Errorf(codes.InvalidArgument, "first message must be ToolExecuteRequest")
	}

	// 2. Look up tool in registry
	def, exists := h.registry.Get(req.ToolName)
	if !exists {
		seq++
		stream.Send(&gogentv1.ToolExecutionEvent{
			Sequence: seq,
			Event: &gogentv1.ToolExecutionEvent_Auth{
				Auth: &gogentv1.AuthResult{
					Passed: false,
					Reason: "tool not found in registry: " + req.ToolName,
				},
			},
		})
		return nil
	}

	// 3. Send auth result
	seq++
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: seq,
		Event: &gogentv1.ToolExecutionEvent_Auth{
			Auth: &gogentv1.AuthResult{
				Passed:         true,
				EffectiveLevel: int32(def.DefaultLvl),
			},
		},
	})

	// 4. Pre-execute hook
	params := req.Params.AsMap()
	seq++
	blocked := h.handleHook(stream, seq, "pre_execute", req.ToolName, params)
	if blocked {
		h.registry.RecordInvocation(req.ToolName, false)
		return nil // hook already sent the error result
	}

	// 5. Extract params and dispatch to driver-specific Runner
	var execResult Result
	runner, ok := h.getRunner(def.Driver)
	if !ok {
		execResult = Result{IsError: true, ErrorMsg: "unknown driver: " + def.Driver}
	} else {
		var execErr error
		execResult, execErr = runner.Execute(stream.Context(), def, params)
		if execErr != nil {
			execResult = Result{IsError: true, ErrorMsg: def.Name + ": " + execErr.Error()}
		}
	}

	// 6. Post-execute hook (can modify output)
	seq++
	h.handlePostHook(stream, seq, req.ToolName, &execResult)

	// 7. Send result
	seq++
	var pbOutput *structpb.Value
	if execResult.Output != nil {
		pbOutput, err = structpb.NewValue(execResult.Output)
		if err != nil {
			pbOutput = structpb.NewStringValue("")
		}
	}
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: seq,
		Event: &gogentv1.ToolExecutionEvent_Result{
			Result: &gogentv1.ToolResult{
				Output:   pbOutput,
				IsError:  execResult.IsError,
				ErrorMsg: execResult.ErrorMsg,
			},
		},
	})

	// 8. Record invocation
	h.registry.RecordInvocation(req.ToolName, !execResult.IsError)
	return nil
}

// handleHook sends a HookInvocation and waits for the App's verdict.
// Returns true if the hook blocked execution (already sent error result).
func (h *Handler) handleHook(stream gogentv1.ToolService_ExecuteToolServer, seq int64, stage, toolName string, params map[string]any) bool {
	hookID := fmt.Sprintf("%s-%s-%d", toolName, stage, time.Now().UnixNano())

	payload := make(map[string]*structpb.Value)
	for k, v := range params {
		if pv, err := structpb.NewValue(v); err == nil {
			payload[k] = pv
		}
	}

	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: seq,
		Event: &gogentv1.ToolExecutionEvent_Hook{
			Hook: &gogentv1.HookInvocation{
				HookId:  hookID,
				Stage:   stage,
				Payload: payload,
			},
		},
	})

	// Wait for verdict
	msg, err := stream.Recv()
	if err != nil {
		return false // timeout/disconnect — continue execution
	}
	verdict := msg.GetVerdict()
	if verdict == nil {
		return false // unexpected message — continue
	}
	if !verdict.Approved {
		// Hook rejected — send error result and signal blocked
		stream.Send(&gogentv1.ToolExecutionEvent{
			Sequence: seq + 1,
			Event: &gogentv1.ToolExecutionEvent_Result{
				Result: &gogentv1.ToolResult{
					IsError:  true,
					ErrorMsg: "blocked by hook: " + verdict.Reason,
				},
			},
		})
		return true
	}

	// Apply modified params if the hook changed them (pre-execute)
	if len(verdict.ModifiedParams) > 0 {
		for k, v := range verdict.ModifiedParams {
			params[k] = v.AsInterface()
		}
	}
	return false
}

// handlePostHook sends a post-execute hook and applies output modifications.
func (h *Handler) handlePostHook(stream gogentv1.ToolService_ExecuteToolServer, seq int64, toolName string, execResult *Result) {
	hookID := fmt.Sprintf("%s-post-%d", toolName, time.Now().UnixNano())

	payload := make(map[string]*structpb.Value)
	if execResult.Output != nil {
		if pv, err := structpb.NewValue(execResult.Output); err == nil {
			payload["output"] = pv
		}
	}

	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: seq,
		Event: &gogentv1.ToolExecutionEvent_Hook{
			Hook: &gogentv1.HookInvocation{
				HookId:  hookID,
				Stage:   "post_execute",
				Payload: payload,
			},
		},
	})

	msg, err := stream.Recv()
	if err != nil {
		return // timeout/disconnect — use original result
	}
	verdict := msg.GetVerdict()
	if verdict == nil || verdict.Output == nil {
		return // no modifications
	}

	// Apply modified output from hook
	execResult.Output = verdict.Output.AsInterface()
}

// RegisterManifest validates and stores an app's tool manifest.
func (h *Handler) RegisterManifest(ctx context.Context, req *gogentv1.ManifestRequest) (*gogentv1.ManifestResponse, error) {
	if req.AppName == "" {
		return &gogentv1.ManifestResponse{
			Accepted:     false,
			ErrorMessage: "app_name is required",
		}, nil
	}

	var statuses []*gogentv1.ManifestStatus
	allAccepted := true

	for _, entry := range req.Tools {
		def, exists := h.registry.Get(entry.Name)
		if !exists {
			allAccepted = false
			statuses = append(statuses, &gogentv1.ManifestStatus{
				Name:     entry.Name,
				Accepted: false,
				Reason:   "tool not found in registry",
			})
			continue
		}

		effectiveLevel := int(entry.SecurityLevel)
		if effectiveLevel < def.DefaultLvl {
			effectiveLevel = def.DefaultLvl
		}

		statuses = append(statuses, &gogentv1.ManifestStatus{
			Name:           entry.Name,
			Accepted:       true,
			EffectiveLevel: int32(effectiveLevel),
		})
	}

	h.manifestStore.Register(req.AppName, req.Tools)

	return &gogentv1.ManifestResponse{
		Accepted: allAccepted,
		Tools:    statuses,
	}, nil
}
