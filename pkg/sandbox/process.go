package sandbox

import (
	"context"

	"github.com/tltre/gagent/internal/client"
)

type ProcessSandboxConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessSandbox struct {
	cfg *ProcessSandboxConfig
}

func NewProcessSandbox(cfg *ProcessSandboxConfig) *ProcessSandbox {
	return &ProcessSandbox{cfg: cfg}
}

func (s *ProcessSandbox) Create(ctx context.Context) (string, error) {
	var id string
	if err := s.cfg.Transport.Call(ctx, "sandbox/create", nil, &id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *ProcessSandbox) Destroy(ctx context.Context, id string) error {
	return s.cfg.Transport.Call(ctx, "sandbox/destroy", id, nil)
}

func (s *ProcessSandbox) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	var result ExecResult
	if err := s.cfg.Transport.Call(ctx, "sandbox/execute", map[string]any{
		"sandboxId": sandboxID,
		"request":   req,
	}, &result); err != nil {
		return ExecResult{}, err
	}
	return result, nil
}

func (s *ProcessSandbox) SetLimits(limits ResourceLimits) {
	_ = s.cfg.Transport.Call(context.Background(), "sandbox/setLimits", limits, nil)
}

func (s *ProcessSandbox) GetLimits() ResourceLimits {
	var limits ResourceLimits
	_ = s.cfg.Transport.Call(context.Background(), "sandbox/getLimits", nil, &limits)
	return limits
}
