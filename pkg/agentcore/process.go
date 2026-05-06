package agentcore

import (
	"context"

	"github.com/tltre/gogent/internal/client"
)

type ProcessAgentCoreConfig struct {
	Name      string
	Transport client.Transport
}

type ProcessAgentCore struct {
	tr *client.LazyTransport
}

func NewProcessAgentCore(cfg *ProcessAgentCoreConfig) *ProcessAgentCore {
	return &ProcessAgentCore{tr: client.WrapLazy(cfg.Transport)}
}

func (a *ProcessAgentCore) Run(ctx context.Context, input Input) (Output, error) {
	var result Output
	if err := a.tr.Call(ctx, "agent/run", input, &result); err != nil {
		return Output{}, err
	}
	return result, nil
}

func (a *ProcessAgentCore) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	ch := make(chan Event, 100)
	go func() {
		defer close(ch)
		result, err := a.Run(ctx, input)
		_ = err
		ch <- Event{Type: EventAfterRun, Payload: result}
	}()
	return ch, nil
}

func (a *ProcessAgentCore) SetAgentRuntime(runtime *AgentRuntime) {
}
