package tool

import (
	"sync"

	"github.com/tltre/gogent/internal/grpctransport/gogentv1"
)

// AppSandboxInfo holds the sandbox-related configuration that an app declares
// in its RegisterManifest request. This is stored per app and consulted
// during ExecuteTool sandbox routing.
type AppSandboxInfo struct {
	// SandboxConfigs maps sandbox config name to profile name.
	// Populated from app's sandboxes declaration.
	SandboxConfigs map[string]string

	// DefaultSandbox is the app-level default sandbox name.
	// Empty means fall back to daemon defaults.
	DefaultSandbox string

	// ToolSandboxMap maps tool name to sandbox instance name.
	// Populated from tools[].sandbox fields.
	ToolSandboxMap map[string]string
}

// ManifestStore holds per-app tool manifest registrations.
// Thread-safe. Entries are lost on daemon restart (apps re-register on reconnect).
type ManifestStore struct {
	mu        sync.RWMutex
	manifests map[string][]*gogentv1.ManifestEntry // key: app_name
	sandboxes map[string]*AppSandboxInfo           // key: app_name (v0.13.2)
}

// NewManifestStore creates an empty manifest store.
func NewManifestStore() *ManifestStore {
	return &ManifestStore{
		manifests: make(map[string][]*gogentv1.ManifestEntry),
		sandboxes: make(map[string]*AppSandboxInfo),
	}
}

// Register stores or replaces a manifest for the given app (idempotent).
func (s *ManifestStore) Register(appName string, tools []*gogentv1.ManifestEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.manifests[appName] = tools
}

// RegisterSandbox stores sandbox configuration alongside the manifest.
func (s *ManifestStore) RegisterSandbox(appName string, info *AppSandboxInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sandboxes[appName] = info
}

// GetSandbox returns the sandbox info for an app.
func (s *ManifestStore) GetSandbox(appName string) *AppSandboxInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sandboxes[appName]
}

// AppsUsingTool returns all app names whose manifest includes the given tool.
func (s *ManifestStore) AppsUsingTool(toolName string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var apps []string
	for appName, entries := range s.manifests {
		for _, e := range entries {
			if e.Name == toolName {
				apps = append(apps, appName)
				break
			}
		}
	}
	return apps
}

// Unregister removes a manifest (called when app stops).
func (s *ManifestStore) Unregister(appName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.manifests, appName)
	delete(s.sandboxes, appName)
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
