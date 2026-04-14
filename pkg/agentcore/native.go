package agentcore

import (
	"context"
)

type NativeAgent struct {
	name        string
	provider    any
	toolManager any
	hookManager any
	contextMgr  any
	memory      any
	eventBus    any
}

func NewNativeAgent(name string) *NativeAgent {
	return &NativeAgent{
		name: name,
	}
}

func (a *NativeAgent) Run(ctx context.Context, input Input) (Output, error) {
	return Output{}, nil
}

func (a *NativeAgent) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	ch := make(chan Event)
	go func() {
		defer close(ch)
	}()
	return ch, nil
}

func (a *NativeAgent) SetProvider(provider any) *NativeAgent {
	a.provider = provider
	return a
}

func (a *NativeAgent) SetToolManager(toolManager any) *NativeAgent {
	a.toolManager = toolManager
	return a
}

func (a *NativeAgent) SetHookManager(hookManager any) *NativeAgent {
	a.hookManager = hookManager
	return a
}

func (a *NativeAgent) SetContextManager(contextManager any) *NativeAgent {
	a.contextMgr = contextManager
	return a
}

func (a *NativeAgent) SetMemory(memory any) *NativeAgent {
	a.memory = memory
	return a
}

func (a *NativeAgent) SetEventBus(eventBus any) *NativeAgent {
	a.eventBus = eventBus
	return a
}
