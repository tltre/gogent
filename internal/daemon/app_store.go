package daemon

import (
	"fmt"
	"sync"

	"github.com/tltre/gogent/internal/api"
)

// AppStore is a concurrent-safe in-memory registry for api.AppInfo entries.
type AppStore struct {
	mu   sync.RWMutex
	apps map[string]*api.AppInfo
}

// NewAppStore creates a new empty AppStore.
func NewAppStore() *AppStore {
	return &AppStore{
		apps: make(map[string]*api.AppInfo),
	}
}

// Register adds an app to the store.
func (s *AppStore) Register(info *api.AppInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := info.Name
	if _, exists := s.apps[name]; exists {
		return fmt.Errorf("app %s already registered", name)
	}

	s.apps[name] = info
	return nil
}

// Unregister removes an app from the store by name.
func (s *AppStore) Unregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.apps[name]; !exists {
		return fmt.Errorf("app %s not found", name)
	}

	delete(s.apps, name)
	return nil
}

// Get retrieves an app by name.
func (s *AppStore) Get(name string) (*api.AppInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.apps[name]
	return info, exists
}

// List returns a copy of all registered AppInfo entries.
func (s *AppStore) List() []api.AppInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]api.AppInfo, 0, len(s.apps))
	for _, info := range s.apps {
		result = append(result, *info)
	}
	return result
}

// UpdateStatus updates the status field of a registered app.
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

// UpdatePID updates the PID field of a registered app.
func (s *AppStore) UpdatePID(name string, pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.apps[name]
	if !exists {
		return fmt.Errorf("app %s not found", name)
	}

	info.PID = pid
	return nil
}

// Count returns the number of registered apps.
func (s *AppStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.apps)
}
