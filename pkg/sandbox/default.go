package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

type DefaultSandbox struct {
	mu     sync.RWMutex
	limits ResourceLimits
	active map[string]*exec.Cmd
}

func NewDefaultSandbox(limits ResourceLimits) *DefaultSandbox {
	return &DefaultSandbox{
		limits: limits,
		active: make(map[string]*exec.Cmd),
	}
}

func (s *DefaultSandbox) Create(_ context.Context) (string, error) {
	id := fmt.Sprintf("sb-%x", rand.Uint64())
	s.mu.Lock()
	s.active[id] = nil
	s.mu.Unlock()
	return id, nil
}

func (s *DefaultSandbox) Destroy(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cmd, ok := s.active[id]; ok && cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
	delete(s.active, id)
	return nil
}

func (s *DefaultSandbox) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	s.mu.RLock()
	limits := s.limits
	s.mu.RUnlock()

	if len(limits.AllowedCommands) > 0 {
		executable := strings.Fields(req.Code)[0]
		allowed := false
		for _, c := range limits.AllowedCommands {
			if c == executable || strings.HasPrefix(req.Code, c+" ") || req.Code == c {
				allowed = true
				break
			}
		}
		if !allowed {
			return ExecResult{
				Stdout:   "",
				Stderr:   fmt.Sprintf("command not allowed: %s", executable),
				ExitCode: -1,
				Error:    fmt.Errorf("command not allowed: %s", executable),
			}, nil
		}
	}

	timeout := limits.MaxCPUTime
	if req.Timeout > 0 {
		timeout = req.Timeout
	}

	execCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	var shellCmd *exec.Cmd
	switch {
	case req.Language == "sh" || req.Language == "bash":
		if runtime.GOOS == "windows" {
			shellCmd = exec.CommandContext(execCtx, "cmd", "/c", req.Code)
		} else {
			shellCmd = exec.CommandContext(execCtx, "sh", "-c", req.Code)
		}
	default:
		parts := strings.Fields(req.Code)
		if len(parts) == 0 {
			return ExecResult{ExitCode: -1, Error: fmt.Errorf("empty command")}, nil
		}
		if len(parts) > 1 {
			shellCmd = exec.CommandContext(execCtx, parts[0], parts[1:]...)
		} else {
			shellCmd = exec.CommandContext(execCtx, parts[0])
		}
	}

	shellCmd.Env = make([]string, 0, len(req.Env))
	for k, v := range req.Env {
		shellCmd.Env = append(shellCmd.Env, k+"="+v)
	}

	s.mu.Lock()
	s.active[sandboxID] = shellCmd
	s.mu.Unlock()

	var stdout, stderr bytes.Buffer
	shellCmd.Stdout = &stdout
	shellCmd.Stderr = &stderr

	err := shellCmd.Run()

	s.mu.Lock()
	delete(s.active, sandboxID)
	s.mu.Unlock()

	result := ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
			result.Error = err
		}
	}
	return result, nil
}

func (s *DefaultSandbox) SetLimits(limits ResourceLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = limits
}

func (s *DefaultSandbox) GetLimits() ResourceLimits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.limits
}

func (s *DefaultSandbox) Health(ctx context.Context) error { return nil }
