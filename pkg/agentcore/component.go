package agentcore

import (
	"context"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/eventbus"
	"github.com/tltre/gagent/pkg/logger"
)

type AgentRuntime struct {
	Name  string
	Agent IAgentCore
	reg   *component.Registry
}

func NewComponent(name string, agent IAgentCore) *AgentRuntime {
	return &AgentRuntime{
		Name:  name,
		Agent: agent,
	}
}

func (c *AgentRuntime) GetName() string {
	return c.Name
}

func (c *AgentRuntime) GetType() component.ComponentType {
	return component.ComponentAgentCore
}

func (c *AgentRuntime) Reg() *component.Registry {
	return c.reg
}

func (c *AgentRuntime) Initialize(ctx context.Context, registry *component.Registry) error {
	c.reg = registry
	c.Agent.SetAgentRuntime(c)
	c.publishLog(ctx, logger.InfoLevel, "dependencies resolved")
	return nil
}

func (c *AgentRuntime) Start(ctx context.Context) error {
	c.publishLog(ctx, logger.InfoLevel, "agent started")
	return nil
}

func (c *AgentRuntime) Stop(ctx context.Context) error {
	c.publishLog(ctx, logger.InfoLevel, "agent stopped")
	return nil
}

func (c *AgentRuntime) Dependencies() map[string]component.DependencySpec {
	return map[string]component.DependencySpec{
		"provider": {
			Type:     component.ComponentProvider,
			Required: true,
		},
		"toolManager": {
			Type:     component.ComponentTool,
			Required: false,
		},
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
	c.publishLog(ctx, logger.DebugLevel, "run started",
		logger.Field{Key: "messages", Value: len(input.Messages)},
	)

	start := time.Now()
	output, err := c.Agent.Run(ctx, input)
	dur := time.Since(start)

	if err != nil {
		c.publishLog(ctx, logger.ErrorLevel, "run failed",
			logger.Field{Key: "error", Value: err.Error()},
			logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		)
		return output, err
	}

	traceID := logger.TraceIDFromContext(ctx)
	c.publishLog(ctx, logger.InfoLevel, "run completed",
		logger.Field{Key: "dur_ms", Value: dur.Milliseconds()},
		logger.Field{Key: "traceId", Value: traceID},
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

func (c *AgentRuntime) publishLog(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	bus := c.reg.GetDefault(component.ComponentEventBus)
	if bus == nil {
		return
	}
	eb, ok := bus.(eventbus.IEventBus)
	if !ok {
		return
	}
	eb.Publish(ctx, "system.log", eventbus.Event{
		Topic: "system.log",
		Key:   "agentcore",
		Payload: logger.LogEvent{
			TraceID: logger.TraceIDFromContext(ctx),
			Entry: logger.LogEntry{
				Timestamp: time.Now(),
				Level:     level,
				Module:    "agentcore",
				Message:   msg,
				Fields:    fields,
			},
		},
	})
}
