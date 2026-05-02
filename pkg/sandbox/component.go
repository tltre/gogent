package sandbox

import (
	"context"

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
