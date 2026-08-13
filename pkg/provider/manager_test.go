package provider

import (
	"context"
	"errors"
	"testing"
)

type mockProvider struct {
	modelInfo ModelInfo
}

func (m *mockProvider) Generate(_ context.Context, _ []ProviderMessage) (Response, error) {
	return Response{Content: "mock"}, nil
}

func (m *mockProvider) Stream(_ context.Context, _ []ProviderMessage) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Delta: "mock", Done: true}
	close(ch)
	return ch, nil
}

func (m *mockProvider) ModelInfo() ModelInfo {
	return m.modelInfo
}

func TestProviderManagerRegisterGet(t *testing.T) {
	m := NewManagerComponent("provider-manager")

	if err := m.Register("openai", &mockProvider{modelInfo: ModelInfo{Provider: "openai"}}); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := m.Register("gemini", &mockProvider{modelInfo: ModelInfo{Provider: "gemini"}}); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	if got := m.Get("openai"); got == nil {
		t.Fatal("Get(openai) = nil, want provider")
	}
	if got := m.Get("nope"); got != nil {
		t.Fatalf("Get(nope) = %v, want nil", got)
	}
}

func TestProviderManagerRegisterDuplicate(t *testing.T) {
	m := NewManagerComponent("provider-manager")

	if err := m.Register("openai", &mockProvider{}); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	err := m.Register("openai", &mockProvider{})
	if !errors.Is(err, ErrProviderAlreadyExists) {
		t.Fatalf("Register(duplicate) = %v, want ErrProviderAlreadyExists", err)
	}
}

func TestProviderManagerUnregister(t *testing.T) {
	m := NewManagerComponent("provider-manager")

	if err := m.Register("openai", &mockProvider{}); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := m.Unregister("openai"); err != nil {
		t.Fatalf("Unregister() = %v", err)
	}
	if got := m.Get("openai"); got != nil {
		t.Fatalf("Get(openai) after unregister = %v, want nil", got)
	}

	err := m.Unregister("openai")
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Unregister(missing) = %v, want ErrProviderNotFound", err)
	}
}

func TestProviderManagerList(t *testing.T) {
	m := NewManagerComponent("provider-manager")

	if err := m.Register("gemini", &mockProvider{modelInfo: ModelInfo{
		Provider: "gemini", DisplayName: "Google Gemini", Models: []string{"gemini-2.0-flash"},
	}}); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := m.Register("openai", &mockProvider{modelInfo: ModelInfo{
		Provider: "openai", DisplayName: "OpenAI", Models: []string{"gpt-4o", "gpt-4o-mini"},
	}}); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	infos := m.List()
	if len(infos) != 2 {
		t.Fatalf("List() len = %d, want 2", len(infos))
	}
	// sorted by name: gemini, openai
	if infos[0].Name != "gemini" || infos[1].Name != "openai" {
		t.Fatalf("List() order = %v, want [gemini openai]", infos)
	}
	if infos[1].DisplayName != "OpenAI" {
		t.Errorf("List()[openai].DisplayName = %q, want OpenAI", infos[1].DisplayName)
	}
	if len(infos[1].Models) != 2 || infos[1].Models[0] != "gpt-4o" {
		t.Errorf("List()[openai].Models = %v, want [gpt-4o gpt-4o-mini]", infos[1].Models)
	}
}

func TestProviderManagerDisplayNameFallback(t *testing.T) {
	m := NewManagerComponent("provider-manager")

	// DisplayName empty → falls back to Provider (engine name).
	if err := m.Register("openai", &mockProvider{modelInfo: ModelInfo{Provider: "openai"}}); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	infos := m.List()
	if len(infos) != 1 {
		t.Fatalf("List() len = %d, want 1", len(infos))
	}
	if infos[0].DisplayName != "openai" {
		t.Errorf("DisplayName = %q, want fallback to provider name", infos[0].DisplayName)
	}
}

