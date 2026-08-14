package agentcore

import (
	"context"
	"time"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/logger"
	"github.com/tltre/gogent/pkg/tool"
)

type AgentRuntime struct {
	component.BasicComponent
	Agent IAgentCore

	// toolService is the framework-managed tool-calling surface injected by
	// the Builder (v0.15.1). It exposes only List/Execute — lifecycle is
	// owned by App via the ToolManager. Any IAgentCore may access it.
	toolService tool.Service
}

func NewComponent(name string, agent IAgentCore) *AgentRuntime {
	return &AgentRuntime{
		BasicComponent: component.NewBasicComponent(name),
		Agent:          agent,
	}
}

func (c *AgentRuntime) GetType() component.ComponentType {
	return component.ComponentAgentCore
}

func (c *AgentRuntime) Reg() *component.Registry {
	return c.Registry()
}

// SetToolService injects the tool-calling surface (called by Builder after
// ToolManager creation; the concrete *ToolManager satisfies tool.Service).
func (c *AgentRuntime) SetToolService(s tool.Service) {
	c.toolService = s
}

// ToolService returns the tool-calling surface available to agents, or nil
// when no tool manager is configured.
func (c *AgentRuntime) ToolService() tool.Service {
	return c.toolService
}

func (c *AgentRuntime) Initialize(ctx context.Context, registry *component.Registry) error {
	c.SetRegistry(registry)
	c.Agent.SetAgentRuntime(c)
	c.log(ctx, logger.InfoLevel, "dependencies resolved")
	return nil
}

func (c *AgentRuntime) Start(ctx context.Context) error {
	c.log(ctx, logger.InfoLevel, "agent started")
	return nil
}

func (c *AgentRuntime) Stop(ctx context.Context) error {
	c.log(ctx, logger.InfoLevel, "agent stopped")
	return nil
}

func (c *AgentRuntime) Dependencies() map[string]component.DependencySpec {
	return map[string]component.DependencySpec{
		"provider": {
			Type:     component.ComponentProvider,
			Required: true,
		},
		// toolManager removed in v0.12.2 — no longer a registry component
		"HookManager": {
			Type:     component.ComponentHook,
			Required: false,
		},
		"contextManager": {
			Type:     component.ComponentContextManager,
			Required: false,
		},
		"Memory": {
			Type:     component.ComponentMemory,
			Required: false,
		},
		"EventBus": {
			Type:     component.ComponentEventBus,
			Required: false,
		},
		"Sandbox": {
			Type:     component.ComponentSandbox,
			Required: false,
		},
	}
}

func (c *AgentRuntime) Run(ctx context.Context, input Input) (Output, error) {
	if c.Agent == nil {
		return Output{}, nil
	}
	ctx = logger.WithTraceID(ctx)
	c.log(ctx, logger.DebugLevel, "run started",
		logger.Field{Key: "messages", Value: len(input.Messages)},
	)

	start := time.Now()
	output, err := c.Agent.Run(ctx, input)
	dur := time.Since(start)

	if err != nil {
		c.log(ctx, logger.ErrorLevel, "run failed",
			logger.Field{Key: "error", Value: err.Error()},
			logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		)
		return output, err
	}

	c.log(ctx, logger.InfoLevel, "run completed",
		logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
	)
	return output, nil
}

func (c *AgentRuntime) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	if c.Agent == nil {
		ch := make(chan Event)
		close(ch)
		return ch, nil
	}
	return c.Agent.Stream(ctx, input)
}

func (c *AgentRuntime) SetAgentCore(Agent IAgentCore) *AgentRuntime {
	c.Agent = Agent
	return c
}

func (c *AgentRuntime) log(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
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
