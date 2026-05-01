package hook

import (
	"context"

	"github.com/tltre/gagent/internal/client"
)

type ProcessHookConfig struct {
	Name      string
	Events    []EventType
	Transport client.Transport
}

type ProcessHook struct {
	cfg *ProcessHookConfig
	tr  *client.LazyTransport
}

func NewProcessHook(cfg *ProcessHookConfig) *ProcessHook {
	return &ProcessHook{
		cfg: cfg,
		tr:  client.WrapLazy(cfg.Transport),
	}
}

func (h *ProcessHook) OnEvent(ctx context.Context, event Event) (context.Context, error) {
	if err := h.tr.Call(ctx, "hook/onEvent", event, nil); err != nil {
		return ctx, err
	}
	return ctx, nil
}

func (h *ProcessHook) Events() []EventType {
	return h.cfg.Events
}
