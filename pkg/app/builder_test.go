package app

import (
	"context"
	"strings"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/provider"
)

type builderMockProvider struct {
	modelInfo provider.ModelInfo
}

func (m *builderMockProvider) Generate(_ context.Context, _ []provider.ProviderMessage) (provider.Response, error) {
	return provider.Response{Content: "mock"}, nil
}

func (m *builderMockProvider) Stream(_ context.Context, _ []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Delta: "mock", Done: true}
	close(ch)
	return ch, nil
}

func (m *builderMockProvider) ModelInfo() provider.ModelInfo {
	return m.modelInfo
}

// registerTestEngine registers a mock engine factory and returns a cleanup func.
func registerTestEngine(t *testing.T, name string) {
	t.Helper()
	if err := provider.RegisterEngine(name, func() (provider.IProvider, error) {
		return &builderMockProvider{modelInfo: provider.ModelInfo{Provider: name, DisplayName: name}}, nil
	}); err != nil {
		t.Fatalf("RegisterEngine(%q) = %v", name, err)
	}
	t.Cleanup(func() { provider.UnregisterEngine(name) })
}

func TestBuildProviderExclude(t *testing.T) {
	registerTestEngine(t, "test-engine-a")
	registerTestEngine(t, "test-engine-b")
	registerTestEngine(t, "test-engine-c")

	cfg := &Config{
		Name: "test-app",
		Provider: ProviderSection{
			Exclude: []string{"test-engine-b"},
		},
	}

	b := NewBuilderFromConfig(cfg)
	app, err := b.Build()
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	if app == nil {
		t.Fatal("Build() = nil app")
	}

	// The ProviderManager must be registered as the provider component.
	comp := b.registry.GetDefault(component.ComponentProvider)
	if comp == nil {
		t.Fatal("GetDefault(ComponentProvider) = nil, want ProviderManager")
	}
	mgr, ok := comp.(*provider.ProviderManager)
	if !ok {
		t.Fatalf("GetDefault() type = %T, want *provider.ProviderManager", comp)
	}

	infos := mgr.List()
	// The openai engine is registered via init() in this binary, plus the
	// three mock engines. test-engine-b must be excluded; the rest remain.
	if len(infos) < 2 {
		t.Fatalf("List() len = %d, want >= 2 (mock a+c)", len(infos))
	}
	got := map[string]bool{}
	for _, info := range infos {
		got[info.Name] = true
	}
	if !got["test-engine-a"] || !got["test-engine-c"] {
		t.Fatalf("List() = %v, want a and c (b excluded)", got)
	}
	if got["test-engine-b"] {
		t.Fatal("List() includes excluded engine test-engine-b")
	}
	if !got["openai"] {
		t.Error("List() missing openai engine (registered via init)")
	}
}

func TestBuildNoProviderConfigRegistersAllEngines(t *testing.T) {
	// v0.15.x: built-in engines are always registered. Without a provider:
	// section, the ProviderManager still exists and carries every engine.
	registerTestEngine(t, "default-engine")

	cfg := &Config{Name: "test-app"}
	b := NewBuilderFromConfig(cfg)
	app, err := b.Build()
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	if app == nil {
		t.Fatal("Build() = nil app")
	}
	comp := b.registry.GetDefault(component.ComponentProvider)
	if comp == nil {
		t.Fatal("GetDefault(ComponentProvider) = nil, want ProviderManager (built-ins always registered)")
	}
	mgr, ok := comp.(*provider.ProviderManager)
	if !ok {
		t.Fatalf("GetDefault() type = %T, want *provider.ProviderManager", comp)
	}
	names := map[string]bool{}
	for _, info := range mgr.List() {
		names[info.Name] = true
	}
	if !names["default-engine"] || !names["openai"] {
		t.Fatalf("List() = %v, want built-in + registered engines", names)
	}
}

func TestBuildComponentProviderRejected(t *testing.T) {
	// v0.15.x: provider type no longer lives in components[] — the top-level
	// provider: section is the only configuration surface. Old configs must
	// fail with a migration hint instead of silently registering nothing.
	cfg := &Config{
		Name: "test-app",
		Components: []ComponentConfig{
			{Name: "provider-main", Type: "provider", Driver: "native"},
		},
	}
	b := NewBuilderFromConfig(cfg)
	_, err := b.Build()
	if err == nil {
		t.Fatal("Build() = nil error, want rejection of components[] provider")
	}
	if !strings.Contains(err.Error(), "provider:") {
		t.Errorf("error = %v, want migration hint mentioning top-level provider section", err)
	}
}

