package native_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/app"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/hook"
	"github.com/tltre/gogent/pkg/provider"
	"github.com/tltre/gogent/pkg/tool"
)

type integrationAgent struct {
	runtime *agentcore.AgentRuntime
	asked   *bool
}

// mockProvider is a minimal IProvider used to register a provider via
// app.WithProvider into the ProviderManager.
type mockProvider struct {
	resp      provider.Response
	modelInfo provider.ModelInfo
}

func (m *mockProvider) Generate(_ context.Context, _ []provider.ProviderMessage) (provider.Response, error) {
	return m.resp, nil
}

func (m *mockProvider) Stream(_ context.Context, _ []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 1)
	go func() {
		ch <- provider.StreamChunk{Delta: m.resp.Content, Done: true}
		close(ch)
	}()
	return ch, nil
}

func (m *mockProvider) ModelInfo() provider.ModelInfo {
	return m.modelInfo
}

func (a *integrationAgent) SetAgentRuntime(r *agentcore.AgentRuntime) {
	a.runtime = r
}

func (a *integrationAgent) Run(ctx context.Context, input agentcore.Input) (agentcore.Output, error) {
	*a.asked = true
	msgs := make([]provider.ProviderMessage, len(input.Messages))
	for i, m := range input.Messages {
		msgs[i] = provider.ProviderMessage{Role: m.Role, Content: m.Content}
	}
	prov := a.runtime.Reg().GetDefault(component.ComponentProvider)
	mgr, ok := prov.(*provider.ProviderManager)
	if !ok {
		return agentcore.Output{}, fmt.Errorf("default provider component is not ProviderManager")
	}
	impl := mgr.Get("test-provider")
	if impl == nil {
		return agentcore.Output{}, fmt.Errorf("provider test-provider not registered")
	}
	resp, err := impl.Generate(ctx, msgs)
	if err != nil {
		return agentcore.Output{}, err
	}
	return agentcore.Output{
		Response: agentcore.Message{
			Role:    "assistant",
			Content: resp.Content,
		},
		Actions: []agentcore.Action{
			{ToolName: "echo", Params: map[string]any{"input": "test"}, Result: "echoed"},
		},
	}, nil
}

func (a *integrationAgent) Stream(ctx context.Context, input agentcore.Input) (<-chan agentcore.Event, error) {
	ch := make(chan agentcore.Event, 1)
	go func() {
		defer close(ch)
		output, err := a.Run(ctx, input)
		if err != nil {
			ch <- agentcore.Event{Type: agentcore.EventError, Error: err}
			return
		}
		ch <- agentcore.Event{Type: agentcore.EventAfterRun, Payload: output}
	}()
	return ch, nil
}

func TestIntegration(t *testing.T) {
	asked := false
	agent := &integrationAgent{asked: &asked}

	prov := &mockProvider{
		resp:      provider.Response{Content: "Hello from provider", FinishReason: "stop"},
		modelInfo: provider.ModelInfo{Name: "test-model"},
	}

	mem := newMockMemory()
	bus := newMockEventBus()
	cm := newMockContextManager()

	tm := tool.NewToolManager("test-agent")
	// v0.12.2: No local ITool registration. Tool execution goes through daemon.
	_ = tm

	hm := hook.NewComponent("hooks-main")
	_ = hm.Register(&mockHook{
		name:   "logger",
		events: []hook.EventType{hook.EventBeforeRun},
	})

	builder := app.NewBuilderFromConfig(&app.Config{
		Name:       "test-agent",
		Components: nil,
		Defaults:   map[string]string{},
	})

	a, err := builder.Build(
		app.WithAgentCore("agent-main", agent),
		app.WithProvider("test-provider", prov),
		app.WithMemory("test-memory", mem),
		app.WithEventBus("test-eventbus", bus),
		app.WithContextManager("test-context", cm),
		app.WithComponent(hm),
	)
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}

	// Set ToolManager directly (v0.12.2: no longer a registry component).
	a.SetToolManager(tm)

	a.Registry().SetDefault(component.ComponentHook, "hooks-main")
	a.Registry().SetDefault(component.ComponentMemory, "test-memory")
	a.Registry().SetDefault(component.ComponentEventBus, "test-eventbus")
	a.Registry().SetDefault(component.ComponentContextManager, "test-context")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	agentComp := a.Registry().GetDefault(component.ComponentAgentCore).(*agentcore.AgentRuntime)
	reg := agentComp.Reg()

	if reg.GetDefault(component.ComponentProvider) == nil {
		t.Fatal("Provider is nil in registry")
	}
	if reg.GetDefault(component.ComponentHook) == nil {
		t.Fatal("HookManager is nil in registry")
	}

	input := agentcore.Input{
		Messages: []agentcore.Message{
			{Role: "user", Content: "hello"},
		},
	}

	output, err := agentComp.Run(ctx, input)
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !asked {
		t.Error("agent.Run() was not called")
	}
	if output.Response.Content != "Hello from provider" {
		t.Errorf("Run().Response.Content = %q, want %q",
			output.Response.Content, "Hello from provider")
	}
	if output.Response.Role != "assistant" {
		t.Errorf("Run().Response.Role = %q, want assistant", output.Response.Role)
	}
	// v0.12.2: tool execution goes through daemon, not available in native tests.

	if err := a.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
}
