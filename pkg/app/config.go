package app

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

func ParseConfig(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	return &cfg, nil
}

type Config struct {
	Name       string            `yaml:"name"`
	Version    string            `yaml:"version"`
	Components []ComponentConfig `yaml:"components"`
	Defaults   map[string]string `yaml:"defaults,omitempty"`
}

type ComponentConfig struct {
	Name         string                   `yaml:"name"`
	Type         string                   `yaml:"type"`
	Driver       string                   `yaml:"driver"`
	Config       map[string]any           `yaml:"config,omitempty"`
	Dependencies map[string]DependencyRef `yaml:"dependencies,omitempty"`
}

type DependencyRef struct {
	Name string `yaml:"name"`
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
