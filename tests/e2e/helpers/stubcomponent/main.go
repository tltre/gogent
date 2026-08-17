// Command stubcomponent is a standalone gRPC server that registers stub
// implementations for all 9 Gogent component services. It is forked by the
// daemon during e2e testing when config.command points to this binary.
//
// Usage:
//
//	stubcomponent --port 54321 --type provider
//
// The --type flag is accepted for compatibility with the daemon's fork
// convention but is effectively ignored — all 9 services are registered
// on a single server.
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	port := ":0"
	typ := "unknown"

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--port":
			if i+1 < len(os.Args) {
				port = strings.TrimPrefix(os.Args[i+1], ":")
				port = ":" + port
				i++
			}
		case "--type":
			if i+1 < len(os.Args) {
				typ = os.Args[i+1]
				i++
			}
		}
	}

	lis, err := net.Listen("tcp", port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "stubcomponent: listen %s: %v\n", port, err)
		os.Exit(1)
	}
	_ = typ // all services are registered regardless

	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
	)

	// Health service (required by daemon health checks).
	hs := health.NewServer()
	grpc_health_v1.RegisterHealthServer(srv, hs)
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// All 9 stub services.
	RegisterAll(srv)

	fmt.Fprintf(os.Stderr, "stubcomponent: all 9 services registered on %s  pid=%d\n", lis.Addr(), os.Getpid())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() { <-sigChan; srv.GracefulStop() }()

	if err := srv.Serve(lis); err != nil {
		fmt.Fprintf(os.Stderr, "stubcomponent: serve: %v\n", err)
		os.Exit(1)
	}
}
