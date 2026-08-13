package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// FileCredentialStore
// ---------------------------------------------------------------------------

func TestFileCredentialStoreCRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	s := NewFileCredentialStore(path)

	// Get on empty store returns empty, no error.
	if v, err := s.Get("openai"); err != nil || v != "" {
		t.Fatalf("Get(empty) = %q, %v; want \"\", nil", v, err)
	}

	if err := s.Set("openai", "sk-openai-1"); err != nil {
		t.Fatalf("Set() = %v", err)
	}
	if err := s.Set("deepseek", "sk-deepseek-1"); err != nil {
		t.Fatalf("Set() = %v", err)
	}

	if v, err := s.Get("openai"); err != nil || v != "sk-openai-1" {
		t.Fatalf("Get(openai) = %q, %v; want sk-openai-1, nil", v, err)
	}

	// List sorted.
	names, err := s.List()
	if err != nil {
		t.Fatalf("List() = %v", err)
	}
	if len(names) != 2 || names[0] != "deepseek" || names[1] != "openai" {
		t.Fatalf("List() = %v, want [deepseek openai]", names)
	}

	if err := s.Delete("openai"); err != nil {
		t.Fatalf("Delete() = %v", err)
	}
	if v, _ := s.Get("openai"); v != "" {
		t.Errorf("Get(openai) after delete = %q, want empty", v)
	}
}

func TestFileCredentialStorePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.yaml")
	s := NewFileCredentialStore(path)
	if err := s.Set("openai", "sk-persist"); err != nil {
		t.Fatalf("Set() = %v", err)
	}

	// A new store over the same file must see the persisted value.
	s2 := NewFileCredentialStore(path)
	if v, err := s2.Get("openai"); err != nil || v != "sk-persist" {
		t.Fatalf("reload Get(openai) = %q, %v; want sk-persist, nil", v, err)
	}
}

func TestDefaultAppCredentialPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	got := DefaultAppCredentialPath("my-agent")
	want := filepath.Join(home, ".gogent", "apps", "my-agent", "credentials.yaml")
	if got != want {
		t.Errorf("DefaultAppCredentialPath() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Engine key resolution (CredentialStoreAware)
// ---------------------------------------------------------------------------

func TestEngineUsesCredentialStore(t *testing.T) {
	// Server captures the Authorization header.
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)

	store := NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.yaml"))
	if err := store.Set("openai", "sk-from-store"); err != nil {
		t.Fatalf("store.Set() = %v", err)
	}

	p := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, APIKey: "sk-from-env"})
	p.SetCredentialStore(store)

	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if auth != "Bearer sk-from-store" {
		t.Errorf("auth = %q, want Bearer sk-from-store (store overrides env)", auth)
	}
}

func TestEngineEnvFallbackWithoutStore(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)

	p := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, APIKey: "sk-from-env"})
	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if auth != "Bearer sk-from-env" {
		t.Errorf("auth = %q, want Bearer sk-from-env", auth)
	}
}

func TestEngineStoreEmptyFallsBackToEnv(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)

	store := NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.yaml")) // no openai key
	p := NewOpenAI(OpenAIConfig{BaseURL: srv.URL, APIKey: "sk-from-env"})
	p.SetCredentialStore(store)

	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if auth != "Bearer sk-from-env" {
		t.Errorf("auth = %q, want Bearer sk-from-env (store empty)", auth)
	}
}

func TestDeepSeekUsesOwnCredentialKey(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))
	}))
	t.Cleanup(srv.Close)

	store := NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.yaml"))
	if err := store.Set("deepseek", "sk-ds-store"); err != nil {
		t.Fatalf("store.Set() = %v", err)
	}

	p := NewDeepSeek(DeepSeekConfig{APIKey: "sk-ds-env"})
	p.baseURL = srv.URL
	p.SetCredentialStore(store)

	if _, err := p.Generate(context.Background(), []ProviderMessage{{Role: "user", Content: "hi"}}); err != nil {
		t.Fatalf("Generate() = %v", err)
	}
	if auth != "Bearer sk-ds-store" {
		t.Errorf("auth = %q, want Bearer sk-ds-store (deepseek key from store)", auth)
	}
}

// ---------------------------------------------------------------------------
// CredentialStoreAware interface conformance
// ---------------------------------------------------------------------------

func TestEnginesImplementCredentialStoreAware(t *testing.T) {
	var openaiAware CredentialStoreAware = NewOpenAI(OpenAIConfig{})
	if openaiAware == nil {
		t.Fatal("OpenAIProvider does not implement CredentialStoreAware")
	}
	var dsAware CredentialStoreAware = NewDeepSeek(DeepSeekConfig{})
	if dsAware == nil {
		t.Fatal("DeepSeekProvider does not implement CredentialStoreAware")
	}
}
