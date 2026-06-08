package daemon

import (
	"context"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/tltre/gogent/internal/credentials"
	"github.com/tltre/gogent/internal/daemon/sandbox"
	"github.com/tltre/gogent/internal/daemon/tool"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// GrpcServer wraps the gRPC server that exposes ToolService to agent processes.
// It listens on localhost only (loopback), on basePort+1.
type GrpcServer struct {
	server   *grpc.Server
	listener net.Listener
	addr     string
	handler  *tool.Handler // v0.12.8: for StartAllServers
}

// NewGrpcServer creates a gRPC server with ToolService registered.
// basePort is the HTTP mgmt port (e.g. 9090); gRPC listens on basePort+1 (e.g. 9091).
// Returns nil if the port cannot be listened on.
func NewGrpcServer(basePort int, registry *tool.ToolRegistry, manifestStore *tool.ManifestStore, resolver *credentials.Resolver, serverStore *tool.ServerStore, sandboxMgr *sandbox.SandboxManager, sandboxDefaults *sandbox.DefaultMappings) *GrpcServer {
	addr := fmt.Sprintf("127.0.0.1:%d", basePort+1)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[grpc] failed to listen on %s: %v\n", addr, err)
		return nil
	}

	s := grpc.NewServer(
		grpc.MaxRecvMsgSize(64 * 1024 * 1024), // 64MB max message size
	)

	// Health service (gRPC standard)
	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(s, healthSrv)
	healthSrv.SetServingStatus("gogent.v1.ToolService", grpc_health_v1.HealthCheckResponse_SERVING)

	// ToolService
	handler := tool.NewHandler(registry, manifestStore, serverStore, sandboxMgr, sandboxDefaults)
	gogentv1.RegisterToolServiceServer(s, handler)

	// v0.13.3: SandboxManager service (app-side sandbox CRUD + exec)
	if sandboxMgr != nil {
		gogentv1.RegisterSandboxManagerServer(s, &sandboxManagerGRPC{sandboxMgr: sandboxMgr})
	}

	// Reflection for debugging (grpc_cli, grpcurl, etc.)
	reflection.Register(s)

	return &GrpcServer{
		server:   s,
		listener: lis,
		handler:  handler,
		addr:     addr,
	}
}

// Start begins listening in a background goroutine. Safe to call on nil.
func (g *GrpcServer) Start() {
	if g == nil || g.server == nil {
		return
	}
	go func() {
		fmt.Fprintf(os.Stderr, "[grpc] ToolService listening on %s\n", g.addr)
		if err := g.server.Serve(g.listener); err != nil {
			fmt.Fprintf(os.Stderr, "[grpc] serve error: %v\n", err)
		}
	}()
}

// Addr returns the gRPC server address (e.g. "127.0.0.1:9091"). Returns "" if nil.
func (g *GrpcServer) Addr() string {
	if g == nil {
		return ""
	}
	return g.addr
}

// Handler returns the ToolService handler, nil if GrpcServer is nil.
func (g *GrpcServer) Handler() *tool.Handler {
	if g == nil {
		return nil
	}
	return g.handler
}

// Shutdown gracefully stops the gRPC server. Safe to call on nil.
func (g *GrpcServer) Shutdown(ctx context.Context) error {
	if g == nil || g.server == nil {
		return nil
	}
	stopped := make(chan struct{})
	go func() {
		g.server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		g.server.Stop()
		return ctx.Err()
	}
}
