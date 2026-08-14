package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockChatServer is a configurable httptest server that records the last
// request it received and returns a canned response. It serves both
// /chat/completions (canned body) and /models (canned model list).
type mockChatServer struct {
	server    *httptest.Server
	lastPath  string
	lastAuth  string
	lastBody  map[string]any
	status    int
	body      string
	models    string   // response for GET /models; defaults to a small list
	checkBody func(t *testing.T, body map[string]any)
}

func newMockChatServer(t *testing.T, status int, body string) *mockChatServer {
	m := &mockChatServer{status: status, body: body, models: `{
		"object": "list",
		"data": [
			{"id": "gpt-4o", "object": "model", "owned_by": "openai"},
			{"id": "gpt-4o-mini", "object": "model", "owned_by": "openai"}
		]
	}`}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.lastPath = r.URL.Path
		m.lastAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(m.status)
			_, _ = w.Write([]byte(m.models))
			return
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&m.lastBody)
			if m.checkBody != nil {
				m.checkBody(t, m.lastBody)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.status)
		_, _ = w.Write([]byte(m.body))
	}))
	t.Cleanup(m.server.Close)
	return m
}

func newTestOpenAI(m *mockChatServer) *OpenAIProvider {
	return NewOpenAI(OpenAIConfig{
		BaseURL: m.server.URL,
		APIKey:  "test-key",
	})
}

func TestOpenAIEngineRegistered(t *testing.T) {
	if !IsEngineRegistered("openai") {
		t.Fatal("openai engine not registered via init()")
	}
}

func TestOpenAIGenerate(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{
		"choices": [{"message": {"content": "Hello from OpenAI"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`)
	p := newTestOpenAI(m)

	resp, err := p.Generate(context.Background(), []ProviderMessage{
		{Role: "user", Content: "hi"},
	})
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if resp.Content != "Hello from OpenAI" {
		t.Errorf("Content = %q, want Hello from OpenAI", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.Usage.TotalTokens)
	}

	// Wire checks.
	if m.lastPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", m.lastPath)
	}
	if m.lastAuth != "Bearer test-key" {
		t.Errorf("auth = %q, want Bearer test-key", m.lastAuth)
	}
	if m.lastBody == nil {
		t.Fatal("request body not captured")
	}
	if m.lastBody["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o", m.lastBody["model"])
	}
	if stream, ok := m.lastBody["stream"].(bool); !ok || stream {
		t.Errorf("stream = %v, want false", m.lastBody["stream"])
	}
}

func TestOpenAIGenerateToolCall(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{
		"choices": [{
			"message": {
				"content": "",
				"tool_calls": [{
					"id": "call_123",
					"function": {"name": "get_weather", "arguments": "{\"city\":\"Tokyo\"}"}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 8, "completion_tokens": 4, "total_tokens": 12}
	}`)
	p := newTestOpenAI(m)

	resp, err := p.Generate(context.Background(), []ProviderMessage{
		{Role: "user", Content: "weather in Tokyo"},
	})
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_123" || tc.Name != "get_weather" {
		t.Errorf("ToolCall = %+v, want id=call_123 name=get_weather", tc)
	}
	if city, ok := tc.Args["city"].(string); !ok || city != "Tokyo" {
		t.Errorf("ToolCall.Args = %v, want city=Tokyo", tc.Args)
	}
}

func TestOpenAIGenerateToolsInRequest(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	m.checkBody = func(t *testing.T, body map[string]any) {
		tools, ok := body["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("tools = %v, want 1 entry", body["tools"])
		}
		tool := tools[0].(map[string]any)
		if tool["type"] != "function" {
			t.Errorf("tool.type = %v, want function", tool["type"])
		}
	}
	p := newTestOpenAI(m)

	_, err := p.Generate(context.Background(), []ProviderMessage{
		{Role: "user", Content: "hi", Tools: []ToolDefinition{
			{Name: "calc", Description: "calculate", Parameters: map[string]any{"type": "object"}},
		}},
	})
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
}

func TestOpenAIStream(t *testing.T) {
	// SSE payload: two content deltas, then [DONE].
	chunks := []string{
		`data: {"choices":[{"delta":{"content":"Hel"},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}
	body := strings.Join(chunks, "\n") + "\n"

	m := newMockChatServer(t, http.StatusOK, body)
	m.checkBody = func(t *testing.T, b map[string]any) {
		if b["stream"] != true {
			t.Errorf("stream = %v, want true", b["stream"])
		}
	}
	p := newTestOpenAI(m)

	ch, err := p.Stream(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}

	var got []StreamChunk
	timeout := time.After(3 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				goto done
			}
			got = append(got, c)
		case <-timeout:
			t.Fatal("stream timed out")
		}
	}
done:

	if len(got) != 2 {
		t.Fatalf("chunks = %d, want 2 (%+v)", len(got), got)
	}
	if got[0].Delta != "Hel" {
		t.Errorf("chunk[0].Delta = %q, want Hel (raw delta)", got[0].Delta)
	}
	if got[1].Delta != "lo" {
		t.Errorf("chunk[1].Delta = %q, want lo (raw delta — caller accumulates)", got[1].Delta)
	}
	if !got[1].Done {
		t.Error("chunk[1].Done = false, want true")
	}
}

func TestOpenAIStreamToolCall(t *testing.T) {
	// Tool-call arguments split across two SSE events, finish with tool_calls.
	chunks := []string{
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_x","function":{"name":"get_weather","arguments":"{\"city\":"}}]},"finish_reason":null}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}
	body := strings.Join(chunks, "\n") + "\n"

	m := newMockChatServer(t, http.StatusOK, body)
	p := newTestOpenAI(m)

	ch, err := p.Stream(context.Background(), []ProviderMessage{{Role: "user", Content: "weather"}})
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}

	var last StreamChunk
	timeout := time.After(3 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				goto done
			}
			last = c
		case <-timeout:
			t.Fatal("stream timed out")
		}
	}
done:

	if last.ToolCall == nil {
		t.Fatal("final chunk has no ToolCall")
	}
	if last.ToolCall.Name != "get_weather" {
		t.Errorf("ToolCall.Name = %q, want get_weather", last.ToolCall.Name)
	}
	if city, ok := last.ToolCall.Args["city"].(string); !ok || city != "Tokyo" {
		t.Errorf("ToolCall.Args = %v, want city=Tokyo", last.ToolCall.Args)
	}
	if !last.Done {
		t.Error("final chunk Done = false, want true")
	}
}

func TestOpenAICompatBaseURL(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{"choices":[{"message":{"content":"compat"},"finish_reason":"stop"}]}`)
	p := NewOpenAICompat(m.server.URL)
	p.apiKey = "compat-key"

	resp, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if resp.Content != "compat" {
		t.Errorf("Content = %q, want compat", resp.Content)
	}
}

func TestOpenAIErrorMapping(t *testing.T) {
	m := newMockChatServer(t, http.StatusTooManyRequests, `{
		"error": {"message": "Rate limit reached", "type": "rate_limit_error", "code": "rate_limited"}
	}`)
	p := newTestOpenAI(m)

	_, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("Generate() = nil error, want ProviderError")
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if pe.Code != http.StatusTooManyRequests {
		t.Errorf("Code = %d, want 429", pe.Code)
	}
	if pe.Type != "rate_limit_error" {
		t.Errorf("Type = %q, want rate_limit_error", pe.Type)
	}
	if !pe.Retryable {
		t.Error("Retryable = false, want true (429)")
	}
	if !IsRetryable(err) {
		t.Error("IsRetryable = false, want true")
	}
}

func TestOpenAIErrorServer500(t *testing.T) {
	m := newMockChatServer(t, http.StatusInternalServerError, `{"error":{"message":"boom","type":"server_error"}}`)
	p := newTestOpenAI(m)

	_, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}})
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *ProviderError", err)
	}
	if !pe.Retryable {
		t.Error("Retryable = false, want true (5xx)")
	}
}