func TestProviderManagerComponentLifecycle(t *testing.T) {
	m := NewManagerComponent("provider-manager")
	ctx := context.Background()

	if got := m.GetName(); got != "provider-manager" {
		t.Errorf("GetName() = %q, want provider-manager", got)
	}
	if got := m.GetType(); got != "provider" {
		t.Errorf("GetType() = %q, want provider", got)
	}

	if err := m.Initialize(ctx, nil); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start() = %v", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if got := m.Dependencies(); got != nil {
		t.Fatalf("Dependencies() = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// Dispatch (v0.14.7)
// ---------------------------------------------------------------------------

// dispatchMock records the ctx it received and the messages.
type dispatchMock struct {
	name     string
	lastCtx  context.Context
	messages []ProviderMessage
}

func (d *dispatchMock) Generate(ctx context.Context, msgs []ProviderMessage) (Response, error) {
	d.lastCtx = ctx
	d.messages = msgs
	return Response{Content: "dispatch-" + d.name}, nil
}

func (d *dispatchMock) Stream(ctx context.Context, msgs []ProviderMessage) (<-chan StreamChunk, error) {
	d.lastCtx = ctx
	d.messages = msgs
	ch := make(chan StreamChunk, 1)
	ch <- StreamChunk{Delta: "dispatch-" + d.name, Done: true}
	close(ch)
	return ch, nil
}

func (d *dispatchMock) ModelInfo() ModelInfo {
	return ModelInfo{Name: "m-" + d.name, Provider: d.name, DisplayName: d.name}
}

func TestProviderManagerDispatchByCtx(t *testing.T) {
	m := NewManagerComponent("provider-manager")
	openaiMock := &dispatchMock{name: "openai"}
	dsMock := &dispatchMock{name: "deepseek"}
	if err := m.Register("openai", openaiMock); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	if err := m.Register("deepseek", dsMock); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	ctx := WithProviderName(context.Background(), "deepseek")
	resp, err := m.Generate(ctx, []ProviderMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if resp.Content != "dispatch-deepseek" {
		t.Errorf("Content = %q, want dispatch-deepseek", resp.Content)
	}
	if len(openaiMock.messages) != 0 {
		t.Error("Generate routed to openai, want deepseek")
	}
	if got := ProviderNameFrom(dsMock.lastCtx); got != "deepseek" {
		t.Errorf("provider name passed through ctx = %q, want deepseek", got)
	}
}

func TestProviderManagerDispatchDefaultFirst(t *testing.T) {
	m := NewManagerComponent("provider-manager")
	dsMock := &dispatchMock{name: "deepseek"} // alphabetical first
	if err := m.Register("deepseek", dsMock); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	// No provider name in ctx → first registered.
	resp, err := m.Generate(context.Background(), nil)
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if resp.Content != "dispatch-deepseek" {
		t.Errorf("Content = %q, want dispatch-deepseek (default)", resp.Content)
	}
}

func TestProviderManagerDispatchUnknown(t *testing.T) {
	m := NewManagerComponent("provider-manager")
	if err := m.Register("openai", &dispatchMock{name: "openai"}); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	ctx := WithProviderName(context.Background(), "nope")
	_, err := m.Generate(ctx, nil)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Generate(unknown) err = %v, want ErrProviderNotFound", err)
	}
}

func TestProviderManagerDispatchNoProviders(t *testing.T) {
	m := NewManagerComponent("provider-manager")
	_, err := m.Generate(context.Background(), nil)
	if !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("Generate(empty) err = %v, want ErrProviderNotFound", err)
	}
}

func TestProviderManagerDispatchStream(t *testing.T) {
	m := NewManagerComponent("provider-manager")
	openaiMock := &dispatchMock{name: "openai"}
	if err := m.Register("openai", openaiMock); err != nil {
		t.Fatalf("Register() = %v", err)
	}

	ctx := WithProviderName(context.Background(), "openai")
	ch, err := m.Stream(ctx, []ProviderMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}
	chunk := <-ch
	if chunk.Delta != "dispatch-openai" {
		t.Errorf("chunk.Delta = %q, want dispatch-openai", chunk.Delta)
	}
}

// ---------------------------------------------------------------------------
// Context keys
// ---------------------------------------------------------------------------

func TestCtxProviderAndModel(t *testing.T) {
	ctx := context.Background()
	if got := ProviderNameFrom(ctx); got != "" {
		t.Errorf("ProviderNameFrom(empty) = %q, want empty", got)
	}
	if got := ModelFrom(ctx); got != "" {
		t.Errorf("ModelFrom(empty) = %q, want empty", got)
	}

	ctx = WithProviderName(ctx, "openai")
	ctx = WithModel(ctx, "gpt-4o-mini")
	if got := ProviderNameFrom(ctx); got != "openai" {
		t.Errorf("ProviderNameFrom = %q, want openai", got)
	}
	if got := ModelFrom(ctx); got != "gpt-4o-mini" {
		t.Errorf("ModelFrom = %q, want gpt-4o-mini", got)
	}
}
