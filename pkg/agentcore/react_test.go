package agentcore

import (
	"context"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/provider"
)

// reactMockProvider records the messages/model it received and returns a
// canned response.
type reactMockProvider struct {
	name     string
	lastCtx  context.Context
	messages []provider.ProviderMessage
}

func (p *reactMockProvider) Generate(ctx context.Context, msgs []provider.ProviderMessage) (provider.Response, error) {
	p.lastCtx = ctx
	p.messages = msgs
	return provider.Response{Content: "reply-" + p.name, FinishReason: "stop"}, nil
}

func (p *reactMockProvider) Stream(ctx context.Context, msgs []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	p.lastCtx = ctx
	p.messages = msgs
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Delta: "reply-" + p.name, Done: true}
	close(ch)
	return ch, nil
}

func (p *reactMockProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{
		Name:        "default-" + p.name,
		Provider:    p.name,
		DisplayName: p.name,
		Models:      []string{"default-" + p.name},
	}
}

// newReactTestSetup builds a registry with a ProviderManager (two mock
// providers) and an AgentRuntime wrapping a ReactAgent.
func newReactTestSetup(t *testing.T) (*AgentRuntime, *reactMockProvider, *reactMockProvider) {
	t.Helper()

	openaiMock := &reactMockProvider{name: "openai"}
	dsMock := &reactMockProvider{name: "deepseek"}

	mgr := provider.NewManagerComponent("provider-manager")
	if err := mgr.Register("openai", openaiMock); err != nil {
		t.Fatalf("Register(openai) = %v", err)
	}
	if err := mgr.Register("deepseek", dsMock); err != nil {
		t.Fatalf("Register(deepseek) = %v", err)
	}

	reg := component.NewRegistry()
	if err := reg.Register(mgr); err != nil {
		t.Fatalf("Register manager = %v", err)
	}
	reg.SetDefault(component.ComponentProvider, mgr.GetName())

	runtime := NewComponent("agent-main", NewReactAgent())
	if err := runtime.Initialize(context.Background(), reg); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	return runtime, openaiMock, dsMock
}

func TestReactAgentRoutesByProvider(t *testing.T) {
	runtime, openaiMock, dsMock := newReactTestSetup(t)

	// Explicit provider via Input.ProviderName.
	out, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "hi"}},
		ProviderName: "deepseek",
		ModelName:    "default-deepseek",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if out.Response.Content != "reply-deepseek" {
		t.Errorf("Response.Content = %q, want reply-deepseek", out.Response.Content)
	}
	if len(dsMock.messages) != 1 || len(openaiMock.messages) != 0 {
		t.Fatalf("dispatch wrong: deepseek msgs=%d openai msgs=%d", len(dsMock.messages), len(openaiMock.messages))
	}

	// Provider name reached the manager via ctx.
	if got := provider.ProviderNameFrom(dsMock.lastCtx); got != "deepseek" {
		t.Errorf("ctx provider = %q, want deepseek", got)
	}
	if got := provider.ModelFrom(dsMock.lastCtx); got != "default-deepseek" {
		t.Errorf("ctx model = %q, want default-deepseek", got)
	}
}

func TestReactAgentDefaultsToFirstProvider(t *testing.T) {
	runtime, openaiMock, _ := newReactTestSetup(t)

	// Empty provider name → first registered (alphabetical: deepseek).
	out, err := runtime.Run(context.Background(), Input{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if len(openaiMock.messages) != 0 {
		t.Errorf("default dispatch should not hit openai (first is deepseek)")
	}
	if out.Response.Content != "reply-deepseek" {
		t.Errorf("Response.Content = %q, want reply-deepseek (default)", out.Response.Content)
	}
}

func TestReactAgentUnknownProvider(t *testing.T) {
	runtime, _, _ := newReactTestSetup(t)

	_, err := runtime.Run(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "hi"}},
		ProviderName: "nope",
	})
	if err == nil {
		t.Fatal("Run() with unknown provider = nil error, want error")
	}
}

func TestReactAgentStream(t *testing.T) {
	runtime, _, _ := newReactTestSetup(t)

	ch, err := runtime.Stream(context.Background(), Input{
		Messages:     []Message{{Role: "user", Content: "hi"}},
		ProviderName: "deepseek",
	})
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}

	var events []Event
	for evt := range ch {
		events = append(events, evt)
	}
	// Expect EventStreaming + EventAfterRun.
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 (streaming + after-run)", len(events))
	}
	if events[0].Type != EventStreaming {
		t.Errorf("events[0].Type = %v, want EventStreaming", events[0].Type)
	}
	after, ok := events[1].Payload.(Output)
	if !ok || after.Response.Content != "reply-deepseek" {
		t.Errorf("after-run payload = %+v, want Output with reply-deepseek", events[1].Payload)
	}
}

func TestReactAgentNoManager(t *testing.T) {
	// ReactAgent without a ProviderManager in the registry.
	runtime := NewComponent("agent-main", NewReactAgent())
	reg := component.NewRegistry()
	if err := runtime.Initialize(context.Background(), reg); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	_, err := runtime.Run(context.Background(), Input{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("Run() without manager = nil error, want error")
	}
}

func TestReactAgentRegisteredType(t *testing.T) {
	if !isAgentTypeRegistered("react") {
		t.Fatal("react agent type not registered via init()")
	}
	agent, err := CreateAgent("react")
	if err != nil {
		t.Fatalf("CreateAgent(react) = %v", err)
	}
	if agent == nil {
		t.Fatal("CreateAgent(react) = nil")
	}
	if _, err := CreateAgent("nope"); err == nil {
		t.Error("CreateAgent(nope) = nil error, want ErrUnknownAgentType")
	}
}

func isAgentTypeRegistered(name string) bool {
	for _, n := range RegisteredAgentTypes() {
		if n == name {
			return true
		}
	}
	return false
}