func TestOpenAIAPIKeyMissing(t *testing.T) {
	p := NewOpenAI(OpenAIConfig{}) // no key configured, env unset
	_, err := p.Generate(context.Background(), nil)
	if !errors.Is(err, ErrAPIKeyMissing) {
		t.Fatalf("Generate() err = %v, want ErrAPIKeyMissing", err)
	}
}

func TestOpenAIModelInfo(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{}`)
	p := newTestOpenAI(m)

	info := p.ModelInfo()
	if info.Provider != "openai" || info.DisplayName != "OpenAI" {
		t.Errorf("ModelInfo = %+v, want provider=openai display=OpenAI", info)
	}
	if !info.SupportsTool || !info.SupportsVision {
		t.Errorf("ModelInfo.SupportsTool/Vision = %v/%v, want true/true", info.SupportsTool, info.SupportsVision)
	}
	// Models come from the dynamic /models endpoint (mock), sorted.
	if len(info.Models) != 2 || info.Models[0] != "gpt-4o" || info.Models[1] != "gpt-4o-mini" {
		t.Errorf("ModelInfo.Models = %v, want [gpt-4o gpt-4o-mini] from mock /models", info.Models)
	}
	// Default model resolves to the first /models entry (no hardcode).
	if info.Name != "gpt-4o" {
		t.Errorf("ModelInfo.Name = %q, want gpt-4o (first from mock /models)", info.Name)
	}
	// ContextSize is reserved/deferred — must be 0 (not hardcoded).
	if info.ContextSize != 0 {
		t.Errorf("ModelInfo.ContextSize = %d, want 0 (reserved/deferred)", info.ContextSize)
	}
}

func TestOpenAIGenerateDefaultModelDynamic(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	// No configured model → Generate resolves to the first /models entry.
	p := NewOpenAI(OpenAIConfig{BaseURL: m.server.URL, APIKey: "test-key"})

	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if m.lastBody["model"] != "gpt-4o" {
		t.Errorf("model = %v, want gpt-4o (first from mock /models, not hardcoded)", m.lastBody["model"])
	}
}

func TestOpenAIFetchModelsCached(t *testing.T) {
	m := newMockChatServer(t, http.StatusOK, `{}`)
	p := newTestOpenAI(m)

	// First call fetches; second uses cache (mock would still serve, but the
	// point is determinism). Verify the auth header on the /models request.
	_ = p.ModelInfo()
	_ = p.ModelInfo()
	if m.lastPath != "/models" {
		t.Errorf("lastPath = %q, want /models", m.lastPath)
	}
	if m.lastAuth != "Bearer test-key" {
		t.Errorf("auth on /models = %q, want Bearer test-key", m.lastAuth)
	}
}

func TestOpenAIFetchModelsFailureEmpty(t *testing.T) {
	// Server returns 401 for /models — fetch must degrade to empty, not panic.
	m := newMockChatServer(t, http.StatusOK, `{}`)
	m.models = `{"error": {"message": "unauthorized"}}`
	m.status = http.StatusUnauthorized
	p := newTestOpenAI(m)

	info := p.ModelInfo()
	if len(info.Models) != 0 {
		t.Errorf("ModelInfo.Models = %v, want empty on failure (never hardcoded)", info.Models)
	}
}
