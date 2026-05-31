package tool

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultCredentialsPath returns ~/.gogent/credentials.yaml.
func DefaultCredentialsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".gogent", "credentials.yaml")
}

// LoadCredentials reads a YAML credentials file and returns a flat key-value map.
// Supports both flat and grouped structures:
//
//	flat:   KEY: value
//	group:  tool-name: { KEY: value } → flattened to "tool-name.KEY": value
//
// Returns an empty map if the file doesn't exist (creates it with a warning).
func LoadCredentials(path string) (map[string]string, error) {
	if path == "" {
		path = DefaultCredentialsPath()
	}
	if path == "" {
		return make(map[string]string), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Create empty file on first use
			dir := filepath.Dir(path)
			if err := os.MkdirAll(dir, 0700); err != nil {
				return make(map[string]string), nil
			}
			if wErr := os.WriteFile(path, []byte("{}\n"), 0600); wErr != nil {
				return make(map[string]string), nil
			}
			fmt.Fprintf(os.Stderr, "[cred] created %s (add your secrets here)\n", path)
			return make(map[string]string), nil
		}
		return nil, fmt.Errorf("read credentials: %w", err)
	}

	return parseCredentials(data)
}

// parseCredentials unmarshals YAML and flattens grouped keys.
func parseCredentials(data []byte) (map[string]string, error) {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}

	result := make(map[string]string)
	flatten("", raw, result)
	return result, nil
}

// flatten recursively converts a nested YAML structure into dot-separated keys.
func flatten(prefix string, input map[string]any, output map[string]string) {
	for k, v := range input {
		fullKey := k
		if prefix != "" {
			fullKey = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			flatten(fullKey, val, output)
		case string:
			output[fullKey] = val
		default:
			// Non-string values are ignored
		}
	}
}

// WriteCredentials merges new entries into the credentials file.
// Existing keys are preserved; only new keys are added (no overwrite).
func WriteCredentials(path string, entries map[string]string) error {
	if path == "" {
		path = DefaultCredentialsPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	// Read existing
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read credentials for write: %w", err)
	}

	var existing map[string]any
	if len(data) > 0 {
		yaml.Unmarshal(data, &existing) // ignore parse errors, treat as empty
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	// Merge new entries (no overwrite)
	for k, v := range entries {
		if _, ok := existing[k]; !ok {
			setNested(existing, k, v)
		}
	}

	out, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	return os.WriteFile(path, out, 0600)
}

// setNested sets a value in a nested map using a dot-separated key.
// e.g., "my-mcp.API_KEY" → map["my-mcp"]["API_KEY"] = value
func setNested(m map[string]any, key string, value string) {
	parts := splitN(key, ".", 2)
	if len(parts) == 1 {
		// Handle top-level keys that contain dots (e.g. "API_KEY")
		if _, exists := m[parts[0]]; exists {
			return // don't overwrite existing top-level key
		}
		// Check if there's already a nested map at this key
		if _, isMap := m[parts[0]].(map[string]any); isMap {
			return // don't convert map to string
		}
		m[parts[0]] = value
		return
	}

	group := parts[0]
	rest := parts[1]

	sub, ok := m[group].(map[string]any)
	if !ok {
		sub = make(map[string]any)
		m[group] = sub
	}
	if _, exists := sub[rest]; !exists {
		sub[rest] = value
	}
}

func splitN(s, sep string, n int) []string {
	result := make([]string, 0, n)
	start := 0
	for i := 0; i < len(s) && len(result) < n-1; i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}
