package hook

import "context"

type NativeHook struct {
	name    string
	events  []EventType
	handler func(ctx context.Context, event Event) (context.Context, error)
}

func NewNativeHook(name string, events []EventType, handler func(ctx context.Context, event Event) (context.Context, error)) *NativeHook {
	return &NativeHook{
		name:    name,
		events:  events,
		handler: handler,
	}
}

func (h *NativeHook) OnEvent(ctx context.Context, event Event) (context.Context, error) {
	if h.handler != nil {
		return h.handler(ctx, event)
	}
	return ctx, nil
}

func (h *NativeHook) Events() []EventType {
	return h.events
}
