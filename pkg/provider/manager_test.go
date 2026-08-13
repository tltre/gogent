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
