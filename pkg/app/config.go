package app

import (
	"fmt"
	"time"

	internal_otel "github.com/tltre/gogent/internal/otel"
	"gopkg.in/yaml.v3"
)

func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	return &cfg, nil
}

// ToolManifestEntry declares a tool the app intends to use.
// The sandbox field is optional — if empty, the daemon applies default
// fallback routing (app default → daemon defaults).
type ToolManifestEntry struct {
	Name          string `yaml:"name"`
	SecurityLevel int    `yaml:"securityLevel,omitempty"`
	Sandbox       string `yaml:"sandbox,omitempty"` // v0.13.3: sandbox instance name
}

// SandboxEntry declares a sandbox instance the app needs.
// The profile must reference a profile defined in the daemon's sandbox.yaml.
type SandboxEntry struct {
	Name            string   `yaml:"name"`
	Profile         string   `yaml:"profile"`
	WorkDir         string   `yaml:"workDir,omitempty"`
	AllowedCommands []string `yaml:"allowedCommands,omitempty"`
}

// DefaultSandboxConfig defines the app-level default sandbox.
// Tools without an explicit sandbox mapping fall back to this value,
// which then falls back to the daemon's sandbox.yaml defaults.
type DefaultSandboxConfig struct {
	Sandbox string `yaml:"sandbox,omitempty"` // sandbox instance name
}

// ProviderSection is the top-level provider configuration (v0.15.x).
// Built-in engines are always registered (minus Exclude); external
// suppliers listed in Servers are connected via gRPC endpoint and
// registered into the same ProviderManager.
type ProviderSection struct {
	Exclude []string               `yaml:"exclude,omitempty"` // exclude built-in engines by name
	Servers []ProviderServerConfig `yaml:"servers,omitempty"` // external gRPC suppliers
}

// ProviderServerConfig describes an external provider service connected
// over gRPC. The server must expose gogent.v1.ProviderService at Endpoint.
// Name must not collide with a built-in engine name (reserved namespace).
type ProviderServerConfig struct {
	Name     string `yaml:"name"`
	Endpoint string `yaml:"endpoint,omitempty"` // gRPC target, e.g. "localhost:9092"
}

type Config struct {
	Name          string                `yaml:"name"`
	Version       string                `yaml:"version"`
	Interface     InterfaceConfig       `yaml:"interface,omitempty"`
	Provider      ProviderSection       `yaml:"provider,omitempty"` // v0.15.x top-level provider config
	Components    []ComponentConfig     `yaml:"components,omitempty"`
	Defaults      map[string]string     `yaml:"defaults,omitempty"`
	Observability ObservabilityConfig   `yaml:"observability,omitempty"`
	Tools         []ToolManifestEntry   `yaml:"tools,omitempty"`
	Sandboxes     []SandboxEntry        `yaml:"sandboxes,omitempty"`      // v0.13.3
	Default       *DefaultSandboxConfig `yaml:"default,omitempty"`        // v0.13.3
}

type ObservabilityConfig struct {
	OTel internal_otel.Config `yaml:"otel,omitempty"`
}

type InterfaceConfig struct {
	Type string     `yaml:"type"` // "cli" | "tui" | "http"
	CLI  CLIConfig  `yaml:"cli,omitempty"`
	HTTP HTTPConfig `yaml:"http,omitempty"`
}

type CLIConfig struct {
	Prompt string `yaml:"prompt,omitempty"`
	Banner string `yaml:"banner,omitempty"`
}

type HTTPConfig struct {
	Port int `yaml:"port,omitempty"`
}

type ComponentConfig struct {
	Name         string            `yaml:"name"`
	Type         string            `yaml:"type"`
	Driver       string            `yaml:"driver"`
	Config       map[string]any    `yaml:"config,omitempty"`
	Dependencies map[string]string `yaml:"dependencies,omitempty"`
}

type ChannelConfig struct {
	BufferSize int           `yaml:"bufferSize,omitempty"`
	Endpoint   string        `yaml:"endpoint,omitempty"`
	Command    []string      `yaml:"command,omitempty"`
	Timeout    time.Duration `yaml:"timeout,omitempty"`
}

type ToolConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	Command  []string      `yaml:"command,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
	Tools    []ToolItem    `yaml:"tools,omitempty"`
}

type ToolItem struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type HookConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	Events   []string      `yaml:"events,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

type EventBusConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

type ContextManagerConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

type MemoryConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

type SandboxConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
	Limits   LimitsConfig  `yaml:"limits,omitempty"`
}

type LimitsConfig struct {
	MaxMemoryMB   int    `yaml:"maxMemoryMB,omitempty"`
	MaxCPUTime    string `yaml:"maxCPUTime,omitempty"`
	MaxFileSize   int64  `yaml:"maxFileSize,omitempty"`
	NetworkAccess bool   `yaml:"networkAccess,omitempty"`
}
