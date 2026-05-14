package agentcore

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport"
	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ProcessAgentCoreConfig holds the configuration for a gRPC-based ProcessAgentCore.
type ProcessAgentCoreConfig struct {
	Name   string
	Pool   *grpctransport.Pool
	Target string
}

// ProcessAgentCore is an agent core implementation that communicates with a
// remote agent core service over gRPC.
type ProcessAgentCore struct {
	cfg    *ProcessAgentCoreConfig
	client gogentv1.AgentCoreServiceClient
}

// NewProcessAgentCore creates a new ProcessAgentCore. The gRPC client is
// lazily initialized on the first method call.
func NewProcessAgentCore(cfg *ProcessAgentCoreConfig) *ProcessAgentCore {
	return &ProcessAgentCore{cfg: cfg}
}

// getClient lazily initializes and returns the gRPC AgentCoreServiceClient.
func (a *ProcessAgentCore) getClient() (gogentv1.AgentCoreServiceClient, error) {
	if a.client != nil {
		return a.client, nil
	}
	conn, err := a.cfg.Pool.Get(a.cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("agentcore: get connection: %w", err)
	}
	a.client = gogentv1.NewAgentCoreServiceClient(conn.ClientConn())
	return a.client, nil
}

// Run executes the agent with the given input via gRPC and returns the final output.
func (a *ProcessAgentCore) Run(ctx context.Context, input Input) (Output, error) {
	client, err := a.getClient()
	if err != nil {
		return Output{}, err
	}

	resp, err := client.Run(ctx, &gogentv1.RunRequest{
		Input: inputToProto(input),
	})
	if err != nil {
		return Output{}, err
	}
	return outputFromProto(resp.Output), nil
}

// Stream executes the agent and streams execution events back to the caller.
func (a *ProcessAgentCore) Stream(ctx context.Context, input Input) (<-chan Event, error) {
	client, err := a.getClient()
	if err != nil {
		return nil, err
	}

	stream, err := client.Stream(ctx, &gogentv1.RunRequest{
		Input: inputToProto(input),
	})
	if err != nil {
		return nil, fmt.Errorf("agentcore: stream: %w", err)
	}

	ch := make(chan Event, 100)
	go readStream(stream, ch, ctx)
	return ch, nil
}

// SetAgentRuntime stores the runtime reference. Not applicable for process clients.
func (a *ProcessAgentCore) SetAgentRuntime(runtime *AgentRuntime) {
}

// readStream reads AgentEvents from the gRPC stream and sends them on the channel.
func readStream(stream grpc.ServerStreamingClient[gogentv1.AgentEvent], ch chan<- Event, ctx context.Context) {
	defer close(ch)
	for {
		evt, err := stream.Recv()
		if err != nil {
			return
		}
		select {
		case ch <- agentEventToEvent(evt):
		case <-ctx.Done():
			return
		}
	}
}

// --- conversion helpers: Go domain types → gRPC proto types ---

func inputToProto(input Input) *gogentv1.AgentInput {
	ai := &gogentv1.AgentInput{
		Messages: messagesToProto(input.Messages),
	}
	if len(input.Context) > 0 {
		s, err := structpb.NewStruct(input.Context)
		if err == nil {
			ai.Context = s
		}
	}
	return ai
}

func messagesToProto(msgs []Message) []*gogentv1.Message {
	result := make([]*gogentv1.Message, len(msgs))
	for i, m := range msgs {
		result[i] = &gogentv1.Message{
			Role:    m.Role,
			Content: m.Content,
			Tools:   toolInfosToProto(m.Tools),
		}
	}
	return result
}

func toolInfosToProto(tools []ToolInfo) []*gogentv1.AgentToolInfo {
	if len(tools) == 0 {
		return nil
	}
	result := make([]*gogentv1.AgentToolInfo, len(tools))
	for i, t := range tools {
		params, _ := structpb.NewStruct(t.Parameters)
		result[i] = &gogentv1.AgentToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		}
	}
	return result
}

// --- conversion helpers: gRPC proto types → Go domain types ---

func outputFromProto(out *gogentv1.AgentOutput) Output {
	if out == nil {
		return Output{}
	}
	o := Output{
		Response: messageFromProto(out.Response),
		Actions:  actionsFromProto(out.Actions),
	}
	if out.Metadata != nil {
		o.Metadata = out.Metadata.AsMap()
	}
	return o
}

func messageFromProto(m *gogentv1.Message) Message {
	if m == nil {
		return Message{}
	}
	return Message{
		Role:    m.Role,
		Content: m.Content,
		Tools:   toolInfosFromProto(m.Tools),
	}
}

func toolInfosFromProto(tools []*gogentv1.AgentToolInfo) []ToolInfo {
	if len(tools) == 0 {
		return nil
	}
	result := make([]ToolInfo, len(tools))
	for i, t := range tools {
		ti := ToolInfo{
			Name:        t.Name,
			Description: t.Description,
		}
		if t.Parameters != nil {
			ti.Parameters = t.Parameters.AsMap()
		}
		result[i] = ti
	}
	return result
}

func actionsFromProto(actions []*gogentv1.Action) []Action {
	if len(actions) == 0 {
		return nil
	}
	result := make([]Action, len(actions))
	for i, a := range actions {
		act := Action{
			ToolName: a.ToolName,
		}
		if a.Params != nil {
			act.Params = a.Params.AsMap()
		}
		if a.Result != nil {
			act.Result = a.Result.AsInterface()
		}
		result[i] = act
	}
	return result
}

func agentEventToEvent(evt *gogentv1.AgentEvent) Event {
	e := Event{
		Type: eventTypeFromProto(evt.Type),
	}
	if evt.Payload != nil {
		e.Payload = evt.Payload.AsInterface()
	}
	// Error is a string in proto; convert to Go error if non-empty.
	if evt.Error != "" {
		e.Error = fmt.Errorf("%s", evt.Error)
	}
	return e
}

func eventTypeFromProto(t gogentv1.EventType) EventType {
	// Proto enum values are offset by +1 (0 = UNSPECIFIED).
	return EventType(int32(t) - 1)
}
