package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tltre/gogent/internal/credentials"
	"gopkg.in/yaml.v3"
)

// Resolver is an alias for credentials.Resolver.
type Resolver = credentials.Resolver

// NewResolver creates a credentials resolver.
func NewResolver(creds map[string]string) *Resolver {
	return credentials.NewResolver(creds)
}

// DefaultCredentialsPath returns ~/.gogent/credentials.yaml.
func DefaultCredentialsPath() string {
	return credentials.DefaultCredentialsPath()
}

// LoadCredentials reads a YAML credentials file.
func LoadCredentials(path string) (map[string]string, error) {
	return credentials.LoadCredentials(path)
}

// ---------------------------------------------------------------------------
// On-disk YAML structure
// ---------------------------------------------------------------------------

// SandboxFile represents the full structure of ~/.gogent/sandbox.yaml.
type SandboxFile struct {
	Providers map[string]ProviderEntry `yaml:"sandbox-providers"`
	Profiles  map[string]ProfileEntry  `yaml:"sandbox-profiles"`
	Defaults  *DefaultMappingsEntry    `yaml:"defaults,omitempty"`
}

type ProviderEntry struct {
	Type     string `yaml:"type"`               // "e2b" | "builtin" | "agent-sandbox"
	Endpoint string `yaml:"endpoint,omitempty"` // API base URL
	APIKey   string `yaml:"apiKey,omitempty"`   // API key (may contain ${VAR} refs)
	Token    string `yaml:"token,omitempty"`    // alternative auth token
}

type ProfileEntry struct {
	Provider      string            `yaml:"provider"`
	Template      string            `yaml:"template"`
	Network       *bool             `yaml:"network,omitempty"`
	MaxMemoryMB   int               `yaml:"maxMemoryMB,omitempty"`
	MaxCPUTime    string            `yaml:"maxCPUTime,omitempty"` // duration string like "30s"
	ReadOnlyRoot  bool              `yaml:"readOnlyRoot,omitempty"`
	AllowCommands []string          `yaml:"allowCommands,omitempty"`
	AllowReadPaths []string         `yaml:"allowReadPaths,omitempty"`
	Egress        *EgressEntry      `yaml:"egress,omitempty"`
	Lifecycle     *LifecycleEntry   `yaml:"lifecycle,omitempty"`
	Snapshot      *SnapshotEntry    `yaml:"snapshot,omitempty"`
	E2B           map[string]any    `yaml:"e2b,omitempty"`
}

type EgressEntry struct {
	AllowDomains []string `yaml:"allowDomains,omitempty"`
	DenyDomains  []string `yaml:"denyDomains,omitempty"`
}

type LifecycleEntry struct {
	Mode    string         `yaml:"mode"`               // "persistent" | "ephemeral" | "session"
	Timeout string         `yaml:"timeout,omitempty"`  // duration string
	Snapshot *SnapshotEntry `yaml:"snapshot,omitempty"`
}

type SnapshotEntry struct {
	Enabled bool   `yaml:"enabled"`
	OnStop  string `yaml:"onStop,omitempty"`
	OnStart string `yaml:"onStart,omitempty"`
}

type DefaultMappingsEntry struct {
	Builtin ProfileRefEntry `yaml:"builtin"`
	Process ProfileRefEntry `yaml:"process"`
}

type ProfileRefEntry struct {
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

// ParseSandboxFile unmarshals YAML data into a SandboxFile struct.
// Exported for testing. Use ApplySandboxFile to register into a SandboxManager.
func ParseSandboxFile(data []byte) (*SandboxFile, error) {
	var sf SandboxFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse sandbox.yaml: %w", err)
	}
	return &sf, nil
}

// LoadSandboxFile reads and parses ~/.gogent/sandbox.yaml.
// Returns nil, nil if the file doesn't exist (defaults apply).
func LoadSandboxFile() (*SandboxFile, error) {
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

	var sf SandboxFile
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
func ApplySandboxFile(mgr *SandboxManager, sf *SandboxFile, resolver *Resolver) (*DefaultMappings, error) {
	if sf == nil {
		return nil, nil
	}

	if resolver == nil {
		resolver = NewResolver(nil)
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
			p = &GenericProvider{
				PName:      name,
				PType:      SandboxType(entry.Type),
				PEndpoint:  entry.Endpoint,
				PAPIKey:    apiKey,
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

func toProfile(name string, e ProfileEntry) (*SandboxProfile, error) {
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

func validateProvider(_ string, e ProviderEntry) error {
	if e.Type == "" {
		return fmt.Errorf("type is required")
	}
	switch SandboxType(e.Type) {
	case SandboxE2B:
		// Endpoint is optional for E2B — defaults to https://api.e2b.app
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
// GenericProvider — v0.13.0 placeholder
// ---------------------------------------------------------------------------

// GenericProvider is a placeholder provider that registers metadata
// from sandbox.yaml. Actual backends (E2BProvider) have their own implementations.
type GenericProvider struct {
	PName        string       `yaml:"-"`
	PType        SandboxType  `yaml:"-"`
	PEndpoint    string       `yaml:"-"`
	PAPIKey      string       `yaml:"-"`
}

func (p *GenericProvider) Name() string                   { return p.PName }
func (p *GenericProvider) Type() SandboxType              { return p.PType }
func (p *GenericProvider) Create(_ context.Context, _ *SandboxProfile, _ *SandboxConfig) (ISandbox, error) {
	return nil, fmt.Errorf("sandbox provider %q (%s) not yet implemented", p.PName, p.PType)
}

// ---------------------------------------------------------------------------
// Duration parser
// ---------------------------------------------------------------------------

// parseDuration parses a duration string like "30s", "5m", "1h".
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}
