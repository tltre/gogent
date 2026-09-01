package provider

import (
	"context"
	"net/http"
	"testing"
)

func TestDeepSeekEngineRegistered(t *testing.T) {
	if !IsEngineRegistered("deepseek") {
		t.Fatal("deepseek engine not registered via init()")
	}
}

func TestDeepSeekGenerate(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{
		"choices": [{"message": {"content": "DeepSeek reply"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
	}`)
	// Mock /models so the dynamic default resolves to deepseek-chat.
	m.models = `{
		"object": "list",
		"data": [
			{"id": "deepseek-chat", "object": "model"},
			{"id": "deepseek-reasoner", "object": "model"}
		]
	}`

	p := NewDeepSeek(DeepSeekConfig{APIKey: "ds-key"})
	// Point at the mock server for wire checks.
	p.OpenAIProvider.baseURL = m.server.URL

	resp, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if resp.Content != "DeepSeek reply" {
		t.Errorf("Content = %q, want DeepSeek reply", resp.Content)
	}
	if m.lastAuth != "Bearer ds-key" {
		t.Errorf("auth = %q, want Bearer ds-key", m.lastAuth)
	}
	if m.lastBody["model"] != "deepseek-chat" {
		t.Errorf("model = %v, want deepseek-chat", m.lastBody["model"])
	}
}

func TestDeepSeekAPIKeyFromEnv(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)

	t.Setenv(DeepSeekEnvAPIKey, "env-ds-key")
	p := NewDeepSeek(DeepSeekConfig{})
	p.OpenAIProvider.baseURL = m.server.URL

	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if m.lastAuth != "Bearer env-ds-key" {
		t.Errorf("auth = %q, want Bearer env-ds-key", m.lastAuth)
	}
}

func TestDeepSeekModelOverride(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)

	p := NewDeepSeek(DeepSeekConfig{APIKey: "k", Model: "deepseek-reasoner"})
	p.OpenAIProvider.baseURL = m.server.URL

	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if m.lastBody["model"] != "deepseek-reasoner" {
		t.Errorf("model = %v, want deepseek-reasoner", m.lastBody["model"])
	}
}

func TestDeepSeekModelInfo(t *testing.T) {
	// Keep the model-catalog lookup offline/deterministic (disabled → 0).
	t.Setenv(ModelsDevEnvDisable, "1")
	m := newMockChatServer(t, http.StatusOK, `{}`)
	m.models = `{
		"object": "list",
		"data": [
			{"id": "deepseek-chat", "object": "model"},
			{"id": "deepseek-reasoner", "object": "model"}
		]
	}`
	p := NewDeepSeek(DeepSeekConfig{APIKey: "k"})
	p.baseURL = m.server.URL

	info := p.ModelInfo()
	if info.Provider != "deepseek" || info.DisplayName != "DeepSeek" {
		t.Errorf("ModelInfo = %+v, want provider=deepseek display=DeepSeek", info)
	}
	if info.SupportsTool != true {
		t.Errorf("SupportsTool = %v, want true", info.SupportsTool)
	}
	if info.SupportsVision != false {
		t.Errorf("SupportsVision = %v, want false", info.SupportsVision)
	}
	// Models fetched dynamically from the mock /models endpoint.
	if len(info.Models) != 2 || info.Models[0] != "deepseek-chat" || info.Models[1] != "deepseek-reasoner" {
		t.Errorf("Models = %v, want [deepseek-chat deepseek-reasoner] from mock /models", info.Models)
	}
	// ContextSize comes from the centralized catalog; disabled → 0 (unknown).
	if info.ContextSize != 0 {
		t.Errorf("ContextSize = %d, want 0 (catalog disabled)", info.ContextSize)
	}
}

func TestDeepSeekDefaultModelFromEnv(t *testing.T) {
	t.Setenv(DeepSeekEnvModel, "deepseek-reasoner")
	p := NewDeepSeek(DeepSeekConfig{})
	if p.model != "deepseek-reasoner" {
		t.Errorf("model = %q, want deepseek-reasoner from env", p.model)
	}
}
