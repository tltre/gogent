package agentcore

import (
	"context"
	"fmt"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/provider"
)

// ReactAgent is the default IAgentCore implementation for the "react" agent
// type (v0.14.7). It routes each run to the provider selected in the context
// via the unified ProviderManager.
//
// Current scope: single-turn chat routing — the provider is dispatched from
// Input.ProviderName / Input.ModelName (carried through the context), the
// response is returned as Output. The full ReAct tool-calling loop
// (tool_calls → execute → iterate) is a follow-up enhancement that builds on
// this routing core.
type ReactAgent struct {
	runtime *AgentRuntime
	manager *provider.ProviderManager
}

var _ IAgentCore = (*ReactAgent)(nil)

// NewReactAgent creates a ReactAgent. The ProviderManager is resolved lazily
// from the Registry in SetAgentRuntime.
func NewReactAgent() *ReactAgent {
	return &ReactAgent{}
}

// init registers the "react" agent type into the agent type registry.
func init() {
	_ = RegisterAgentType("react", func() IAgentCore {
		return NewReactAgent()
	})
}

// SetAgentRuntime stores the runtime reference and resolves the
// ProviderManager from the Registry.
func (a *ReactAgent) SetAgentRuntime(runtime *AgentRuntime) {
	a.runtime = runtime
	if comp := runtime.Reg().GetDefault(component.ComponentProvider); comp != nil {
		if mgr, ok := comp.(*provider.ProviderManager); ok {
			a.manager = mgr
		}
	}
}

// Run dispatches a single-turn generation to the selected provider and
// returns the response as Output.
func (a *ReactAgent) Run(ctx context.Context, input Input) (Output, error) {
	if a.manager == nil {
		return Output{}, fmt.Errorf("react agent: no ProviderManager available")
	}

	ctx = provider.WithProviderName(ctx, input.ProviderName)
	ctx = provider.WithModel(ctx, input.ModelName)

	resp, err := a.manager.Generate(ctx, toProviderMessages(input.Messages))
	if err != nil {
		return Output{}, err
	}

	return Output{
		Response: Message{
			Role:    "assistant",
			Content: resp.Content,
		},
		Metadata: map[string]any{
			"provider":  provider.ProviderNameFrom(ctx),
			"model":     provider.ModelFrom(ctx),
			"finish":    resp.FinishReason,
			"toolCalls": len(resp.ToolCalls),
		},
	}, nil
}

// Stream dispatches a streaming generation to the selected provider and
// converts provider chunks into agent events.
func (a *ReactAgent) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	if a.manager == nil {
		ch := make(chan Event, 1)
		ch <- Event{Type: EventError, Error: fmt.Errorf("react agent: no ProviderManager available")}
		close(ch)
		return ch, nil
	}

	ctx = provider.WithProviderName(ctx, input.ProviderName)
	ctx = provider.WithModel(ctx, input.ModelName)

	stream, err := a.manager.Stream(ctx, toProviderMessages(input.Messages))
	if err != nil {
		ch := make(chan Event, 1)
		ch <- Event{Type: EventError, Error: err}
		close(ch)
		return ch, nil
	}

	ch := make(chan Event, 100)
	go func() {
		defer close(ch)
		var content string
		for chunk := range stream {
			content += chunk.Delta
			ch <- Event{Type: EventStreaming, Payload: chunk.Delta}
			if chunk.Done {
				break
			}
		}
		ch <- Event{
			Type: EventAfterRun,
			Payload: Output{
				Response: Message{Role: "assistant", Content: content},
			},
		}
	}()
	return ch, nil
}

// toProviderMessages converts agentcore messages to provider messages.
func toProviderMessages(msgs []Message) []provider.ProviderMessage {
	result := make([]provider.ProviderMessage, 0, len(msgs))
	for _, m := range msgs {
		pm := provider.ProviderMessage{Role: m.Role, Content: m.Content}
		for _, t := range m.Tools {
			pm.Tools = append(pm.Tools, provider.ToolDefinition{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			})
		}
		result = append(result, pm)
	}
	return result
}
