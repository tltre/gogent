package sandbox

import (
	"context"
	"fmt"
	"sync"
)

// ---------------------------------------------------------------------------
// SandboxProvider interface
// ---------------------------------------------------------------------------

// SandboxProvider is the abstraction for creating sandbox instances from
// different backends. Each backend type (e2b, builtin, agent-sandbox, etc.)
// implements this interface.
//
// Pattern reference: McpRunner handles process/http via different transports
// but shares the same Runner interface. Similarly, SandboxProvider handles
// different sandbox backends via a unified Create method.
type SandboxProvider interface {
	// Name returns a human-readable name for this provider instance
	// (e.g., "my-e2b", "local-builtin").
	Name() string

	// Type returns the sandbox backend type identifier.
	Type() SandboxType

	// Create creates a new sandbox instance from the given profile and
	// app-specific config. For remote backends (E2B), this may involve
	// an HTTP API call. For local backends (builtin), it sets up OS-level
	// isolation synchronously.
	Create(ctx context.Context, profile *SandboxProfile, cfg *SandboxConfig) (ISandbox, error)
}

// ---------------------------------------------------------------------------
// ProviderRegistry
// ---------------------------------------------------------------------------

// ProviderRegistry manages all registered SandboxProvider instances.
// Thread-safe.
//
// Pattern reference: ServerStore manages MCP server metadata; McpRunner
// dispatches to the right server by name. ProviderRegistry similarly stores
// provider metadata and allows lookup by type or name.
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]SandboxProvider // key: provider name (from sandbox.yaml)
	byType    map[SandboxType][]string   // key: type -> list of provider names
}

// NewProviderRegistry creates an empty registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]SandboxProvider),
		byType:    make(map[SandboxType][]string),
	}
}

// Register adds a provider. Returns error if name already exists.
func (r *ProviderRegistry) Register(p SandboxProvider) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := p.Name()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("provider %q already registered", name)
	}

	r.providers[name] = p
	r.byType[p.Type()] = append(r.byType[p.Type()], name)
	return nil
}

// Get retrieves a provider by name.
func (r *ProviderRegistry) Get(name string) (SandboxProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// GetByType returns all providers of the given type.
func (r *ProviderRegistry) GetByType(typ SandboxType) []SandboxProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := r.byType[typ]
	if len(names) == 0 {
		return nil
	}

	result := make([]SandboxProvider, 0, len(names))
	for _, name := range names {
		if p, ok := r.providers[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

// List returns all registered providers.
func (r *ProviderRegistry) List() []SandboxProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SandboxProvider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result
}

// Remove unregisters a provider by name.
func (r *ProviderRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if p, ok := r.providers[name]; ok {
		// Remove from byType index
		names := r.byType[p.Type()]
		for i, n := range names {
			if n == name {
				r.byType[p.Type()] = append(names[:i], names[i+1:]...)
				break
			}
		}
		delete(r.providers, name)
	}
}