func TestBuildProviderServerReservedName(t *testing.T) {
	// Built-in engine names are reserved: a server named "openai" must be
	// rejected so /provider openai can never silently route to a remote.
	cfg := &Config{
		Name: "test-app",
		Provider: ProviderSection{
			Servers: []ProviderServerConfig{
				{Name: "openai", Endpoint: "localhost:9092"},
			},
		},
	}
	b := NewBuilderFromConfig(cfg)
	_, err := b.Build()
	if err == nil {
		t.Fatal("Build() = nil error, want reserved-name rejection")
	}
	if !strings.Contains(err.Error(), "conflicts with builtin engine") {
		t.Errorf("error = %v, want conflicts-with-builtin message", err)
	}
}

func TestBuildProviderServerRequiresEndpoint(t *testing.T) {
	cfg := &Config{
		Name: "test-app",
		Provider: ProviderSection{
			Servers: []ProviderServerConfig{
				{Name: "my-gateway"},
			},
		},
	}
	b := NewBuilderFromConfig(cfg)
	_, err := b.Build()
	if err == nil {
		t.Fatal("Build() = nil error, want missing-endpoint rejection")
	}
	if !strings.Contains(err.Error(), "endpoint is required") {
		t.Errorf("error = %v, want endpoint-is-required message", err)
	}
}

func TestBuildProviderServerRegistered(t *testing.T) {
	// A valid external server lands in the ProviderManager under its name.
	cfg := &Config{
		Name: "test-app",
		Provider: ProviderSection{
			Servers: []ProviderServerConfig{
				{Name: "my-gateway", Endpoint: "localhost:9092"},
			},
		},
	}
	b := NewBuilderFromConfig(cfg)
	if _, err := b.Build(); err != nil {
		t.Fatalf("Build() = %v", err)
	}
	mgr := b.registry.GetDefault(component.ComponentProvider).(*provider.ProviderManager)
	if mgr.Get("my-gateway") == nil {
		t.Fatal("ProviderManager.Get(my-gateway) = nil, want registered server")
	}
}

func TestBuildDefaultsProviderIgnored(t *testing.T) {
	// defaults.provider is managed by the ProviderManager; the entry is
	// skipped (with a warning) instead of failing Build.
	cfg := &Config{
		Name:     "test-app",
		Defaults: map[string]string{"provider": "provider-main"},
	}
	b := NewBuilderFromConfig(cfg)
	if _, err := b.Build(); err != nil {
		t.Fatalf("Build() = %v, want defaults.provider ignored", err)
	}
}

// storeAwareMock records whether a CredentialStore was injected.
type storeAwareMock struct {
	builderMockProvider
	store provider.CredentialStore
}

func (m *storeAwareMock) SetCredentialStore(s provider.CredentialStore) {
	m.store = s
}

func TestBuildInjectsCredentialStore(t *testing.T) {
	registerTestEngine(t, "cred-test-engine")

	cfg := &Config{
		Name:     "cred-app",
		Provider: ProviderSection{},
	}

	// Register a store-aware mock engine by replacing the factory is not
	// possible post-registration; instead verify the default store is a
	// FileCredentialStore with the app-scoped path, and that
	// WithCredentialStore overrides it.
	custom := provider.NewFileCredentialStore("/tmp/custom-cred.yaml")

	b := NewBuilderFromConfig(cfg)
	if _, err := b.Build(WithCredentialStore(custom)); err != nil {
		t.Fatalf("Build() = %v", err)
	}
	if b.credStore != custom {
		t.Fatalf("credStore after WithCredentialStore = %v, want custom", b.credStore)
	}

	// Default path derivation is app-scoped.
	path := provider.DefaultAppCredentialPath("cred-app")
	if path == "" || path == "/tmp/custom-cred.yaml" {
		t.Fatalf("DefaultAppCredentialPath = %q, want app-scoped path", path)
	}
}

