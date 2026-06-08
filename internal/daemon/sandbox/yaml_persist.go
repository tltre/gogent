package sandbox

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// sandbox.yaml read/write helpers
// ---------------------------------------------------------------------------

// readSandboxFile reads and parses the sandbox.yaml at the given path.
// Returns an empty SandboxFile if the file doesn't exist.
func readSandboxFile(path string) (*SandboxFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SandboxFile{
				Providers: make(map[string]ProviderEntry),
				Profiles:  make(map[string]ProfileEntry),
			}, nil
		}
		return nil, fmt.Errorf("read sandbox.yaml: %w", err)
	}

	var sf SandboxFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("parse sandbox.yaml: %w", err)
	}
	if sf.Providers == nil {
		sf.Providers = make(map[string]ProviderEntry)
	}
	if sf.Profiles == nil {
		sf.Profiles = make(map[string]ProfileEntry)
	}
	return &sf, nil
}

// writeSandboxFile serializes a SandboxFile to YAML and writes it.
func writeSandboxFile(path string, sf *SandboxFile) error {
	if sf == nil {
		return fmt.Errorf("cannot write nil SandboxFile")
	}
	if sf.Providers == nil {
		sf.Providers = make(map[string]ProviderEntry)
	}
	if sf.Profiles == nil {
		sf.Profiles = make(map[string]ProfileEntry)
	}

	// Build the output structure matching sandbox.yaml schema
	out := struct {
		Providers map[string]ProviderEntry `yaml:"sandbox-providers"`
		Profiles  map[string]ProfileEntry  `yaml:"sandbox-profiles"`
		Defaults  *DefaultMappingsEntry    `yaml:"defaults,omitempty"`
	}{
		Providers: sf.Providers,
		Profiles:  sf.Profiles,
		Defaults:  sf.Defaults,
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return fmt.Errorf("marshal sandbox.yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write sandbox.yaml: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Provider persistence
// ---------------------------------------------------------------------------

// WriteProviderToFile adds or updates a provider entry in sandbox.yaml.
func WriteProviderToFile(path, name string, entry ProviderEntry) error {
	if path == "" {
		path = sandboxYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine sandbox.yaml path")
	}

	sf, err := readSandboxFile(path)
	if err != nil {
		return err
	}

	sf.Providers[name] = entry
	return writeSandboxFile(path, sf)
}

// RemoveProviderFromFile deletes a provider entry from sandbox.yaml.
func RemoveProviderFromFile(path, name string) error {
	if path == "" {
		path = sandboxYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine sandbox.yaml path")
	}

	sf, err := readSandboxFile(path)
	if err != nil {
		return err
	}

	delete(sf.Providers, name)
	return writeSandboxFile(path, sf)
}

// ---------------------------------------------------------------------------
// Profile persistence
// ---------------------------------------------------------------------------

// WriteProfileToFile adds or updates a profile entry in sandbox.yaml.
func WriteProfileToFile(path, name string, entry ProfileEntry) error {
	if path == "" {
		path = sandboxYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine sandbox.yaml path")
	}

	sf, err := readSandboxFile(path)
	if err != nil {
		return err
	}

	sf.Profiles[name] = entry
	return writeSandboxFile(path, sf)
}

// RemoveProfileFromFile deletes a profile entry from sandbox.yaml.
func RemoveProfileFromFile(path, name string) error {
	if path == "" {
		path = sandboxYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine sandbox.yaml path")
	}

	sf, err := readSandboxFile(path)
	if err != nil {
		return err
	}

	delete(sf.Profiles, name)
	return writeSandboxFile(path, sf)
}

// ---------------------------------------------------------------------------
// Defaults persistence
// ---------------------------------------------------------------------------

// WriteDefaultsToFile updates the defaults section in sandbox.yaml.
func WriteDefaultsToFile(path string, defaults *DefaultMappingsEntry) error {
	if path == "" {
		path = sandboxYamlPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine sandbox.yaml path")
	}

	sf, err := readSandboxFile(path)
	if err != nil {
		return err
	}

	sf.Defaults = defaults
	return writeSandboxFile(path, sf)
}
