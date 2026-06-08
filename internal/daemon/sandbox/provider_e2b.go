package sandbox

import (
	"context"
	"fmt"

	e2bsdk "github.com/matiasinsaurralde/go-e2b"
)

// ---------------------------------------------------------------------------
// E2BProvider
// ---------------------------------------------------------------------------

// E2BProvider creates sandbox instances via the E2B-compatible REST API.
// It uses the community-maintained Go SDK (github.com/matiasinsaurralde/go-e2b).
type E2BProvider struct {
	name     string
	client   *e2bsdk.Client
	endpoint string
}

// NewE2BProvider creates an E2BProvider with the given API key and optional endpoint.
// If endpoint is empty, the SDK's default (https://api.e2b.app) is used.
func NewE2BProvider(name, apiKey, endpoint string) (*E2BProvider, error) {
	cfg := e2bsdk.ClientConfig{
		APIKey: apiKey,
	}
	if endpoint != "" {
		cfg.APIBaseURL = endpoint
	}

	client, err := e2bsdk.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("e2b client: %w", err)
	}

	return &E2BProvider{
		name:     name,
		client:   client,
		endpoint: endpoint,
	}, nil
}

func (p *E2BProvider) Name() string      { return p.name }
func (p *E2BProvider) Type() SandboxType { return SandboxE2B }

func (p *E2BProvider) Create(ctx context.Context, profile *SandboxProfile, cfg *SandboxConfig) (ISandbox, error) {
	timeout := determineTimeout(cfg)

	sbCfg := e2bsdk.SandboxConfig{
		Template: profile.Template,
		Timeout:  timeout,
	}

	sdkSb, err := p.client.NewSandbox(ctx, sbCfg)
	if err != nil {
		return nil, fmt.Errorf("e2b create: %w", err)
	}

	return &e2bSandbox{
		sdk:       sdkSb,
		timeout:   timeout,
	}, nil
}

// ---------------------------------------------------------------------------
// e2bSandbox
// ---------------------------------------------------------------------------

// e2bSandbox wraps the SDK's Sandbox instance and implements ISandbox.
type e2bSandbox struct {
	sdk     *e2bsdk.Sandbox
	timeout int // seconds
}

func (s *e2bSandbox) Execute(ctx context.Context, req ExecRequest) (ExecResult, error) {
	args := []string{"-c", req.Code}
	opts := []e2bsdk.RunOption{}
	if req.Timeout > 0 {
		opts = append(opts, e2bsdk.WithTimeout(req.Timeout))
	}

	result, err := s.sdk.Commands.RunWithContext(ctx, "sh", args, opts...)
	if err != nil {
		return ExecResult{}, fmt.Errorf("e2b exec: %w", err)
	}

	return ExecResult{
		Stdout:   result.Stdout,
		Stderr:   result.Stderr,
		ExitCode: result.ExitCode,
	}, nil
}

func (s *e2bSandbox) Close(ctx context.Context) error {
	return s.sdk.CloseWithContext(ctx)
}

// Refresh implements Refreshable. Resets the sandbox TTL.
func (s *e2bSandbox) Refresh(ctx context.Context) error {
	return s.sdk.SetTimeoutWithContext(ctx, s.timeout)
}

// CreateSnapshot creates a point-in-time snapshot of the sandbox.
// The snapshot can be used to restore the sandbox later.
func (s *e2bSandbox) CreateSnapshot(ctx context.Context) (string, error) {
	snapshot, err := s.sdk.CreateSnapshot(ctx)
	if err != nil {
		return "", fmt.Errorf("e2b snapshot: %w", err)
	}
	return snapshot.SnapshotID, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// determineTimeout returns the TTL in seconds based on the sandbox config.
// timeout=0 means the E2B default (300s). For persistent sandboxes, use 3600.
func determineTimeout(cfg *SandboxConfig) int {
	if cfg.Lifecycle.Mode == LifecyclePersistent {
		return 3600
	}
	if cfg.Lifecycle.Timeout > 0 {
		return int(cfg.Lifecycle.Timeout.Seconds())
	}
	return 300 // default
}
