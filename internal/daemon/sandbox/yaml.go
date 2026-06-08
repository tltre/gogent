package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Resolver resolves ${VAR} references from environment variables.
// This is a simplified resolver for v0.13.0. In later versions,
// credentials.yaml can be integrated.
type Resolver struct{}

func NewResolver() *Resolver { return &Resolver{} }

func (r *Resolver) Resolve(input string) string {
	return os.Expand(input, os.Getenv)
}

// ---------------------------------------------------------------------------
// On-disk YAML structure
// ---------------------------------------------------------------------------

// sandboxFile represents the full structure of ~/.gogent/sandbox.yaml.
type sandboxFile struct {
	Providers map[string]providerEntry `yaml:"sandbox-providers"`
	Profiles  map[string]profileEntry  `yaml:"sandbox-profiles"`
	Defaults  *defaultMappingsEntry    `yaml:"defaults,omitempty"`
}

type providerEntry struct {
	Type     string `yaml:"type"`               // "e2b" | "builtin" | "agent-sandbox"
	Endpoint string `yaml:"endpoint,omitempty"` // API base URL
	APIKey   string `yaml:"apiKey,omitempty"`   // API key (may contain ${VAR} refs)
	Token    string `yaml:"token,omitempty"`    // alternative auth token
}

type profileEntry struct {
	Provider      string            `yaml:"provider"`
	Template      string            `yaml:"template"`
	Network       *bool             `yaml:"network,omitempty"`
	MaxMemoryMB   int               `yaml:"maxMemoryMB,omitempty"`
	MaxCPUTime    string            `yaml:"maxCPUTime,omitempty"` // duration string like "30s"
	ReadOnlyRoot  bool              `yaml:"readOnlyRoot,omitempty"`
	AllowCommands []string          `yaml:"allowCommands,omitempty"`
	AllowReadPaths []string         `yaml:"allowReadPaths,omitempty"`
	Egress        *egressEntry      `yaml:"egress,omitempty"`
	Lifecycle     *lifecycleEntry   `yaml:"lifecycle,omitempty"`
	Snapshot      *snapshotEntry    `yaml:"snapshot,omitempty"`
	E2B           map[string]any    `yaml:"e2b,omitempty"`
}

type egressEntry struct {
	AllowDomains []string `yaml:"allowDomains,omitempty"`
	DenyDomains  []string `yaml:"denyDomains,omitempty"`
}

type lifecycleEntry struct {
	Mode    string         `yaml:"mode"`               // "persistent" | "ephemeral" | "session"
	Timeout string         `yaml:"timeout,omitempty"`  // duration string
	Snapshot *snapshotEntry `yaml:"snapshot,omitempty"`
}

type snapshotEntry struct {
	Enabled bool   `yaml:"enabled"`
	OnStop  string `yaml:"onStop,omitempty"`
	OnStart string `yaml:"onStart,omitempty"`
}

type defaultMappingsEntry struct {
	Builtin profileRefEntry `yaml:"builtin"`
	Process profileRefEntry `yaml:"process"`
}

type profileRefEntry struct {
	Profile string `yaml:"profile"`
}

// ---------------------------------------------------------------------------
// Path resolution
// ---------------------------------------------------------------------------

// sandboxYamlPath returns ~/.gogent/sandbox.yaml.
func sandboxYamlPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gogent", "sandbox.yaml")
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

// LoadSandboxFile reads and parses ~/.gogent/sandbox.yaml.
// Returns nil, nil if the file doesn't exist (defaults apply).
func LoadSandboxFile() (*sandboxFile, error) {
	path := sandboxYamlPath()
	if path == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // file doesn't exist, use defaults
		}
		return nil, fmt.Errorf("read sandbox.yaml: %w", err)
	}

	var sf sandboxFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse sandbox.yaml: %w", err)
	}

	return &sf, nil
}

