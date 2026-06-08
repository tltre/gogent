package daemon

import (
	"context"
	"fmt"

	"google.golang.org/grpc/metadata"

	"github.com/tltre/gogent/internal/daemon/sandbox"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// sandboxManagerGRPC implements gogentv1.SandboxManagerServer.
// It wraps the daemon's SandboxManager and resolves the calling app
// from gRPC metadata.
type sandboxManagerGRPC struct {
	gogentv1.UnimplementedSandboxManagerServer
	sandboxMgr *sandbox.SandboxManager
}

func (s *sandboxManagerGRPC) ListSandboxes(ctx context.Context, req *gogentv1.ListSandboxesRequest) (*gogentv1.SandboxInfoList, error) {
	appName := extractAppName(ctx)
	_ = appName
	// TODO: wire up app-scoped sandbox listing
	return &gogentv1.SandboxInfoList{}, nil
}

func (s *sandboxManagerGRPC) CreateSandbox(ctx context.Context, req *gogentv1.CreateSandboxRequest) (*gogentv1.SandboxInfo, error) {
	appName := extractAppName(ctx)
	if appName == "unknown" {
		return nil, fmt.Errorf("app name not found in gRPC metadata")
	}

	cfg := &sandbox.SandboxConfig{
		Name:        req.Name,
		ProfileName: req.Profile,
	}
	// Set lifecycle from request
	switch req.Lifecycle {
	case "persistent":
		cfg.Lifecycle.Mode = sandbox.LifecyclePersistent
	case "ephemeral":
		cfg.Lifecycle.Mode = sandbox.LifecycleEphemeral
	default:
		cfg.Lifecycle.Mode = sandbox.LifecycleSession
	}

	_, err := s.sandboxMgr.GetOrCreateForApp(ctx, appName, cfg)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	return &gogentv1.SandboxInfo{
		Name:    req.Name,
		Profile: req.Profile,
		Status:  "active",
	}, nil
}

func (s *sandboxManagerGRPC) GetSandbox(ctx context.Context, req *gogentv1.GetSandboxRequest) (*gogentv1.SandboxInfo, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *sandboxManagerGRPC) DestroySandbox(ctx context.Context, req *gogentv1.DestroySandboxRequest) (*gogentv1.DestroySandboxResponse, error) {
	appName := extractAppName(ctx)
	if err := s.sandboxMgr.DestroyAppSandboxes(ctx, appName); err != nil {
		return &gogentv1.DestroySandboxResponse{Success: false}, nil
	}
	return &gogentv1.DestroySandboxResponse{Success: true}, nil
}

func (s *sandboxManagerGRPC) ExecInSandbox(ctx context.Context, req *gogentv1.ExecInSandboxRequest) (*gogentv1.ExecResult, error) {
	return nil, fmt.Errorf("not implemented")
}

// extractAppName reads the app name from gRPC metadata.
func extractAppName(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "unknown"
	}
	vals := md.Get("app-name")
	if len(vals) == 0 {
		return "unknown"
	}
	return vals[0]
}
