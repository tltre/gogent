package sandbox

import (
	"context"
	"sync"

	"github.com/tltre/gagent/pkg/component"
)

type Component struct {
	name    string
	sandbox ISandbox
	limits  ResourceLimits
}

func NewComponent(name string, sandbox ISandbox, limits ResourceLimits) *Component {
	return &Component{
		name:    name,
		sandbox: sandbox,
		limits:  limits,
	}
}

func (c *Component) GetName() string {
	return c.name
}

func (c *Component) GetType() component.ComponentType {
	return component.ComponentSandbox
}

func (c *Component) Initialize(ctx context.Context, deps *component.Registry) error {
	c.sandbox.SetLimits(c.limits)
	return nil
}

func (c *Component) Start(ctx context.Context) error {
	return nil
}

func (c *Component) Stop(ctx context.Context) error {
	return nil
}

func (c *Component) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *Component) Create(ctx context.Context) (string, error) {
	return c.sandbox.Create(ctx)
}

func (c *Component) Destroy(ctx context.Context, id string) error {
	return c.sandbox.Destroy(ctx, id)
}

func (c *Component) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	return c.sandbox.Execute(ctx, sandboxID, req)
}

func (c *Component) SetLimits(limits ResourceLimits) {
	c.limits = limits
	c.sandbox.SetLimits(limits)
}

func (c *Component) GetLimits() ResourceLimits {
	return c.sandbox.GetLimits()
}

type NativeSandbox struct {
	mu     sync.RWMutex
	limits ResourceLimits
}

func NewNativeSandbox(limits ResourceLimits) *NativeSandbox {
	return &NativeSandbox{
		limits: limits,
	}
}

func (s *NativeSandbox) Create(ctx context.Context) (string, error) {
	return "native-default", nil
}

func (s *NativeSandbox) Destroy(ctx context.Context, id string) error {
	return nil
}

func (s *NativeSandbox) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	return ExecResult{}, nil
}

func (s *NativeSandbox) SetLimits(limits ResourceLimits) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = limits
}

func (s *NativeSandbox) GetLimits() ResourceLimits {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.limits
}
