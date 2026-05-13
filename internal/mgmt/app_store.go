package mgmt

import (
	"fmt"
	"sync"
)

// AppStore is a concurrent-safe in-memory registry for AppInfo entries.
type AppStore struct {
	mu   sync.RWMutex
	apps map[string]*AppInfo
}

// NewAppStore creates a new empty AppStore.
func NewAppStore() *AppStore {
	return &AppStore{
		apps: make(map[string]*AppInfo),
	}
}

// Register adds an app to the store. Returns an error if an app with the same
// name already exists.
func (s *AppStore) Register(info *AppInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := info.Name
	if _, exists := s.apps[name]; exists {
		return fmt.Errorf("app %s already registered", name)
	}

	s.apps[name] = info
	return nil
}

// Unregister removes an app from the store by name. Returns an error if the
// app is not found.
func (s *AppStore) Unregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.apps[name]; !exists {
		return fmt.Errorf("app %s not found", name)
	}

	delete(s.apps, name)
	return nil
}

// Get retrieves an app by name. Returns the AppInfo pointer and true if found,
// or nil and false otherwise.
func (s *AppStore) Get(name string) (*AppInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.apps[name]
	return info, exists
}

// List returns a copy of all registered AppInfo entries. The returned slice is
// safe to modify by the caller.
func (s *AppStore) List() []AppInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]AppInfo, 0, len(s.apps))
	for _, info := range s.apps {
		result = append(result, *info)
	}
	return result
}

// UpdateStatus updates the status field of a registered app. Returns an error
// if the app is not found.
func (s *AppStore) UpdateStatus(name string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.apps[name]
	if !exists {
		return fmt.Errorf("app %s not found", name)
	}

	info.Status = status
	return nil
}

// Count returns the number of registered apps.
func (s *AppStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.apps)
}