// ApplySandboxFile loads the sandbox.yaml configuration into the SandboxManager.
// It registers providers, profiles, and returns the default mappings.
//
// The resolver is used to resolve ${VAR} references in API keys and tokens.
// If nil, a default resolver (os.Getenv) is used.
func ApplySandboxFile(mgr *SandboxManager, sf *sandboxFile, resolver *Resolver) (*DefaultMappings, error) {
	if sf == nil {
		return nil, nil
	}

	if resolver == nil {
		resolver = NewResolver()
	}

	// 1. Register providers
	for name, entry := range sf.Providers {
		if err := validateProvider(name, entry); err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}

		var p SandboxProvider
		apiKey := resolver.Resolve(entry.APIKey)
		switch SandboxType(entry.Type) {
		case SandboxE2B:
			e2b, err := NewE2BProvider(name, apiKey, entry.Endpoint)
			if err != nil {
				return nil, fmt.Errorf("create E2B provider %q: %w", name, err)
			}
			p = e2b
		default:
			p = &genericProvider{
				name:         name,
				providerType: SandboxType(entry.Type),
				endpoint:     entry.Endpoint,
				apiKey:       apiKey,
			}
		}

		if err := mgr.RegisterProvider(p); err != nil {
			return nil, fmt.Errorf("register provider %q: %w", name, err)
		}
	}

	// 2. Register profiles
	for name, entry := range sf.Profiles {
		profile, err := toProfile(name, entry)
		if err != nil {
			return nil, fmt.Errorf("profile %q: %w", name, err)
		}
		if err := mgr.RegisterProfile(profile); err != nil {
			return nil, fmt.Errorf("register profile %q: %w", name, err)
		}
	}

	// 3. Parse default mappings
	if sf.Defaults == nil {
		return nil, nil
	}

	return &DefaultMappings{
		Builtin: ProfileRef{Profile: sf.Defaults.Builtin.Profile},
		Process: ProfileRef{Profile: sf.Defaults.Process.Profile},
	}, nil
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func toProfile(name string, e profileEntry) (*SandboxProfile, error) {
	p := &SandboxProfile{
		Name:           name,
		Provider:       e.Provider,
		Template:       e.Template,
		Network:        e.Network,
		MaxMemoryMB:    e.MaxMemoryMB,
		ReadOnlyRoot:   e.ReadOnlyRoot,
		AllowCommands:  e.AllowCommands,
		AllowReadPaths: e.AllowReadPaths,
	}

	// Parse duration fields
	if e.MaxCPUTime != "" {
		d, err := parseDuration(e.MaxCPUTime)
		if err != nil {
			return nil, fmt.Errorf("invalid maxCPUTime: %w", err)
		}
		p.MaxCPUTime = d
	}

	// Egress
	if e.Egress != nil {
		p.Egress = &EgressConfig{
			AllowDomains: e.Egress.AllowDomains,
			DenyDomains:  e.Egress.DenyDomains,
		}
	}

	// Lifecycle
	if e.Lifecycle != nil {
		p.Lifecycle.Mode = LifecycleMode(e.Lifecycle.Mode)
		if e.Lifecycle.Timeout != "" {
			d, err := parseDuration(e.Lifecycle.Timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid lifecycle.timeout: %w", err)
			}
			p.Lifecycle.Timeout = d
		}
		if e.Lifecycle.Snapshot != nil {
			p.Snapshot.Enabled = e.Lifecycle.Snapshot.Enabled
		}
	}

	// Snapshot (top-level)
	if e.Snapshot != nil {
		p.Snapshot.Enabled = e.Snapshot.Enabled
		p.Snapshot.OnStop = e.Snapshot.OnStop
		p.Snapshot.OnStart = e.Snapshot.OnStart
	}

	// Passthrough
	p.E2B = e.E2B

	return p, nil
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateProvider(_ string, e providerEntry) error {
	if e.Type == "" {
		return fmt.Errorf("type is required")
	}
	switch SandboxType(e.Type) {
	case SandboxE2B:
		if e.Endpoint == "" {
			return fmt.Errorf("endpoint is required for type %q", e.Type)
		}
		if e.APIKey == "" {
			return fmt.Errorf("apiKey is required for type %q", e.Type)
		}
	case SandboxBuiltin:
		// Builtin needs no endpoint or apiKey
	case SandboxAgent:
		if e.Endpoint == "" {
			return fmt.Errorf("endpoint is required for type %q", e.Type)
		}
	default:
		return fmt.Errorf("unknown provider type %q", e.Type)
	}
	return nil
}

// ---------------------------------------------------------------------------
// genericProvider — v0.13.0 placeholder
// ---------------------------------------------------------------------------

// genericProvider is a placeholder provider that registers the metadata
// from sandbox.yaml. Actual implementation (E2BProvider, etc.) comes in v0.13.1.
type genericProvider struct {
	name         string
	providerType SandboxType
	endpoint     string
	apiKey       string
}

func (p *genericProvider) Name() string                   { return p.name }
func (p *genericProvider) Type() SandboxType              { return p.providerType }
func (p *genericProvider) Create(_ context.Context, _ *SandboxProfile, _ *SandboxConfig) (ISandbox, error) {
	return nil, fmt.Errorf("sandbox provider %q (%s) not yet implemented — planned for v0.13.1", p.name, p.providerType)
}

// ---------------------------------------------------------------------------
// Duration parser
// ---------------------------------------------------------------------------

// parseDuration parses a duration string like "30s", "5m", "1h".
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
