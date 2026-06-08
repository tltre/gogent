// Package credentials provides shared credential resolution for all daemon
// subsystems (tool, sandbox, etc.). It consolidates the credentials.yaml
// loading, ${VAR} resolution, and live-reload logic that was previously
// duplicated across packages.
package credentials

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Resolver
// ---------------------------------------------------------------------------

// Resolver resolves ${VAR} references from a credential map first,
// then falls back to os.Getenv. Thread-safe.
type Resolver struct {
	mu    sync.RWMutex
	creds map[string]string
	path  string // credentials.yaml path, set by SetCredentialsPath
}

// NewResolver creates a resolver backed by the given credential map.
// If nil, only environment variables are used.
func NewResolver(creds map[string]string) *Resolver {
	if creds == nil {
		creds = make(map[string]string)
	}
	return &Resolver{creds: creds}
}

// SetCredentialsPath sets the credentials file path for fsnotify watching.
func (r *Resolver) SetCredentialsPath(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.path = path
}

// Update atomically replaces the credential map (used by fsnotify reload).
func (r *Resolver) Update(creds map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creds = creds
}

// Resolve substitutes ${VAR} or $VAR in a string using the credential map
// first, then falling back to os.Getenv.
func (r *Resolver) Resolve(input string) string {
	return os.Expand(input, func(name string) string {
		r.mu.RLock()
		v, ok := r.creds[name]
		r.mu.RUnlock()
		if ok {
			return v
		}
		return os.Getenv(name)
	})
}

// ResolveMap resolves all values in a map using Resolve.
func (r *Resolver) ResolveMap(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = os.Expand(v, func(name string) string {
			if vv, ok := r.creds[name]; ok {
				return vv
			}
			return os.Getenv(name)
		})
	}
	return result
}

// ---------------------------------------------------------------------------
// Credentials file loading
// ---------------------------------------------------------------------------

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
// Returns an empty map if the file doesn't exist.
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

// IsEnvVarRef checks if a value is already a ${...} reference.
func IsEnvVarRef(val string) bool {
	return strings.HasPrefix(val, "${")
}

// EnvVarRef creates a ${tool-name.KEY} reference string.
func EnvVarRef(toolName, key string) string {
	return fmt.Sprintf("${%s.%s}", toolName, key)
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
	creds, err := LoadCredentials(path)
	if err != nil {
		return err
	}

	// Merge new entries (preserve existing)
	for k, v := range entries {
		if _, exists := creds[k]; !exists {
			creds[k] = v
		}
	}

	// Convert back to flat YAML
	outData, err := yaml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	if err := os.WriteFile(path, outData, 0600); err != nil {
		return fmt.Errorf("write credentials: %w", err)
	}

	return nil
}

// DeleteCredentials removes a key from the credentials file.
func DeleteCredentials(path, key string) error {
	if path == "" {
		path = DefaultCredentialsPath()
	}
	if path == "" {
		return fmt.Errorf("cannot determine home directory")
	}

	creds, err := LoadCredentials(path)
	if err != nil {
		return err
	}

	delete(creds, key)

	outData, err := yaml.Marshal(creds)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	return os.WriteFile(path, outData, 0600)
}

// RemoveCredentialsGroup removes all credentials for a tool prefix.
// In the grouped YAML format (tool-name: { KEY: value }), this removes
// the entire tool-name section.
func RemoveCredentialsGroup(path, toolName string) error {
	if path == "" {
		path = DefaultCredentialsPath()
	}
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read credentials for remove: %w", err)
	}

	var existing map[string]any
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &existing); err != nil {
			return nil
		}
	}

	if _, exists := existing[toolName]; !exists {
		return nil
	}

	delete(existing, toolName)

	out, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	return os.WriteFile(path, out, 0600)
}
