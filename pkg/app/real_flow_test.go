package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/iface/cli"
	"github.com/tltre/gogent/pkg/provider"
)

// mockLLMServer simulates an OpenAI-compatible /chat/completions endpoint.
// It records every request and replies with a model-tagged content so tests
// can assert which engine/model was actually used.
type mockLLMServer struct {
	mu       sync.Mutex
	requests []recordedRequest
}

type recordedRequest struct {
	Auth   string
	Model  string
	Stream bool
	Body   map[string]any
}

func newMockLLMServer(t *testing.T) (*mockLLMServer, *httptest.Server) {
	t.Helper()
	m := &mockLLMServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Model list endpoint (engine ModelInfo fetch). Serve per-provider
		// lists keyed by auth so each engine sees only its own models.
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer sk-ds") {
				_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-chat","object":"model"},{"id":"deepseek-reasoner","object":"model"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-4o","object":"model"},{"id":"gpt-4o-mini","object":"model"}]}`))
			}
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		stream, _ := body["stream"].(bool)
		model, _ := body["model"].(string)
		rec := recordedRequest{
			Auth:   r.Header.Get("Authorization"),
			Model:  model,
			Stream: stream,
			Body:   body,
		}
		m.mu.Lock()
		m.requests = append(m.requests, rec)
		m.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if stream {
			// Minimal SSE: one content delta, then done.
			_, _ = w.Write([]byte(
				`data: {"choices":[{"delta":{"content":"` + model + `-stream"},"finish_reason":null}]}` + "\n" +
					`data: {"choices":[{"delta":{},"finish_reason":"stop"}]}` + "\n" +
					`data: [DONE]` + "\n"))
			return
		}
		_, _ = w.Write([]byte(
			`{"choices":[{"message":{"content":"` + model + `-reply"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return m, srv
}

func (m *mockLLMServer) last() recordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.requests) == 0 {
		return recordedRequest{}
	}
	return m.requests[len(m.requests)-1]
}

func (m *mockLLMServer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.requests)
}

