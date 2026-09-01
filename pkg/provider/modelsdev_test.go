package provider

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testCatalog = `{
  "openai": {
    "models": {
      "gpt-4o": {"limit": {"context": 128000, "output": 16384}},
      "gpt-4.1": {"limit": {"context": 1048576, "input": 1000000, "output": 32768}}
    }
  },
  "deepseek": {
    "models": {
      "deepseek-chat": {"limit": {"context": 128000, "output": 8192}}
    }
  }
}`

// newTestSpecs starts an httptest server serving /api.json and returns a
// ModelSpecs bound to it with a temp cache path.
func newTestSpecs(t *testing.T) (*ModelSpecs, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(testCatalog))
	}))
	t.Cleanup(srv.Close)
	cache := filepath.Join(t.TempDir(), "models.json")
	specs := NewModelSpecs()
	specs.Configure(srv.URL, cache)
	return specs, srv
}

func TestModelSpecsLookup(t *testing.T) {
	// Fresh in-memory data (dataTime = now) so ensureLoaded short-circuits
	// and no disk-load/network happens; lookup reads the manually-set table.
	specs := NewModelSpecs()
	specs.Configure("http://127.0.0.1:1", filepath.Join(t.TempDir(), "models.json"))
	specs.mu.Lock()
	specs.providers = map[string]modelsDevProvider{
		"openai": {
			Models: map[string]modelsDevModel{
				"gpt-4o": {Limit: modelsDevLimit{Context: 128000, Output: 16384}},
			},
		},
	}
	specs.loaded = true
	specs.dataTime = time.Now()
	specs.mu.Unlock()

	ctx, out := specs.Get("openai", "gpt-4o")
	if ctx != 128000 || out != 16384 {
		t.Fatalf("Get(openai, gpt-4o) = %d,%d want 128000,16384", ctx, out)
	}
	// Unknown model → 0.
	if ctx, _ := specs.Get("openai", "nope"); ctx != 0 {
		t.Errorf("Get(openai, nope) context = %d, want 0", ctx)
	}
	// Unknown provider → 0.
	if ctx, _ := specs.Get("unknown", "gpt-4o"); ctx != 0 {
		t.Errorf("Get(unknown, gpt-4o) context = %d, want 0", ctx)
	}
}

func TestModelSpecsDisabled(t *testing.T) {
	t.Setenv(ModelsDevEnvDisable, "1")
	specs, _ := newTestSpecs(t)
	if ctx, _ := specs.Get("openai", "gpt-4o"); ctx != 0 {
		t.Errorf("disabled Get = %d, want 0", ctx)
	}
}

func TestModelSpecsFetchAndCache(t *testing.T) {
	specs, srv := newTestSpecs(t)
	_ = srv

	ctx, out := specs.Get("openai", "gpt-4o")
	if ctx != 128000 || out != 16384 {
		t.Fatalf("Get after fetch = %d,%d want 128000,16384", ctx, out)
	}

	// Catalog persisted to the cache file.
	data, err := os.ReadFile(specs.cachePath)
	if err != nil {
		t.Fatalf("read cache: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("cache file empty")
	}

	// A fresh instance loads from disk without network.
	offline := NewModelSpecs()
	offline.Configure("http://127.0.0.1:1", specs.cachePath)
	ctx, out = offline.Get("deepseek", "deepseek-chat")
	if ctx != 128000 || out != 8192 {
		t.Fatalf("Get from disk cache = %d,%d want 128000,8192", ctx, out)
	}
}

func TestModelSpecsFetchErrorDegrades(t *testing.T) {
	// Unreachable endpoint → Get returns 0 (unknown), no panic.
	specs := NewModelSpecs()
	specs.Configure("http://127.0.0.1:1", filepath.Join(t.TempDir(), "models.json"))
	if ctx, _ := specs.Get("openai", "gpt-4o"); ctx != 0 {
		t.Errorf("unreachable Get = %d, want 0", ctx)
	}
}

func TestProviderManagerModelLimits(t *testing.T) {
	m := NewManagerComponent("pm")
	specs, _ := newTestSpecs(t)
	specs.mu.Lock()
	specs.providers = map[string]modelsDevProvider{
		"openai": {
			Models: map[string]modelsDevModel{
				"gpt-4o": {Limit: modelsDevLimit{Context: 128000, Output: 16384}},
			},
		},
	}
	specs.loaded = true
	specs.dataTime = time.Now()
	specs.mu.Unlock()
	m.setSpecs(specs)

	if got := m.ModelLimitsFor("openai", "gpt-4o"); got.ContextSize != 128000 || got.MaxOutput != 16384 {
		t.Fatalf("ModelLimitsFor = %+v, want context=128000 output=16384", got)
	}
	if got := m.ModelLimitsFor("openai", "nope"); got.ContextSize != 0 {
		t.Errorf("unknown ModelLimitsFor context = %d, want 0", got.ContextSize)
	}
}
