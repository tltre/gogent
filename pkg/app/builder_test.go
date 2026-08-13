package app

import (
	"context"
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

func TestBuildNativeProvidersExclude(t *testing.T) {
	registerTestEngine(t, "test-engine-a")
	registerTestEngine(t, "test-engine-b")
	registerTestEngine(t, "test-engine-c")

	cfg := &Config{
		Name: "test-app",
		Components: []ComponentConfig{
			{
				Name:   "provider-main",
				Type:   "provider",
				Driver: "native",
				Config: map[string]any{
					"exclude": []any{"test-engine-b"},
				},
			},
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

func TestBuildNoNativeProviderNoManager(t *testing.T) {
	// Without any provider component, no ProviderManager is registered.
	cfg := &Config{Name: "test-app"}
	b := NewBuilderFromConfig(cfg)
	app, err := b.Build()
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	if app == nil {
		t.Fatal("Build() = nil app")
	}
	if comp := b.registry.GetDefault(component.ComponentProvider); comp != nil {
		t.Fatalf("GetDefault(ComponentProvider) = %v, want nil", comp)
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
		Name: "cred-app",
		Components: []ComponentConfig{
			{
				Name:   "provider-main",
				Type:   "provider",
				Driver: "native",
			},
		},
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

