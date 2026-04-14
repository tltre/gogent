package sandbox

import (
	"context"
	"time"
)

type ResourceLimits struct {
	MaxMemoryMB     int
	MaxCPUTime      time.Duration
	MaxFileSize     int64
	NetworkAccess   bool
	AllowedSyscalls []string
	MaxProcesses    int
}

type ExecRequest struct {
	Code     string
	Language string
	Timeout  time.Duration
	Files    map[string][]byte
	Env      map[string]string
}

type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Error    error
	Duration time.Duration
}

type Sandbox interface {
	Create(ctx context.Context) (string, error)
	Destroy(ctx context.Context, id string) error
	Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error)
	SetLimits(limits ResourceLimits)
	GetLimits() ResourceLimits
}

type Executor interface {
	Execute(ctx context.Context, code string, lang string, limits ResourceLimits) (ExecResult, error)
}
