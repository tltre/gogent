package tool

import (
	"context"
	"sync"

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
	runners       map[string]Runner // key: driver type ("builtin"|"process"|"http")
	runnersMu     sync.RWMutex
}

// NewHandler creates a ToolService gRPC handler backed by the given registry.
// It initializes default runners for builtin/process/http and registers stub
// handlers for the 5 built-in tools.
func NewHandler(registry *ToolRegistry, manifestStore *ManifestStore) *Handler {
	h := &Handler{
		registry:      registry,
		manifestStore: manifestStore,
		runners:       make(map[string]Runner),
	}

	// Register driver runners
	br := NewBuiltinRunner()
	h.RegisterRunner(string(DriverBuiltin), br)
	h.RegisterRunner(string(DriverProcess), NewStubRunner("process"))
	h.RegisterRunner(string(DriverHTTP), NewStubRunner("http"))

	// Register 5 built-in stub handlers into the builtin runner
	RegisterBuiltinHandlers(br)

	return h
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
// Pipeline (v0.12.3):
//  1. Wait for ToolExecuteRequest (first message)
//  2. Look up tool in registry
//  3. Send AuthResult (passed/failed)
//  4. Dispatch to driver-specific Runner.Execute
//  5. Send ToolResult
//  6. RecordInvocation(success/failure)
//  7. Close stream
//
// Future versions will add HookInvocation↔HookVerdict and
// SandboxInvocation↔SandboxResult exchange between steps 3 and 4.
func (h *Handler) ExecuteTool(stream gogentv1.ToolService_ExecuteToolServer) error {
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
		stream.Send(&gogentv1.ToolExecutionEvent{
			Sequence: 1,
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
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: 1,
		Event: &gogentv1.ToolExecutionEvent_Auth{
			Auth: &gogentv1.AuthResult{
				Passed:         true,
				EffectiveLevel: int32(def.DefaultLvl),
			},
		},
	})

	// 4. Extract params and dispatch to driver-specific Runner
	params := req.Params.AsMap()
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

	// 5. Send result
	var pbOutput *structpb.Value
	if execResult.Output != nil {
		pbOutput, err = structpb.NewValue(execResult.Output)
		if err != nil {
			pbOutput = structpb.NewStringValue("")
		}
	}
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: 2,
		Event: &gogentv1.ToolExecutionEvent_Result{
			Result: &gogentv1.ToolResult{
				Output:   pbOutput,
				IsError:  execResult.IsError,
				ErrorMsg: execResult.ErrorMsg,
			},
		},
	})

	// 6. Record invocation
	h.registry.RecordInvocation(req.ToolName, !execResult.IsError)
	return nil
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
