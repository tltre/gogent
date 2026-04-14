package agentcore

import (
	"context"

	"github.com/yourorg/gagent/pkg/component"
)

type Component struct {
	name        string
	agent       Agent
	provider    any
	toolManager any
	hookManager any
	contextMgr  any
	memory      any
	eventBus    any
}

func NewComponent(name string) *Component {
	return &Component{
		name: name,
	}
}

func (c *Component) Name() string {
	return c.name
}

func (c *Component) Type() component.ComponentType {
	return component.ComponentAgentCore
}

func (c *Component) Initialize(ctx context.Context, deps component.Dependencies) error {
	if c.provider == nil {
		if p := deps.GetDefault(component.ComponentProvider); p != nil {
			c.provider = p
		}
	}
	if c.toolManager == nil {
		if tm := deps.GetDefault(component.ComponentTool); tm != nil {
			c.toolManager = tm
		}
	}
	if c.hookManager == nil {
		if hm := deps.GetDefault(component.ComponentHook); hm != nil {
			c.hookManager = hm
		}
	}
	if c.contextMgr == nil {
		if cm := deps.GetDefault(component.ComponentContextManager); cm != nil {
			c.contextMgr = cm
		}
	}
	if c.memory == nil {
		if m := deps.GetDefault(component.ComponentMemory); m != nil {
			c.memory = m
		}
	}
	if c.eventBus == nil {
		if eb := deps.GetDefault(component.ComponentEventBus); eb != nil {
			c.eventBus = eb
		}
	}
	return nil
}

func (c *Component) Start(ctx context.Context) error {
	return nil
}

func (c *Component) Stop(ctx context.Context) error {
	return nil
}

func (c *Component) Dependencies() map[string]component.DependencySpec {
	return map[string]component.DependencySpec{
		"provider": {
			Type:     component.ComponentProvider,
			Required: true,
		},
		"toolManager": {
			Type:     component.ComponentTool,
			Required: false,
		},
		"hookManager": {
			Type:     component.ComponentHook,
			Required: false,
		},
		"contextManager": {
			Type:     component.ComponentContextManager,
			Required: false,
		},
		"memory": {
			Type:     component.ComponentMemory,
			Required: false,
		},
		"eventBus": {
			Type:     component.ComponentEventBus,
			Required: false,
		},
	}
}

func (c *Component) Run(ctx context.Context, input Input) (Output, error) {
	if c.agent == nil {
		return Output{}, nil
	}
	return c.agent.Run(ctx, input)
}

func (c *Component) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	if c.agent == nil {
		ch := make(chan Event)
		close(ch)
		return ch, nil
	}
	return c.agent.Stream(ctx, input)
}

func (c *Component) SetProvider(provider any) *Component {
	c.provider = provider
	return c
}

func (c *Component) SetToolManager(toolManager any) *Component {
	c.toolManager = toolManager
	return c
}

func (c *Component) SetHookManager(hookManager any) *Component {
	c.hookManager = hookManager
	return c
}

func (c *Component) SetContextManager(contextManager any) *Component {
	c.contextMgr = contextManager
	return c
}

func (c *Component) SetMemory(memory any) *Component {
	c.memory = memory
	return c
}

func (c *Component) SetEventBus(eventBus any) *Component {
	c.eventBus = eventBus
	return c
}

func (c *Component) SetAgent(agent Agent) *Component {
	c.agent = agent
	return c
}
