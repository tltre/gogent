package main

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// RegisterAll registers all 9 stub gRPC services on the given server.
// A single binary covers every component type, so the daemon's --type
// flag is effectively ignored.
func RegisterAll(srv *grpc.Server) {
	gogentv1.RegisterProviderServiceServer(srv, &stubProvider{})
	gogentv1.RegisterAgentCoreServiceServer(srv, &stubAgentCore{})
	gogentv1.RegisterMemoryServiceServer(srv, &stubMemory{})
	gogentv1.RegisterToolServiceServer(srv, &stubTool{})
	gogentv1.RegisterHookServiceServer(srv, &stubHook{})
	gogentv1.RegisterEventBusServiceServer(srv, &stubEventBus{})
	gogentv1.RegisterContextManagerServiceServer(srv, &stubContextManager{})
	gogentv1.RegisterSandboxServiceServer(srv, &stubSandbox{})
	gogentv1.RegisterChannelServiceServer(srv, &stubChannel{})
}

// ── Provider ─────────────────────────────────────────────────────────────

type stubProvider struct{ gogentv1.UnimplementedProviderServiceServer }

func (s *stubProvider) Generate(_ context.Context, _ *gogentv1.GenerateRequest) (*gogentv1.GenerateResponse, error) {
	return &gogentv1.GenerateResponse{Content: "Hello from stub!", FinishReason: "stop"}, nil
}

func (s *stubProvider) ModelInfo(_ context.Context, _ *gogentv1.ModelInfoRequest) (*gogentv1.ModelInfoResponse, error) {
	return &gogentv1.ModelInfoResponse{ModelInfo: &gogentv1.ModelInfo{Name: "stub", Provider: "gogent-e2e", ContextSize: 4096, SupportsTool: true}}, nil
}

// ── AgentCore ────────────────────────────────────────────────────────────

type stubAgentCore struct{ gogentv1.UnimplementedAgentCoreServiceServer }

func (s *stubAgentCore) Run(_ context.Context, _ *gogentv1.RunRequest) (*gogentv1.RunResponse, error) {
	return &gogentv1.RunResponse{Output: &gogentv1.AgentOutput{
		Response: &gogentv1.Message{Role: "assistant", Content: "Hello from stub agent!"},
	}}, nil
}

func (s *stubAgentCore) Stream(req *gogentv1.RunRequest, stream grpc.ServerStreamingServer[gogentv1.AgentEvent]) error {
	resp, _ := s.Run(context.Background(), req)
	payload, _ := structpb.NewValue(resp.Output.Response.Content)
	return stream.Send(&gogentv1.AgentEvent{Type: gogentv1.EventType_EVENT_TYPE_AFTER_RUN, Payload: payload})
}

// ── Memory ───────────────────────────────────────────────────────────────

type stubMemory struct{ gogentv1.UnimplementedMemoryServiceServer }

func (s *stubMemory) Query(_ context.Context, _ *gogentv1.QueryRequest) (*gogentv1.QueryResponse, error) {
	return &gogentv1.QueryResponse{Items: []*gogentv1.MemoryItem{}}, nil
}
func (s *stubMemory) Add(_ context.Context, _ *gogentv1.AddEntryRequest) (*gogentv1.AddEntryResponse, error) {
	return &gogentv1.AddEntryResponse{Id: "stub-id"}, nil
}
func (s *stubMemory) Clear(_ context.Context, _ *gogentv1.MemoryClearRequest) (*gogentv1.MemoryClearResponse, error) {
	return &gogentv1.MemoryClearResponse{}, nil
}

// ── Tool ─────────────────────────────────────────────────────────────────

type stubTool struct{ gogentv1.UnimplementedToolServiceServer }

func (s *stubTool) ExecuteTool(stream gogentv1.ToolService_ExecuteToolServer) error {
	// Wait for ToolExecuteRequest
	msg, err := stream.Recv()
	if err != nil {
		return err
	}
	req := msg.GetRequest()
	if req == nil {
		return nil
	}
	// Send auth OK
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: 1,
		Event: &gogentv1.ToolExecutionEvent_Auth{
			Auth: &gogentv1.AuthResult{Passed: true, EffectiveLevel: 0},
		},
	})
	// Send result
	output, _ := structpb.NewValue("stub output")
	stream.Send(&gogentv1.ToolExecutionEvent{
		Sequence: 2,
		Event: &gogentv1.ToolExecutionEvent_Result{
			Result: &gogentv1.ToolResult{Output: output, IsError: false},
		},
	})
	return nil
}

// ── Hook ─────────────────────────────────────────────────────────────────

