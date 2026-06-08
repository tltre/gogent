package credentials

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch starts monitoring the credentials file for changes using fsnotify.
// When the file is modified, it reloads the credential map atomically.
// Blocks until ctx is cancelled or a fatal watch error occurs.
func (r *Resolver) Watch(ctx context.Context) error {
	r.mu.RLock()
	path := r.path
	r.mu.RUnlock()

	if path == "" {
		return nil // no file to watch
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("credential watcher: %w", err)
	}
	defer watcher.Close()

	if err := watcher.Add(path); err != nil {
		// File doesn't exist yet — silently degrade.
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
			return fmt.Errorf("credential watcher error: %w", err)
		case <-ctx.Done():
			return nil
		}
	}
}

// reload re-reads the credentials file and atomically updates the map.
func (r *Resolver) reload() {
	r.mu.RLock()
	path := r.path
	r.mu.RUnlock()

	if path == "" {
		return
	}

	creds, err := LoadCredentials(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[cred] reload failed: %v\n", err)
		return
	}

	r.Update(creds)
	fmt.Fprintf(os.Stderr, "[cred] reloaded %d entries from %s\n", len(creds), path)
}
