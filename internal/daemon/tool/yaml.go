package tool

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// toolsYaml represents the on-disk structure of tools.yaml.
type toolsYaml struct {
	Tools map[string]toolYamlEntry `yaml:"tools"`
}

type toolYamlEntry struct {
	Driver      string            `yaml:"driver"`
	Command     string            `yaml:"command,omitempty"`
	Endpoint    string            `yaml:"endpoint,omitempty"`
	DefaultLvl  int               `yaml:"defaultLevel"`
	Description string            `yaml:"description"`
	Env         map[string]string `yaml:"env,omitempty"`
}

// DefaultBuiltinTools returns the 5 built-in tool definitions.
// These serve as defaults when tools.yaml doesn't exist.
func DefaultBuiltinTools() []ToolDefinition {
	return []ToolDefinition{
		{Name: "calculator", Driver: string(DriverBuiltin), DefaultLvl: 0, Description: "Evaluate mathematical expressions"},
		{Name: "think", Driver: string(DriverBuiltin), DefaultLvl: 0, Description: "Let the agent do step-by-step reasoning"},
		{Name: "todo", Driver: string(DriverBuiltin), DefaultLvl: 0, Description: "Manage todo items within a session"},
		{Name: "filesystem.read", Driver: string(DriverBuiltin), DefaultLvl: 1, Description: "Read files from the filesystem"},
		{Name: "shell", Driver: string(DriverBuiltin), DefaultLvl: 2, Description: "Execute shell commands in sandbox"},
	}
}

// toolYamlPath returns ~/.gogent/tools.yaml.
func toolYamlPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gogent", "tools.yaml")
}

// LoadDefault registers the 5 built-in tool definitions into the registry.
func (r *ToolRegistry) LoadDefault() {
	for _, def := range DefaultBuiltinTools() {
		r.MustRegister(&def)
	}
}

// LoadFromFile reads ~/.gogent/tools.yaml and registers tool definitions.
// If the file doesn't exist, it creates one with default built-in tools.
// File entries take precedence over already-registered defaults.
// If resolver is non-nil, env values are resolved from credentials before storage.
func (r *ToolRegistry) LoadFromFile(resolver *EnvResolver) error {
	path := toolYamlPath()
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r.writeDefaults(path)
		}
		return fmt.Errorf("read tools.yaml: %w", err)
	}

	var doc toolsYaml
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse tools.yaml: %w", err)
	}

	for name, entry := range doc.Tools {
		// Registry always stores raw ${VAR} references, not resolved values.
		// Resolution happens at execution time via EnvResolver.ResolveMap().
		def := &ToolDefinition{
			Name:        name,
			Driver:      entry.Driver,
			Command:     entry.Command,
			Endpoint:    entry.Endpoint,
			DefaultLvl:  entry.DefaultLvl,
			Description: entry.Description,
			Env:         entry.Env,
		}
		if err := r.registerOrUpdate(def); err != nil {
			fmt.Fprintf(os.Stderr, "[tool] warn: skip tool %q: %v\n", name, err)
		}
	}

	return nil
}

// writeDefaults creates ~/.gogent/tools.yaml with built-in tools.
func (r *ToolRegistry) writeDefaults(path string) error {
	doc := toolsYaml{
		Tools: make(map[string]toolYamlEntry),
	}
	for _, def := range DefaultBuiltinTools() {
		doc.Tools[def.Name] = toolYamlEntry{
			Driver:      def.Driver,
			DefaultLvl:  def.DefaultLvl,
			Description: def.Description,
		}
	}

	data, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal default tools: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}

	return nil
}