// buildRealApp assembles a full app whose engines point at the mock server
// via OPENAI_BASE_URL / DEEPSEEK_BASE_URL, with credentials in an app-scoped
// store. It returns the app and the injected store (same instance engines and
// the CLI /key command use).
func buildRealApp(t *testing.T, srvURL string, credEntries map[string]string) (*App, provider.CredentialStore) {
	t.Helper()

	t.Setenv(provider.OpenAIEnvBaseURL, srvURL)
	t.Setenv(provider.DeepSeekEnvBaseURL, srvURL)

	store := provider.NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.yaml"))
	for k, v := range credEntries {
		if err := store.Set(k, v); err != nil {
			t.Fatalf("store.Set(%s) = %v", k, err)
		}
	}

	cfg, err := ParseConfig([]byte(`
name: flow-real
components:
  - name: "provider-main"
    type: "provider"
    driver: "native"
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	b := NewBuilderFromConfig(cfg)
	built, err := b.Build(WithCredentialStore(store))
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	ctx := context.Background()
	if err := built.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	return built, store
}

func realRuntime(t *testing.T, built *App) *agentcore.AgentRuntime {
	t.Helper()
	runtime, ok := built.GetDefault(component.ComponentAgentCore).(*agentcore.AgentRuntime)
	if !ok {
		t.Fatal("agentcore is not *AgentRuntime")
	}
	return runtime
}

// ---------------------------------------------------------------------------
// 2.1 Full chain: real engine → mock server, OpenAI route
// ---------------------------------------------------------------------------

func TestFlowRealOpenAIRoute(t *testing.T) {
	mock, srv := newMockLLMServer(t)
	built, _ := buildRealApp(t, srv.URL, map[string]string{"openai": "sk-openai-flow"})
	runtime := realRuntime(t, built)

	out, err := runtime.Run(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "hi"}},
		ProviderName: "openai",
		ModelName:    "gpt-4o-mini",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(out.Response.Content, "gpt-4o-mini-reply") {
		t.Errorf("Response.Content = %q, want gpt-4o-mini-reply", out.Response.Content)
	}

	rec := mock.last()
	if rec.Auth != "Bearer sk-openai-flow" {
		t.Errorf("auth = %q, want Bearer sk-openai-flow (from store)", rec.Auth)
	}
	if rec.Model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini (via ctx)", rec.Model)
	}
}

// ---------------------------------------------------------------------------
// 2.2 Route switch to DeepSeek with its own credential key
// ---------------------------------------------------------------------------

func TestFlowRealDeepSeekRoute(t *testing.T) {
	mock, srv := newMockLLMServer(t)
	built, _ := buildRealApp(t, srv.URL, map[string]string{"deepseek": "sk-deepseek-flow"})
	runtime := realRuntime(t, built)

	out, err := runtime.Run(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "hi"}},
		ProviderName: "deepseek",
		ModelName:    "deepseek-reasoner",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.Contains(out.Response.Content, "deepseek-reasoner-reply") {
		t.Errorf("Response.Content = %q, want deepseek-reasoner-reply", out.Response.Content)
	}

	rec := mock.last()
	if rec.Auth != "Bearer sk-deepseek-flow" {
		t.Errorf("auth = %q, want Bearer sk-deepseek-flow (isolated key)", rec.Auth)
	}
	if rec.Model != "deepseek-reasoner" {
		t.Errorf("model = %q, want deepseek-reasoner", rec.Model)
	}
}

// ---------------------------------------------------------------------------
// 2.3 Store overrides env fallback
// ---------------------------------------------------------------------------

func TestFlowRealStoreOverridesEnv(t *testing.T) {
	mock, srv := newMockLLMServer(t)
	// No OPENAI_API_KEY env; key only in the store.
	built, _ := buildRealApp(t, srv.URL, map[string]string{"openai": "sk-store-only"})
	runtime := realRuntime(t, built)

	if _, err := runtime.Run(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "hi"}},
		ProviderName: "openai",
	}); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if rec := mock.last(); rec.Auth != "Bearer sk-store-only" {
		t.Errorf("auth = %q, want Bearer sk-store-only (store overrides missing env)", rec.Auth)
	}
}

// ---------------------------------------------------------------------------
// 2.4 Stream through the real chain
// ---------------------------------------------------------------------------

func TestFlowRealStream(t *testing.T) {
	mock, srv := newMockLLMServer(t)
	built, _ := buildRealApp(t, srv.URL, map[string]string{"deepseek": "sk-ds-stream"})
	runtime := realRuntime(t, built)

	ch, err := runtime.Stream(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "hi"}},
		ProviderName: "deepseek",
		ModelName:    "deepseek-chat",
	})
	if err != nil {
		t.Fatalf("Stream() = %v", err)
	}

	var events []agentcore.Event
	for evt := range ch {
		events = append(events, evt)
	}
	// The mock sends two deltas (content + empty finish) → 2 streaming events
	// + 1 after-run. Assert the terminal event carries the streamed content.
	if len(events) < 2 {
		t.Fatalf("events = %d, want >= 2", len(events))
	}
	last := events[len(events)-1]
	after, ok := last.Payload.(agentcore.Output)
	if !ok || !strings.Contains(after.Response.Content, "deepseek-chat-stream") {
		t.Errorf("after-run content = %+v, want deepseek-chat-stream", last.Payload)
	}
	if !mock.last().Stream {
		t.Error("mock received stream=false, want true")
	}
}

// ---------------------------------------------------------------------------
// Interactive /key configuration end-to-end: set a key in the REPL, then a
// message must use it immediately.
// ---------------------------------------------------------------------------

func TestFlowChatConfiguresKeyThenUsesIt(t *testing.T) {
	mock, srv := newMockLLMServer(t)
	built, store := buildRealApp(t, srv.URL, nil) // no credentials pre-configured
	runtime := realRuntime(t, built)
	_ = runtime

	// Drive the chat REPL: configure openai key via /key, switch to openai,
	// then send a message (default engine is deepseek — alphabetical first).
	input := "/key openai sk-interactive\n/provider openai\nhello\nquit\n"
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, _ = w.WriteString(input)
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	iface := cli.New("", "> ")
	iface.SetCredentialStore(store) // same store instance engines use
	built.SetInterface(iface)
	if err := iface.Run(context.Background(), built.Registry()); err != nil {
		t.Fatalf("iface.Run() = %v", err)
	}

	if mock.count() == 0 {
		t.Fatal("no chat request hit the mock server")
	}
	rec := mock.last()
	if rec.Auth != "Bearer sk-interactive" {
		t.Errorf("auth = %q, want Bearer sk-interactive (set interactively via /key)", rec.Auth)
	}
	if rec.Model != "gpt-4o" {
		t.Errorf("model = %q, want gpt-4o (openai default)", rec.Model)
	}
}
