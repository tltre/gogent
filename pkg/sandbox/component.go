package sandbox

import (
	"context"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/logger"
)

type Component struct {
	component.BasicComponent
	sandbox ISandbox
	limits  ResourceLimits
}

func NewComponent(name string, sandbox ISandbox, limits ResourceLimits) *Component {
	return &Component{
		BasicComponent: component.NewBasicComponent(name),
		sandbox:        sandbox,
		limits:         limits,
	}
}

func (c *Component) GetType() component.ComponentType {
	return component.ComponentSandbox
}

func (c *Component) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	c.sandbox.SetLimits(c.limits)
	c.log(ctx, logger.InfoLevel, "sandbox initialized")
	return nil
}

func (c *Component) Start(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "sandbox started")
	return nil
}

func (c *Component) Stop(ctx context.Context) error {
	c.log(ctx, logger.DebugLevel, "sandbox stopped")
	return nil
}

func (c *Component) Dependencies() map[string]component.DependencySpec {
	return nil
}

func (c *Component) Create(ctx context.Context) (string, error) {
	id, err := c.sandbox.Create(ctx)
	if err != nil {
		return id, err
	}
	c.log(ctx, logger.DebugLevel, "sandbox created", logger.Field{Key: "id", Value: id})
	return id, nil
}

func (c *Component) Destroy(ctx context.Context, id string) error {
	c.log(ctx, logger.DebugLevel, "sandbox destroy", logger.Field{Key: "id", Value: id})
	return c.sandbox.Destroy(ctx, id)
}

func (c *Component) Execute(ctx context.Context, sandboxID string, req ExecRequest) (ExecResult, error) {
	c.log(ctx, logger.DebugLevel, "sandbox execute started",
		logger.Field{Key: "sandbox_id", Value: sandboxID},
		logger.Field{Key: "language", Value: req.Language},
	)
	start := time.Now()
	result, err := c.sandbox.Execute(ctx, sandboxID, req)
	dur := time.Since(start)
	if err != nil {
		c.log(ctx, logger.ErrorLevel, "sandbox execute failed",
			logger.Field{Key: "sandbox_id", Value: sandboxID},
			logger.Field{Key: "error", Value: err.Error()},
			logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		)
		return result, err
	}
	if result.Error != nil {
		c.log(ctx, logger.ErrorLevel, "sandbox execute error",
			logger.Field{Key: "sandbox_id", Value: sandboxID},
			logger.Field{Key: "exit_code", Value: result.ExitCode},
			logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		)
		return result, nil
	}
	c.log(ctx, logger.InfoLevel, "sandbox execute completed",
		logger.Field{Key: "sandbox_id", Value: sandboxID},
		logger.Field{Key: "exit_code", Value: result.ExitCode},
		logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
	)
	return result, nil
}

func (c *Component) SetLimits(limits ResourceLimits) {
	c.limits = limits
	c.sandbox.SetLimits(limits)
}

func (c *Component) GetLimits() ResourceLimits {
	return c.sandbox.GetLimits()
}

func (c *Component) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	r := c.Registry()
	if r == nil {
		return
	}
	lc := r.GetDefault(component.ComponentLogger)
	if lc == nil {
		return
	}
	l, ok := lc.(logger.Logger)
	if !ok {
		return
	}
	l.Log(ctx, logger.LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Module:    c.GetName(),
		Message:   msg,
		Fields:    fields,
	})
}
