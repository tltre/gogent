package sandbox

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// SandboxManager
// ---------------------------------------------------------------------------

// SandboxManager is the central entry point for daemon-side sandbox management.
//
// Responsibilities:
//  1. Manage SandboxProfile registry (loaded from sandbox.yaml)
//  2. Manage ProviderRegistry (connected to sandbox-providers config)
//  3. Manage per-App sandbox instances (create on first use, destroy on disconnect)
//  4. Provide RefreshAll() for periodic TTL refresh (called by Daemon)
//
// Pattern reference: ServerStore (stores metadata) + McpRunner (manages instances).
// SandboxManager separates profile metadata (ProfileRegistry) from runtime
// instances (per-App sandbox map).
type SandboxManager struct {
	mu       sync.RWMutex
	profiles map[string]*SandboxProfile   // key: profile name
	providers *ProviderRegistry

	// Per-app sandbox instances.
	// First key: app name; second key: sandbox config name.
	appInstances map[string]map[string]ISandbox

	// Refresh tracking (optional, set via SetRefreshInterval).
	refreshInterval time.Duration // 0 = disabled
}

// NewSandboxManager creates a SandboxManager with empty registries.
func NewSandboxManager() *SandboxManager {
	return &SandboxManager{
		profiles:      make(map[string]*SandboxProfile),
		providers:     NewProviderRegistry(),
		appInstances:  make(map[string]map[string]ISandbox),
	}
}

// ---------------------------------------------------------------------------
// Profile management
// ---------------------------------------------------------------------------

// RegisterProfile adds a sandbox profile. Returns error if name already exists.
func (mgr *SandboxManager) RegisterProfile(p *SandboxProfile) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if _, exists := mgr.profiles[p.Name]; exists {
		return fmt.Errorf("profile %q already registered", p.Name)
	}
	mgr.profiles[p.Name] = p
	return nil
}

// GetProfile returns a profile by name.
func (mgr *SandboxManager) GetProfile(name string) (*SandboxProfile, bool) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	p, ok := mgr.profiles[name]
	return p, ok
}

// ListProfiles returns all registered profile names.
func (mgr *SandboxManager) ListProfiles() []string {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	names := make([]string, 0, len(mgr.profiles))
	for name := range mgr.profiles {
		names = append(names, name)
	}
	return names
}

// ---------------------------------------------------------------------------
// Provider management
// ---------------------------------------------------------------------------

// RegisterProvider registers a sandbox provider with the internal registry.
func (mgr *SandboxManager) RegisterProvider(p SandboxProvider) error {
	return mgr.providers.Register(p)
}

// GetProvider returns a provider by name.
func (mgr *SandboxManager) GetProvider(name string) (SandboxProvider, bool) {
	return mgr.providers.Get(name)
}

// ProviderRegistry returns the internal provider registry.
func (mgr *SandboxManager) ProviderRegistry() *ProviderRegistry {
	return mgr.providers
}

// ---------------------------------------------------------------------------
// Per-App sandbox instance management
// ---------------------------------------------------------------------------

// GetOrCreateForApp returns the named sandbox for the given app, creating it
// if it doesn't exist. This is the main entry point called by ExecuteTool handler.
//
// The sandboxConfig must have ProfileName set to a registered profile.
// On first creation, the profile's provider is called to create the instance.
func (mgr *SandboxManager) GetOrCreateForApp(ctx context.Context, appName string, cfg *SandboxConfig) (ISandbox, error) {
	mgr.mu.Lock()

	// Check if instance already exists
	appSbs, ok := mgr.appInstances[appName]
	if !ok {
		appSbs = make(map[string]ISandbox)
		mgr.appInstances[appName] = appSbs
	} else if existing, ok := appSbs[cfg.Name]; ok {
		mgr.mu.Unlock()
		return existing, nil
	}
	mgr.mu.Unlock()

	// Resolve profile
	profile, ok := mgr.GetProfile(cfg.ProfileName)
	if !ok {
		return nil, fmt.Errorf("sandbox profile %q not found", cfg.ProfileName)
	}

	// Resolve provider
	provider, ok := mgr.providers.Get(profile.Provider)
	if !ok {
		return nil, fmt.Errorf("sandbox provider %q (referenced by profile %q) not found",
			profile.Provider, profile.Name)
	}

	// Create new instance via provider
	sb, err := provider.Create(ctx, profile, cfg)
	if err != nil {
		return nil, fmt.Errorf("create sandbox via %s: %w", provider.Name(), err)
	}

	// Register instance
	mgr.mu.Lock()
	mgr.appInstances[appName][cfg.Name] = sb
	mgr.mu.Unlock()

	return sb, nil
}

// DestroyAppSandboxes destroys all sandbox instances for the given app.
// Called when the app's gRPC connection is closed.
func (mgr *SandboxManager) DestroyAppSandboxes(ctx context.Context, appName string) error {
	mgr.mu.Lock()
	sbs, ok := mgr.appInstances[appName]
	if !ok {
		mgr.mu.Unlock()
		return nil
	}
	delete(mgr.appInstances, appName)
	mgr.mu.Unlock()

	var firstErr error
	for name, sb := range sbs {
		if err := sb.Close(ctx); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("close sandbox %s: %w", name, err)
			}
		}
	}
	return firstErr
}

// GetAppSandboxCount returns the number of active sandboxes for an app.
func (mgr *SandboxManager) GetAppSandboxCount(appName string) int {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	sbs, ok := mgr.appInstances[appName]
	if !ok {
		return 0
	}
	return len(sbs)
}

// ---------------------------------------------------------------------------
// Refresh loop (called by Daemon, not a background goroutine here)
// ---------------------------------------------------------------------------

// SetRefreshInterval configures the interval used by RefreshAll.
// Setting to 0 disables refresh.
func (mgr *SandboxManager) SetRefreshInterval(d time.Duration) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()
	mgr.refreshInterval = d
}

// RefreshInterval returns the configured refresh interval.
func (mgr *SandboxManager) RefreshInterval() time.Duration {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return mgr.refreshInterval
}

// RefreshAll iterates over all sandbox instances and calls Refresh on those
// that implement the Refreshable interface. Called periodically by the Daemon.
//
// This is NOT a background goroutine. The Daemon is responsible for calling
// this at the configured interval (e.g., via time.Ticker in Daemon.Run()).
func (mgr *SandboxManager) RefreshAll(ctx context.Context) {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	for appName, sbs := range mgr.appInstances {
		for sbName, sb := range sbs {
			r, ok := sb.(Refreshable)
			if !ok {
				continue
			}
			if err := r.Refresh(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "[sandbox] refresh %s/%s: %v\n", appName, sbName, err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Utility
// ---------------------------------------------------------------------------

// ResolveFallbackProfile determines the profile name for a tool that has no
// explicit sandbox mapping. It implements the three-layer fallback:
//
//  1. App-level default (appDefaultSandboxName → lookup profile)
//  2. Daemon defaults.builtin (or defaults.process based on driver)
//  3. Empty string (no sandbox available)
func (mgr *SandboxManager) ResolveFallbackProfile(appDefaultProfile, driver string, defaults *DefaultMappings) string {
	if appDefaultProfile != "" {
		return appDefaultProfile
	}
	if defaults != nil {
		switch driver {
		case "builtin":
			return defaults.Builtin.Profile
		case "process", "http":
			return defaults.Process.Profile
		}
	}
	return ""
}
