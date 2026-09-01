package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ModelSpecs is the centralized model-capability catalog (context / max-output
// limits) backed by a remote spec endpoint.
//
// Provider /models endpoints do not expose context windows uniformly (see the
// note on ModelInfo.ContextSize), so Gogent follows the industry pattern
// (LiteLLM / models.dev) of a central spec table. The catalog is fetched from
// models.opencode.ai and cached locally so the agent keeps working offline.
//
// The catalog is lazy: no network I/O happens until a consumer asks for
// limits. The first Get for a provider+model loads the disk cache if present,
// otherwise blocks once to fetch. A stale in-memory table is refreshed in the
// background. Unknown provider/model or fetch failure yields 0 — callers MUST
// treat 0 as "unknown" and disable context-sensitive features (e.g. history
// compression) rather than guessing.
type ModelSpecs struct {
	mu        sync.RWMutex
	providers map[string]modelsDevProvider
	loaded    bool
	dataTime  time.Time // when the in-memory data was produced (cache file mtime or fetch time)

	refreshMu  sync.Mutex
	refreshing bool // single-flight guard for background refreshes

	url       string
	cachePath string
	client    *http.Client
}

// models.opencode.ai endpoint and cache policy.
const (
	// DefaultModelsDevURL is the canonical model-spec endpoint.
	DefaultModelsDevURL = "https://models.opencode.ai"
	// ModelsDevEnvURL overrides the endpoint (tests, self-hosted mirrors).
	ModelsDevEnvURL = "GOGENT_MODELS_URL"
	// ModelsDevEnvDisable disables remote fetching entirely (offline mode).
	ModelsDevEnvDisable = "GOGENT_DISABLE_MODELS_FETCH"

	modelsDevCacheTTL     = 5 * time.Minute
	modelsDevHTTPTimeout  = 10 * time.Second
	modelsDevMaxBodyBytes = 50 << 20 // 50 MiB safety cap
	modelsDevCacheFile    = "models.json"
)

// Minimal /api.json schema — only the fields Gogent consumes.
type modelsDevProvider struct {
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevModel struct {
	Limit modelsDevLimit `json:"limit"`
}

type modelsDevLimit struct {
	Context int `json:"context"`
	Input   int `json:"input"`
	Output  int `json:"output"`
}

// defaultSpecs is the process-wide catalog used by ProviderManager. It is
// lazy — no network I/O occurs until a consumer asks for model limits.
var defaultSpecs = NewModelSpecs()

// NewModelSpecs creates an empty catalog bound to the endpoint from
// GOGENT_MODELS_URL (fallback DefaultModelsDevURL) and the default cache
// location (~/.gogent/models.json).
func NewModelSpecs() *ModelSpecs {
	return &ModelSpecs{
		url:       os.Getenv(ModelsDevEnvURL),
		cachePath: defaultModelCachePath(),
		client:    &http.Client{Timeout: modelsDevHTTPTimeout},
	}
}

// Configure overrides the endpoint URL and cache file path. Intended for
// tests and self-hosted mirrors. Empty arguments keep the current value.
func (s *ModelSpecs) Configure(url, cachePath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if url != "" {
		s.url = url
	}
	if cachePath != "" {
		s.cachePath = cachePath
	}
}

func (s *ModelSpecs) endpoint() string {
	if s.url != "" {
		return s.url
	}
	return DefaultModelsDevURL
}

func (s *ModelSpecs) disabled() bool {
	return os.Getenv(ModelsDevEnvDisable) != ""
}

func defaultModelCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gogent", modelsDevCacheFile)
}

// Get returns the context and max-output limits for provider+model, loading
// the catalog (disk cache → network) when needed. Returns 0,0 when the
// provider/model is unknown or the catalog is unavailable.
func (s *ModelSpecs) Get(provider, model string) (contextSize, maxOutput int) {
	if s.disabled() {
		return 0, 0
	}
	s.ensureLoaded()
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lookup(provider, model)
}

func (s *ModelSpecs) lookup(provider, model string) (contextSize, maxOutput int) {
	p, ok := s.providers[provider]
	if !ok {
		return 0, 0
	}
	m, ok := p.Models[model]
	if !ok {
		return 0, 0
	}
	return m.Limit.Context, m.Limit.Output
}

// Refresh force-fetches the catalog and updates the disk cache. Used by
// tests and maintenance paths; normal operation refreshes lazily.
func (s *ModelSpecs) Refresh(ctx context.Context) error {
	return s.refresh(ctx)
}

// ensureLoaded brings the catalog into memory:
//   - fresh in-memory data  → nothing to do
//   - disk cache present    → load it; refresh in background if stale
//   - no cache              → block once to fetch (the only sync network path)
func (s *ModelSpecs) ensureLoaded() {
	s.mu.RLock()
	fresh := s.loaded && time.Since(s.dataTime) < modelsDevCacheTTL
	s.mu.RUnlock()
	if fresh {
		return
	}

	if s.loadFromDisk() {
		s.mu.RLock()
		stale := time.Since(s.dataTime) >= modelsDevCacheTTL
		s.mu.RUnlock()
		if stale {
			s.triggerAsyncRefresh()
		}
		return
	}
	_ = s.refresh(context.Background())
}

func (s *ModelSpecs) loadFromDisk() bool {
	path := s.cachePath
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil {
		fi = nil
	}
	var mtime time.Time
	if fi != nil {
		mtime = fi.ModTime()
	}
	s.mu.Lock()
	s.providers = providers
	s.loaded = true
	s.dataTime = mtime
	s.mu.Unlock()
	return true
}

// triggerAsyncRefresh starts one background refresh (single-flight) so a
// stale-but-usable cache keeps serving while the network catches up.
func (s *ModelSpecs) triggerAsyncRefresh() {
	s.refreshMu.Lock()
	if s.refreshing {
		s.refreshMu.Unlock()
		return
	}
	s.refreshing = true
	s.refreshMu.Unlock()

	go func() {
		defer func() {
			s.refreshMu.Lock()
			s.refreshing = false
			s.refreshMu.Unlock()
		}()
		_ = s.refresh(context.Background())
	}()
}

func (s *ModelSpecs) refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	// Double-check under the lock: another refresh may have completed while
	// we were waiting.
	s.mu.RLock()
	fresh := s.loaded && time.Since(s.dataTime) < modelsDevCacheTTL
	s.mu.RUnlock()
	if fresh {
		return nil
	}

	raw, err := s.fetch(ctx)
	if err != nil {
		return err
	}
	var providers map[string]modelsDevProvider
	if err := json.Unmarshal(raw, &providers); err != nil {
		return fmt.Errorf("models spec: parse catalog: %w", err)
	}

	s.mu.Lock()
	s.providers = providers
	s.loaded = true
	s.dataTime = time.Now()
	s.mu.Unlock()

	s.writeCache(raw)
	return nil
}

func (s *ModelSpecs) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint()+"/api.json", nil)
	if err != nil {
		return nil, fmt.Errorf("models spec: build request: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("models spec: fetch %s: %w", s.endpoint(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models spec: fetch %s: status %d", s.endpoint(), resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, modelsDevMaxBodyBytes))
}

// writeCache persists the raw catalog atomically (temp file + rename) so a
// concurrent reader never sees a partial file. Best effort — a failed write
// only costs a future re-fetch.
func (s *ModelSpecs) writeCache(raw []byte) {
	path := s.cachePath
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}