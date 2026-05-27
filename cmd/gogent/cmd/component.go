package cmd

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// registerStubs is set by an e2e-tagged init() in stub_hook.go.
// When non-nil, the component subcommand registers component-specific
// gRPC services (stub implementations) in addition to the health service.
var registerStubs func(srv *grpc.Server, typ string)

var (
	componentName string
	componentPort string
	componentType string
)

var componentCmd = &cobra.Command{
	Use:    "component",
	Short:  "Internal: forked by daemon to run an independent component process",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runComponent(componentName, componentPort, componentType)
	},
}

func init() {
	componentCmd.Flags().StringVar(&componentName, "name", "", "Component name (required)")
	componentCmd.Flags().StringVar(&componentPort, "port", "", "Listen port for gRPC health (required)")
	componentCmd.Flags().StringVar(&componentType, "type", "", "Component type (required)")
	componentCmd.MarkFlagRequired("name")
	componentCmd.MarkFlagRequired("port")
	componentCmd.MarkFlagRequired("type")
}

func runComponent(name, port, typ string) error {
	// Normalize port: strip ":" prefix if present.
	port = strings.TrimPrefix(port, ":")
	addr := ":" + port

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)
	hs := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, hs)
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Register component-specific gRPC service.
	// Built with -tags e2e: uses stub implementations from tests/e2e/helpers.
	// Normal build: no-op (registerStubs is nil).
	if registerStubs != nil {
		registerStubs(srv, typ)
	}

	fmt.Fprintf(os.Stderr, "component %q (%s) serving gRPC on %s  pid=%d\n", name, typ, addr, os.Getpid())

	// Block until signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Fprintf(os.Stderr, "component %q received %v, shutting down\n", name, sig)
		srv.GracefulStop()
	}()

	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("gRPC serve: %w", err)
	}

	return nil
}
