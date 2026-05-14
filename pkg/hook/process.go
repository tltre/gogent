package hook

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ProcessHookConfig holds the configuration for a gRPC-based ProcessHook.
type ProcessHookConfig struct {
	Name   string
	Events []EventType
	Pool   *grpctransport.Pool
	Target string
}

// ProcessHook is a hook implementation that communicates with a remote
// hook service over gRPC.
type ProcessHook struct {
	cfg    *ProcessHookConfig
	client gogentv1.HookServiceClient
}

// NewProcessHook creates a new ProcessHook. The gRPC client is lazily
// initialized on the first method call.
func NewProcessHook(cfg *ProcessHookConfig) *ProcessHook {
	return &ProcessHook{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC HookServiceClient.
func (h *ProcessHook) getClient() (gogentv1.HookServiceClient, error) {
	if h.client != nil {
		return h.client, nil
	}
	conn, err := h.cfg.Pool.Get(h.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("hook: get connection: %w", err)
	}
	h.client = gogentv1.NewHookServiceClient(conn.ClientConn())
	return h.client, nil
}

// OnEvent sends a hook event to the remote hook service via gRPC.
func (h *ProcessHook) OnEvent(ctx context.Context, event Event) (context.Context, error) {
	client, err := h.getClient()
	if err != nil {
		return ctx, err
	}

	var payloadStruct *structpb.Struct
	if m, ok := event.Payload.(map[string]any); ok && m != nil {
		s, err := structpb.NewStruct(m)
		if err == nil {
			payloadStruct = s
		}
	}

	var errStr string
	if event.Error != nil {
		errStr = event.Error.Error()
	}

	req := &gogentv1.HookEventRequest{
		Type:    hookEventTypeToProto(event.Type),
		Name:    event.Name,
		Payload: payloadStruct,
		Error:   errStr,
	}

	resp, err := client.OnEvent(ctx, req)
	if err != nil {
		return ctx, err
	}
	if resp.Error != "" {
		return ctx, fmt.Errorf("hook: %s", resp.Error)
	}
	return ctx, nil
}

// Events returns the event types this hook handles.
func (h *ProcessHook) Events() []EventType {
	return h.cfg.Events
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func hookEventTypeToProto(t EventType) gogentv1.HookEventType {
	// Proto enum values are offset by +1 (0 = UNSPECIFIED).
	return gogentv1.HookEventType(int32(t) + 1)
}
