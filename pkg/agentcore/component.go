package agentcore

import (
	"context"
	"time"

	"github.com/tltre/gagent/pkg/component"
	"github.com/tltre/gagent/pkg/contextmanager"
	"github.com/tltre/gagent/pkg/eventbus"
	"github.com/tltre/gagent/pkg/hook"
	"github.com/tltre/gagent/pkg/logger"
	"github.com/tltre/gagent/pkg/memory"
	"github.com/tltre/gagent/pkg/provider"
	"github.com/tltre/gagent/pkg/sandbox"
	"github.com/tltre/gagent/pkg/tool"
)

type AgentRuntime struct {
	Name           string
	Agent          IAgentCore
	Provider       provider.IProvider
	ToolManager    *tool.ToolManager
	HookManager    *hook.HookManager
	ContextManager contextmanager.IContextManager
	Memory         memory.IMemory
	EventBus       eventbus.IEventBus
	Sandbox        sandbox.ISandbox
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

func (c *AgentRuntime) Initialize(ctx context.Context, registry *component.Registry) error {
	if c.Provider == nil {
		if p, ok := registry.GetDefault(component.ComponentProvider).(provider.IProvider); ok && p != nil {
			c.Provider = p
		}
	}
	if c.ToolManager == nil {
		if tm, ok := registry.GetDefault(component.ComponentTool).(*tool.ToolManager); ok && tm != nil {
			c.ToolManager = tm
		}
	}
	if c.HookManager == nil {
		if hm, ok := registry.GetDefault(component.ComponentHook).(*hook.HookManager); ok && hm != nil {
			c.HookManager = hm
		}
	}
	if c.ContextManager == nil {
		if cm, ok := registry.GetDefault(component.ComponentContextManager).(contextmanager.IContextManager); ok && cm != nil {
			c.ContextManager = cm
		}
	}
	if c.Memory == nil {
		if m, ok := registry.GetDefault(component.ComponentMemory).(memory.IMemory); ok && m != nil {
			c.Memory = m
		}
	}
	if c.EventBus == nil {
		if eb, ok := registry.GetDefault(component.ComponentEventBus).(eventbus.IEventBus); ok && eb != nil {
			c.EventBus = eb
		}
	}
	if c.Sandbox == nil {
		if sb, ok := registry.GetDefault(component.ComponentSandbox).(sandbox.ISandbox); ok && sb != nil {
			c.Sandbox = sb
		}
	}
	c.Agent.SetAgentRuntime(c)

	c.publishLog(ctx, logger.InfoLevel, "dependencies resolved",
		logger.Field{Key: "provider", Value: nameOrNil(c.Provider)},
		logger.Field{Key: "tool", Value: nameOrNil(c.ToolManager)},
	)
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

func (c *AgentRuntime) SetProvider(provider provider.IProvider) *AgentRuntime {
	c.Provider = provider
	return c
}

func (c *AgentRuntime) SetToolManager(toolManager *tool.ToolManager) *AgentRuntime {
	c.ToolManager = toolManager
	return c
}

func (c *AgentRuntime) SetHookManager(hookManager *hook.HookManager) *AgentRuntime {
	c.HookManager = hookManager
	return c
}

func (c *AgentRuntime) SetContextManager(contextManager contextmanager.IContextManager) *AgentRuntime {
	c.ContextManager = contextManager
	return c
}

func (c *AgentRuntime) SetMemory(memory memory.IMemory) *AgentRuntime {
	c.Memory = memory
	return c
}

func (c *AgentRuntime) SetEventBus(eventBus eventbus.IEventBus) *AgentRuntime {
	c.EventBus = eventBus
	return c
}

func (c *AgentRuntime) SetSandbox(sb sandbox.ISandbox) *AgentRuntime {
	c.Sandbox = sb
	return c
}

func (c *AgentRuntime) SetAgentCore(Agent IAgentCore) *AgentRuntime {
	c.Agent = Agent
	return c
}

func (c *AgentRuntime) publishLog(ctx context.Context, level logger.Level, msg string, fields ...logger.Field) {
	if c.EventBus == nil {
		return
	}
	ev := logger.LogEvent{
		TraceID: logger.TraceIDFromContext(ctx),
		Entry: logger.LogEntry{
			Timestamp: time.Now(),
			Level:     level,
			Module:    "agentcore",
			Message:   msg,
			Fields:    fields,
		},
	}
	c.EventBus.Publish(ctx, "system.log", eventbus.Event{
		Topic:   "system.log",
		Key:     "agentcore",
		Payload: ev,
	})
}

func nameOrNil(v any) string {
	if v == nil {
		return "nil"
	}
	type named interface{ GetName() string }
	if n, ok := v.(named); ok {
		return n.GetName()
	}
	return "set"
}
