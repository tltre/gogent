package grpctransport

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Conn wraps a gRPC client connection with optional health checking.
type Conn struct {
	conn   *grpc.ClientConn
	target string
}

// Dial creates a new gRPC connection to the given target using insecure transport.
// After connecting, it optionally performs a health check.
func Dial(target string) (*Conn, error) {
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpctransport: dial %s: %w", target, err)
	}

	return &Conn{conn: conn, target: target}, nil
}

// Close closes the underlying gRPC connection.
func (c *Conn) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// ClientConn returns the underlying *grpc.ClientConn for use with generated gRPC clients.
func (c *Conn) ClientConn() *grpc.ClientConn {
	return c.conn
}

// Target returns the target address of this connection.
func (c *Conn) Target() string {
	return c.target
}

// HealthCheck performs a one-shot health check using the gRPC health protocol.
// Returns nil if the service is SERVING, or an error otherwise.
func (c *Conn) HealthCheck(ctx context.Context) error {
	hc := grpc_health_v1.NewHealthClient(c.conn)
	resp, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return fmt.Errorf("grpctransport: health check %s: %w", c.target, err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("grpctransport: health check %s: status %s", c.target, resp.Status.String())
	}
	return nil
}

// WatchHealth returns a streaming health check client for continuous monitoring.
// Callers should call Recv() on the returned stream to receive health status updates.
func (c *Conn) WatchHealth(ctx context.Context) (grpc_health_v1.Health_WatchClient, error) {
	hc := grpc_health_v1.NewHealthClient(c.conn)
	stream, err := hc.Watch(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return nil, fmt.Errorf("grpctransport: watch health %s: %w", c.target, err)
	}
	return stream, nil
}
