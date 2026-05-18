package sandbox

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	"github.com/tltre/gogent/internal/otel"
)

// ProcessSandboxConfig holds the configuration for a gRPC-based ProcessSandbox.
type ProcessSandboxConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessSandbox is a sandbox implementation that communicates with a remote
// sandbox service over gRPC.
type ProcessSandbox struct {
	cfg    *ProcessSandboxConfig
	client gogentv1.SandboxServiceClient
	limits ResourceLimits
}

// NewProcessSandbox creates a new ProcessSandbox. The gRPC client is lazily
// initialized on the first method call.
func NewProcessSandbox(cfg *ProcessSandboxConfig) *ProcessSandbox {
	return &ProcessSandbox{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC SandboxServiceClient.
func (s *ProcessSandbox) getClient() (gogentv1.SandboxServiceClient, error) {
	if s.client != nil {
		return s.client, nil
	}
	conn, err := s.cfg.Pool.Get(s.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("sandbox: get connection: %w", err)
	}
	s.client = gogentv1.NewSandboxServiceClient(conn.ClientConn())
	return s.client, nil
}

// Create allocates a new sandbox instance via gRPC.
func (s *ProcessSandbox) Create(ctx context.Context) (string, error) {
	tracer := otel.Tracer("gogent.sandbox")
	ctx, span := tracer.Start(ctx, "sandbox.create")
	defer span.End()

	client, err := s.getClient()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	resp, err := client.Create(ctx, &gogentv1.CreateRequest{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return "", err
	}
	return resp.SandboxId, nil
}

// Destroy tears down a sandbox instance via gRPC.
func (s *ProcessSandbox) Destroy(ctx context.Context, id string) error {
	client, err := s.getClient()
	if err != nil {
		return err
	}
	_, err = client.Destroy(ctx, &gogentv1.DestroyRequest{SandboxId: id})
	return err
}

// Execute runs code inside an existing sandbox via gRPC.
func (s *ProcessSandbox) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	tracer := otel.Tracer("gogent.sandbox")
	ctx, span := tracer.Start(ctx, "sandbox.execute",
		trace.WithAttributes(
			attribute.String("sandbox_id", sandboxID),
			attribute.String("language", req.Language),
		),
	)
	defer span.End()

	client, err := s.getClient()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ExecResult{}, err
	}
	resp, err := client.Execute(ctx, execRequestToProto(sandboxID, req))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ExecResult{}, err
	}
	return execResultFromProto(resp), nil
}

// SetLimits stores resource limits locally. The gRPC proto does not define
// a SetLimits RPC, so limits are kept in-process.
func (s *ProcessSandbox) SetLimits(limits ResourceLimits) {
	s.limits = limits
}

// GetLimits returns the locally stored resource limits.
func (s *ProcessSandbox) GetLimits() ResourceLimits {
	return s.limits
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func execRequestToProto(sandboxID string, req ExecRequest) *gogentv1.ExecuteRequest {
	return &gogentv1.ExecuteRequest{
		SandboxId: sandboxID,
		Code:      req.Code,
		Language:  req.Language,
		TimeoutMs: req.Timeout.Milliseconds(),
		Files:     req.Files,
		Env:       req.Env,
	}
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func execResultFromProto(resp *gogentv1.ExecuteResponse) ExecResult {
	r := ExecResult{
		Stdout:   resp.Stdout,
		Stderr:   resp.Stderr,
		ExitCode: int(resp.ExitCode),
		Duration: time.Duration(resp.DurationMs) * time.Millisecond,
	}
	if resp.Error != "" {
		r.Error = fmt.Errorf("%s", resp.Error)
	}
	return r
}
