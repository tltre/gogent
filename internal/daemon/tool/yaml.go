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

// LoadDefault registers the 5 built-in tool definitions.
// If a ServerStore is provided, builtins are also registered there.
func (r *ToolRegistry) LoadDefault(store ...*ServerStore) {
	for _, def := range DefaultBuiltinTools() {
		r.MustRegister(&def)
		// Also register in ServerStore if provided
		if len(store) > 0 && store[0] != nil {
			store[0].Add(def.Name, &ServerInfo{
				Name: def.Name, Driver: string(DriverBuiltin), DefaultLvl: def.DefaultLvl,
				Status: StatusActive, ToolCount: 1,
			})
		}
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
		// v0.12.8: builtin tools are managed by LoadDefault, not tools.yaml.
		if entry.Driver == string(DriverBuiltin) {
			continue
		}

		def := &ToolDefinition{
			Name:        name,
			Driver:      entry.Driver,
			Command:     entry.Command,
			Endpoint:    entry.Endpoint,
			DefaultLvl:  entry.DefaultLvl,
			Description: entry.Description,
			Env:         entry.Env,
		}
		if err := r.RegisterOrUpdate(def); err != nil {
			fmt.Fprintf(os.Stderr, "[tool] warn: skip tool %q: %v\n", name, err)
		}
	}

	return nil
}

// WriteToolToFile writes a tool definition to tools.yaml.
// If the tool already exists in the file, it is updated. Otherwise appended.
func WriteToolToFile(path string, def *ToolDefinition) error {
	if path == "" {
		path = toolYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new file with just this tool
			doc := toolsYaml{Tools: make(map[string]toolYamlEntry)}
			doc.Tools[def.Name] = toYamlEntry(def)
			out, _ := yaml.Marshal(&doc)
			dir := filepath.Dir(path)
			os.MkdirAll(dir, 0700)
			return os.WriteFile(path, out, 0600)
		}
		return fmt.Errorf("read tools.yaml for write: %w", err)
	}

	var doc toolsYaml
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse tools.yaml for write: %w", err)
	}
	if doc.Tools == nil {
		doc.Tools = make(map[string]toolYamlEntry)
	}
	doc.Tools[def.Name] = toYamlEntry(def)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal tools.yaml: %w", err)
	}
	return os.WriteFile(path, out, 0600)
}

// RemoveToolFromFile deletes a tool entry from tools.yaml.
func RemoveToolFromFile(path, name string) error {
	if path == "" {
		path = toolYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to clean up
		}
		return fmt.Errorf("read tools.yaml for remove: %w", err)
	}

	var doc toolsYaml
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil // can't parse, skip cleanup
	}

	if _, exists := doc.Tools[name]; !exists {
		return nil // no entry for this tool
	}
	delete(doc.Tools, name)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal tools.yaml after remove: %w", err)
	}
	return os.WriteFile(path, out, 0600)
}

func toYamlEntry(def *ToolDefinition) toolYamlEntry {
	env := def.Env
	if len(env) == 0 {
		env = nil
	}
	return toolYamlEntry{
		Driver:      def.Driver,
		Command:     def.Command,
		Endpoint:    def.Endpoint,
		DefaultLvl:  def.DefaultLvl,
		Description: def.Description,
		Env:         env,
	}
}

// writeDefaults creates an empty ~/.gogent/tools.yaml.
// Built-in tools are managed by LoadDefault and no longer written here.
func (r *ToolRegistry) writeDefaults(path string) error {
	doc := toolsYaml{
		Tools: make(map[string]toolYamlEntry),
	}

	data, err := yaml.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("marshal tools.yaml: %w", err)
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
