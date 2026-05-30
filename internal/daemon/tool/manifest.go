package tool

import (
	"sync"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// ManifestStore holds per-app tool manifest registrations.
// Thread-safe. Entries are lost on daemon restart (apps re-register on reconnect).
type ManifestStore struct {
	mu        sync.RWMutex
	manifests map[string][]*gogentv1.ManifestEntry // key: app_name
}

// NewManifestStore creates an empty manifest store.
func NewManifestStore() *ManifestStore {
	return &ManifestStore{
		manifests: make(map[string][]*gogentv1.ManifestEntry),
	}
}

// Register stores or replaces a manifest for the given app (idempotent).
func (s *ManifestStore) Register(appName string, tools []*gogentv1.ManifestEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifests[appName] = tools
}

// Unregister removes a manifest (called when app stops).
func (s *ManifestStore) Unregister(appName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.manifests, appName)
}

// Get returns the tools declared by an app.
func (s *ManifestStore) Get(appName string) []*gogentv1.ManifestEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.manifests[appName]
}

// IsAuthorized checks if an app is allowed to use a tool.
// Returns true if the tool name appears in the app's registered manifest.
func (s *ManifestStore) IsAuthorized(appName, toolName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, ok := s.manifests[appName]
	if !ok {
		return false
	}
	for _, e := range entries {
		if e.Name == toolName {
			return true
		}
	}
	return false
}
