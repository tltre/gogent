package daemon

import (
	"fmt"
	"sync"

	"github.com/tltre/gogent/internal/api"
)

// ComponentStore is a concurrent-safe in-memory registry for api.ComponentInfo entries.
type ComponentStore struct {
	mu    sync.RWMutex
	comps map[string]*api.ComponentInfo
}

// NewComponentStore creates a new empty ComponentStore.
func NewComponentStore() *ComponentStore {
	return &ComponentStore{
		comps: make(map[string]*api.ComponentInfo),
	}
}

// Register adds a component to the store.
func (s *ComponentStore) Register(info *api.ComponentInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := info.Name
	if _, exists := s.comps[name]; exists {
		return fmt.Errorf("component %s already registered", name)
	}

	s.comps[name] = info
	return nil
}

// Unregister removes a component from the store by name.
func (s *ComponentStore) Unregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.comps[name]; !exists {
		return fmt.Errorf("component %s not found", name)
	}

	delete(s.comps, name)
	return nil
}

// Get retrieves a component by name.
func (s *ComponentStore) Get(name string) (*api.ComponentInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.comps[name]
	return info, exists
}

// List returns a copy of all registered ComponentInfo entries.
func (s *ComponentStore) List() []api.ComponentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]api.ComponentInfo, 0, len(s.comps))
	for _, info := range s.comps {
		result = append(result, *info)
	}
	return result
}

// ListByApp returns a copy of all ComponentInfo entries belonging to the given app.
func (s *ComponentStore) ListByApp(appName string) []api.ComponentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []api.ComponentInfo
	for _, info := range s.comps {
		if info.AppName == appName {
			result = append(result, *info)
		}
	}
	return result
}

// ListByType returns a copy of all ComponentInfo entries matching the given type.
func (s *ComponentStore) ListByType(typ string) []api.ComponentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []api.ComponentInfo
	for _, info := range s.comps {
		if info.Type == typ {
			result = append(result, *info)
		}
	}
	return result
}

// AppendApp adds an app name to the component's Apps list.
func (s *ComponentStore) AppendApp(name, appName string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.comps[name]
	if !exists {
		return
	}
	for _, a := range info.Apps {
		if a == appName {
			return
		}
	}
	info.Apps = append(info.Apps, appName)
}

// RemoveApp removes an app name from the component's Apps list.
// Returns the number of remaining apps.
func (s *ComponentStore) RemoveApp(name, appName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.comps[name]
	if !exists {
		return 0
	}
	remaining := make([]string, 0, len(info.Apps))
	for _, a := range info.Apps {
		if a != appName {
			remaining = append(remaining, a)
		}
	}
	info.Apps = remaining
	return len(remaining)
}
