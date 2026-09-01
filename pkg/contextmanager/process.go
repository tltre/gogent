package contextmanager

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/codes"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
	"github.com/tltre/gogent/internal/otel"
)

// ProcessContextManagerConfig holds the configuration for a gRPC-based ProcessContextManager.
type ProcessContextManagerConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessContextManager is a context manager implementation that communicates
// with a remote context manager service over gRPC.
type ProcessContextManager struct {
	cfg    *ProcessContextManagerConfig
	client gogentv1.ContextManagerServiceClient
}

// NewProcessContextManager creates a new ProcessContextManager. The gRPC
// client is lazily initialized on the first method call.
func NewProcessContextManager(cfg *ProcessContextManagerConfig) *ProcessContextManager {
	return &ProcessContextManager{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC ContextManagerServiceClient.
func (c *ProcessContextManager) getClient() (gogentv1.ContextManagerServiceClient, error) {
	if c.client != nil {
		return c.client, nil
	}
	conn, err := c.cfg.Pool.Get(c.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("contextmanager: get connection: %w", err)
	}
	c.client = gogentv1.NewContextManagerServiceClient(conn.ClientConn())
	return c.client, nil
}

// NewSession creates a new conversation session via gRPC.
func (c *ProcessContextManager) NewSession() string {
	tracer := otel.Tracer("gogent.contextmanager")
	ctx, span := tracer.Start(context.Background(), "context.new_session")
	defer span.End()

	client, err := c.getClient()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ""
	}
	resp, err := client.NewSession(ctx, &gogentv1.NewSessionRequest{})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ""
	}
	return resp.SessionId
}

// AddMessage appends a message to the session history via gRPC.
func (c *ProcessContextManager) AddMessage(sessionId string, msg ContextMessage) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	_, err = client.AddMessage(context.Background(), &gogentv1.AddMessageRequest{
		SessionId: sessionId,
		Message:   contextMessageToProto(msg),
	})
	return err
}

// GetMessages retrieves the full message history for a session via gRPC.
func (c *ProcessContextManager) GetMessages(sessionId string) []ContextMessage {
	client, err := c.getClient()
	if err != nil {
		return nil
	}
	resp, err := client.GetMessages(context.Background(), &gogentv1.GetMessagesRequest{
		SessionId: sessionId,
	})
	if err != nil {
		return nil
	}
	return contextMessagesFromProto(resp.Messages)
}

// GetSummary returns the current conversation summary via gRPC.
func (c *ProcessContextManager) GetSummary(sessionId string) (Summary, error) {
	client, err := c.getClient()
	if err != nil {
		return Summary{}, err
	}
	resp, err := client.GetSummary(context.Background(), &gogentv1.GetSummaryRequest{
		SessionId: sessionId,
	})
	if err != nil {
		return Summary{}, err
	}
	return summaryFromProto(resp.Summary), nil
}

// BuildSystemPrompt constructs a system prompt from history and summary via gRPC.
func (c *ProcessContextManager) BuildSystemPrompt(sessionId string) string {
	client, err := c.getClient()
	if err != nil {
		return ""
	}
	resp, err := client.BuildSystemPrompt(context.Background(), &gogentv1.BuildSystemPromptRequest{
		SessionId: sessionId,
	})
	if err != nil {
		return ""
	}
	return resp.SystemPrompt
}

// BuildInput assembles the complete model input. There is no remote BuildInput
// RPC yet, so this degrades to a local assembly (system prompt + history +
// user messages). Memory injection for the remote path is a follow-up. The
// remote (process) path does not implement context-window compression.
func (c *ProcessContextManager) BuildInput(ctx context.Context, sessionId string, messages []ContextMessage) []ContextMessage {
	input := make([]ContextMessage, 0, len(messages)+8)
	if sys := c.BuildSystemPrompt(sessionId); sys != "" {
		input = append(input, ContextMessage{Role: "system", Content: sys})
	}
	input = append(input, c.GetMessages(sessionId)...)
	input = append(input, messages...)
	return input
}

// Clear removes all messages from a session while keeping the session alive via gRPC.
func (c *ProcessContextManager) Clear(sessionId string) error {
	client, err := c.getClient()
	if err != nil {
		return err
	}
	_, err = client.Clear(context.Background(), &gogentv1.ContextClearRequest{
		SessionId: sessionId,
	})
	return err
}

// DeleteSession permanently removes a session via gRPC.
func (c *ProcessContextManager) DeleteSession(sessionId string) {
	client, err := c.getClient()
	if err != nil {
		return
	}
	_, _ = client.DeleteSession(context.Background(), &gogentv1.DeleteSessionRequest{
		SessionId: sessionId,
	})
}

// ListSessions returns all active session IDs via gRPC.
func (c *ProcessContextManager) ListSessions() []string {
	client, err := c.getClient()
	if err != nil {
		return nil
	}
	resp, err := client.ListSessions(context.Background(), &gogentv1.ListSessionsRequest{})
	if err != nil {
		return nil
	}
	return resp.SessionIds
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func contextMessageToProto(msg ContextMessage) *gogentv1.ContextMessage {
	cm := &gogentv1.ContextMessage{
		Role:    msg.Role,
		Content: msg.Content,
	}
	if !msg.Timestamp.IsZero() {
		cm.Timestamp = timestamppb.New(msg.Timestamp)
	}
	if len(msg.Extra) > 0 {
		s, err := structpb.NewStruct(msg.Extra)
		if err == nil {
			cm.Extra = s
		}
	}
	return cm
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func contextMessagesFromProto(msgs []*gogentv1.ContextMessage) []ContextMessage {
	if len(msgs) == 0 {
		return nil
	}
	result := make([]ContextMessage, len(msgs))
	for i, m := range msgs {
		result[i] = contextMessageFromProto(m)
	}
	return result
}

func contextMessageFromProto(m *gogentv1.ContextMessage) ContextMessage {
	msg := ContextMessage{
		Role:    m.Role,
		Content: m.Content,
	}
	if m.Timestamp != nil {
		msg.Timestamp = m.Timestamp.AsTime()
	}
	if m.Extra != nil {
		msg.Extra = m.Extra.AsMap()
	}
	return msg
}

func summaryFromProto(s *gogentv1.Summary) Summary {
	if s == nil {
		return Summary{}
	}
	sm := Summary{
		Content:   s.Content,
		KeyPoints: s.KeyPoints,
	}
	if s.Timestamp != nil {
		sm.Timestamp = s.Timestamp.AsTime()
	}
	return sm
}

// Ensure unused import is referenced if needed.
var _ = time.Time{}
