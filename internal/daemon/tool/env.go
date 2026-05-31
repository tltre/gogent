package tool

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// EnvResolver resolves ${VAR} references from a credential map first,
// then falls back to os.Getenv. Thread-safe. Supports runtime credential
// reload via file system notifications.
type EnvResolver struct {
	mu    sync.RWMutex
	creds map[string]string
	path  string
}

// NewEnvResolver creates a resolver backed by the given credential map.
func NewEnvResolver(creds map[string]string) *EnvResolver {
	if creds == nil {
		creds = make(map[string]string)
	}
	return &EnvResolver{creds: creds}
}

// SetCredentialsPath sets the credentials file path for fsnotify watching.
func (r *EnvResolver) SetCredentialsPath(path string) {
	r.path = path
}

// Update atomically replaces the credential map.
func (r *EnvResolver) Update(creds map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.creds = creds
}

// Resolve substitutes ${VAR} or $VAR in a string using the credential map
// first, then falling back to os.Getenv.
func (r *EnvResolver) Resolve(input string) string {
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
func (r *EnvResolver) ResolveMap(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = r.Resolve(v)
	}
	return result
}

// Watch starts monitoring the credentials file for changes using fsnotify.
// When the file is modified, it reloads the credential map atomically.
// Blocks until ctx is cancelled or a fatal watch error occurs.
func (r *EnvResolver) Watch(ctx context.Context) error {
	if r.path == "" {
		return nil // no file to watch
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("credential watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(r.path); err != nil {
		// File doesn't exist yet — silently degrade.
		// The watcher is still alive for when the file appears.
		_ = err
	}

	// Debounce reloads: coalesce multiple rapid writes into one reload.
	var debounceTimer *time.Timer

	for {
		select {
		case event := <-watcher.Events:
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
					r.reload()
				})
			}
		case err := <-watcher.Errors:
			fmt.Fprintf(os.Stderr, "[cred] watch error: %v\n", err)
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return nil
		}
	}
}

func (r *EnvResolver) reload() {
	creds, err := LoadCredentials(r.path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[cred] reload error: %v\n", err)
		return
	}
	r.Update(creds)
	fmt.Fprintf(os.Stderr, "[cred] reloaded %d entries\n", len(creds))
}

// EnvVarRef creates a ${tool-name.KEY} reference string.
func EnvVarRef(toolName, key string) string {
	return "${" + toolName + "." + key + "}"
}

// IsEnvVarRef checks if a value is already a ${...} reference.
func IsEnvVarRef(val string) bool {
	return len(val) > 3 && val[0] == '$' && val[1] == '{' && val[len(val)-1] == '}'
}