type stubHook struct{ gogentv1.UnimplementedHookServiceServer }

func (s *stubHook) OnEvent(_ context.Context, _ *gogentv1.HookEventRequest) (*gogentv1.HookEventResponse, error) {
	return &gogentv1.HookEventResponse{}, nil
}

// ── EventBus ─────────────────────────────────────────────────────────────

type stubEventBus struct{ gogentv1.UnimplementedEventBusServiceServer }

func (s *stubEventBus) Publish(_ context.Context, _ *gogentv1.PublishRequest) (*gogentv1.PublishResponse, error) {
	return &gogentv1.PublishResponse{}, nil
}
func (s *stubEventBus) Subscribe(_ *gogentv1.SubscribeRequest, stream grpc.ServerStreamingServer[gogentv1.Event]) error {
	return nil
}
func (s *stubEventBus) Unsubscribe(_ context.Context, _ *gogentv1.UnsubscribeRequest) (*gogentv1.UnsubscribeResponse, error) {
	return &gogentv1.UnsubscribeResponse{}, nil
}

// ── ContextManager ───────────────────────────────────────────────────────

type stubContextManager struct{ gogentv1.UnimplementedContextManagerServiceServer }

func (s *stubContextManager) NewSession(_ context.Context, _ *gogentv1.NewSessionRequest) (*gogentv1.NewSessionResponse, error) {
	return &gogentv1.NewSessionResponse{SessionId: "stub-session"}, nil
}
func (s *stubContextManager) AddMessage(_ context.Context, _ *gogentv1.AddMessageRequest) (*gogentv1.AddMessageResponse, error) {
	return &gogentv1.AddMessageResponse{}, nil
}
func (s *stubContextManager) GetMessages(_ context.Context, _ *gogentv1.GetMessagesRequest) (*gogentv1.GetMessagesResponse, error) {
	return &gogentv1.GetMessagesResponse{Messages: []*gogentv1.ContextMessage{}}, nil
}
func (s *stubContextManager) GetSummary(_ context.Context, _ *gogentv1.GetSummaryRequest) (*gogentv1.GetSummaryResponse, error) {
	return &gogentv1.GetSummaryResponse{Summary: &gogentv1.Summary{Content: "stub summary"}}, nil
}
func (s *stubContextManager) BuildSystemPrompt(_ context.Context, _ *gogentv1.BuildSystemPromptRequest) (*gogentv1.BuildSystemPromptResponse, error) {
	return &gogentv1.BuildSystemPromptResponse{SystemPrompt: "You are a stub."}, nil
}
func (s *stubContextManager) Clear(_ context.Context, _ *gogentv1.ContextClearRequest) (*gogentv1.ContextClearResponse, error) {
	return &gogentv1.ContextClearResponse{}, nil
}
func (s *stubContextManager) DeleteSession(_ context.Context, _ *gogentv1.DeleteSessionRequest) (*gogentv1.DeleteSessionResponse, error) {
	return &gogentv1.DeleteSessionResponse{}, nil
}
func (s *stubContextManager) ListSessions(_ context.Context, _ *gogentv1.ListSessionsRequest) (*gogentv1.ListSessionsResponse, error) {
	return &gogentv1.ListSessionsResponse{SessionIds: []string{"stub-session"}}, nil
}

// ── Sandbox ──────────────────────────────────────────────────────────────

type stubSandbox struct{ gogentv1.UnimplementedSandboxServiceServer }

func (s *stubSandbox) Create(_ context.Context, _ *gogentv1.CreateRequest) (*gogentv1.CreateResponse, error) {
	return &gogentv1.CreateResponse{SandboxId: "stub-sandbox"}, nil
}
func (s *stubSandbox) Execute(_ context.Context, _ *gogentv1.ExecuteRequest) (*gogentv1.ExecuteResponse, error) {
	return &gogentv1.ExecuteResponse{Stdout: "stub output", Stderr: "", ExitCode: 0}, nil
}
func (s *stubSandbox) Destroy(_ context.Context, _ *gogentv1.DestroyRequest) (*gogentv1.DestroyResponse, error) {
	return &gogentv1.DestroyResponse{}, nil
}

// ── Channel ──────────────────────────────────────────────────────────────

type stubChannel struct{ gogentv1.UnimplementedChannelServiceServer }

func (s *stubChannel) Send(_ context.Context, _ *gogentv1.SendRequest) (*gogentv1.SendResponse, error) {
	return &gogentv1.SendResponse{}, nil
}
func (s *stubChannel) Receive(_ *gogentv1.ReceiveRequest, stream grpc.ServerStreamingServer[gogentv1.ChannelMessage]) error {
	return nil
}
