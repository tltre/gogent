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

type ToolManifestEntry struct {
	Name          string `yaml:"name"`
	SecurityLevel int    `yaml:"securityLevel,omitempty"`
}

type Config struct {
	Name          string              `yaml:"name"`
	Version       string              `yaml:"version"`
	Interface     InterfaceConfig     `yaml:"interface,omitempty"`
	Components    []ComponentConfig   `yaml:"components,omitempty"`
	Defaults      map[string]string   `yaml:"defaults,omitempty"`
	Observability ObservabilityConfig `yaml:"observability,omitempty"`
	Tools         []ToolManifestEntry `yaml:"tools,omitempty"`
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

type ProviderConfig struct {
	Endpoint string        `yaml:"endpoint,omitempty"`
	ApiKey   string        `yaml:"apiKey,omitempty"`
	Model    string        `yaml:"model,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
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
