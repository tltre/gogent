package tool

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// Handler implements gogentv1.ToolServiceServer.
type Handler struct {
	gogentv1.UnimplementedToolServiceServer
	registry *ToolRegistry
}

// NewHandler creates a ToolService gRPC handler backed by the given registry.
func NewHandler(registry *ToolRegistry) *Handler {
	return &Handler{registry: registry}
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
// v0.12.1 minimal implementation:
//  1. Wait for ToolExecuteRequest (first message)
//  2. Look up tool in registry
//  3. Send AuthResult (passed/failed)
//  4. Send ToolResult (not-yet-implemented)
//  5. Close stream
//
// Future versions will add HookInvocation↔HookVerdict and
// SandboxInvocation↔SandboxResult exchange before ToolResult.
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

	// 4. Send result (v0.12.1: no actual execution capability yet)
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: 2,
		Event: &gogentv1.ToolExecutionEvent_Result{
			Result: &gogentv1.ToolResult{
				IsError:  true,
				ErrorMsg: "tool " + req.ToolName + " (" + def.Driver + "): execution not yet implemented in v0.12.1",
			},
		},
	})

	h.registry.RecordInvocation(req.ToolName, false)
	return nil
}

// RegisterManifest is a stub for v0.12.3.
func (h *Handler) RegisterManifest(ctx context.Context, req *gogentv1.ManifestRequest) (*gogentv1.ManifestResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "RegisterManifest not implemented in v0.12.1")
}
