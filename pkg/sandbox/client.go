// Package sandbox provides the App-side thin SandboxManager client.
//
// It communicates with the Daemon over gRPC to manage sandbox lifecycle
// (create, list, get, destroy) and execute commands within sandboxes.
// This is the App-side counterpart of internal/daemon/sandbox/ (daemon side).
package sandbox

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ---------------------------------------------------------------------------
// Client
// ---------------------------------------------------------------------------

// Client is the App-side thin sandbox manager client.
// It connects to the Daemon's SandboxManager gRPC service.
type Client struct {
	appName string
	client  gogentv1.SandboxManagerClient
	conn    *grpc.ClientConn
}

// NewClient creates a new SandboxManager client connected to the daemon.
// daemonAddr is the Daemon's gRPC address (e.g. "127.0.0.1:9091").
func NewClient(daemonAddr, appName string) (*Client, error) {
	conn, err := grpc.NewClient(daemonAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("sandbox client: dial: %w", err)
	}
	return &Client{
		appName: appName,
		client:  gogentv1.NewSandboxManagerClient(conn),
		conn:    conn,
	}, nil
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

// List returns all sandbox instances for this app.
func (c *Client) List(ctx context.Context) ([]SandboxInfo, error) {
	result, err := c.client.ListSandboxes(ctx, &gogentv1.ListSandboxesRequest{})
	if err != nil {
		return nil, fmt.Errorf("list sandboxes: %w", err)
	}
	infos := make([]SandboxInfo, len(result.Items))
	for i, item := range result.Items {
		infos[i] = SandboxInfo{
			Name: item.Name, Profile: item.Profile, Status: item.Status,
		}
	}
	return infos, nil
}

// Create creates a new sandbox instance.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*SandboxInfo, error) {
	r, err := c.client.CreateSandbox(ctx, &gogentv1.CreateSandboxRequest{
		Name: req.Name, Profile: req.Profile,
		Lifecycle: req.Lifecycle, TimeoutMs: durToMs(req.Timeout),
	})
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}
	return &SandboxInfo{Name: r.Name, Profile: r.Profile, Status: r.Status}, nil
}

// Get returns details for a specific sandbox.
func (c *Client) Get(ctx context.Context, name string) (*SandboxInfo, error) {
	r, err := c.client.GetSandbox(ctx, &gogentv1.GetSandboxRequest{SandboxName: name})
	if err != nil {
		return nil, fmt.Errorf("get sandbox: %w", err)
	}
	return &SandboxInfo{Name: r.Name, Profile: r.Profile, Status: r.Status}, nil
}

// Destroy removes a sandbox instance.
func (c *Client) Destroy(ctx context.Context, name string) error {
	_, err := c.client.DestroySandbox(ctx, &gogentv1.DestroySandboxRequest{SandboxName: name})
	if err != nil {
		return fmt.Errorf("destroy sandbox: %w", err)
	}
	return nil
}

// Execute runs a command inside a sandbox and returns the result.
func (c *Client) Execute(ctx context.Context, sandboxName string, req ExecRequest) (ExecResult, error) {
	r, err := c.client.ExecInSandbox(ctx, &gogentv1.ExecInSandboxRequest{
		SandboxName: sandboxName, Code: req.Code, Language: req.Language,
		TimeoutMs: durToMs(req.Timeout), Env: req.Env,
	})
	if err != nil {
		return ExecResult{}, fmt.Errorf("execute sandbox: %w", err)
	}
	var e error
	if r.Error != "" {
		e = fmt.Errorf("%s", r.Error)
	}
	return ExecResult{
		Stdout: r.Stdout, Stderr: r.Stderr,
		ExitCode: int(r.ExitCode), Error: e,
	}, nil
}

func durToMs(d time.Duration) int64 { return d.Milliseconds() }

// ---------------------------------------------------------------------------
// App-side types
// ---------------------------------------------------------------------------

type SandboxInfo struct {
	Name    string
	Profile string
	Status  string
}

type CreateRequest struct {
	Name      string
	Profile   string
	Lifecycle string
	Timeout   time.Duration
}
