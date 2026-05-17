package mgmt

import (
	"fmt"
	"sync"
)

// ComponentStore is a concurrent-safe in-memory registry for ComponentInfo entries.
type ComponentStore struct {
	mu   sync.RWMutex
	comps map[string]*ComponentInfo
}

// NewComponentStore creates a new empty ComponentStore.
func NewComponentStore() *ComponentStore {
	return &ComponentStore{
		comps: make(map[string]*ComponentInfo),
	}
}

// Register adds a component to the store. Returns an error if a component with
// the same name already exists.
func (s *ComponentStore) Register(info *ComponentInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := info.Name
	if _, exists := s.comps[name]; exists {
		return fmt.Errorf("component %s already registered", name)
	}

	s.comps[name] = info
	return nil
}

// Unregister removes a component from the store by name. Returns an error if
// the component is not found.
func (s *ComponentStore) Unregister(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.comps[name]; !exists {
		return fmt.Errorf("component %s not found", name)
	}

	delete(s.comps, name)
	return nil
}

// Get retrieves a component by name. Returns the ComponentInfo pointer and true
// if found, or nil and false otherwise.
func (s *ComponentStore) Get(name string) (*ComponentInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.comps[name]
	return info, exists
}

// List returns a copy of all registered ComponentInfo entries. The returned
// slice is safe to modify by the caller.
func (s *ComponentStore) List() []ComponentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]ComponentInfo, 0, len(s.comps))
	for _, info := range s.comps {
		result = append(result, *info)
	}
	return result
}

// ListByApp returns a copy of all ComponentInfo entries belonging to the given
// app. If appName is empty, returns components with no owning app (centralized).
func (s *ComponentStore) ListByApp(appName string) []ComponentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ComponentInfo
	for _, info := range s.comps {
		if info.AppName == appName {
			result = append(result, *info)
		}
	}
	return result
}

// ListByType returns a copy of all ComponentInfo entries matching the given
// component type (e.g. "provider", "tool").
func (s *ComponentStore) ListByType(typ string) []ComponentInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []ComponentInfo
	for _, info := range s.comps {
		if info.Type == typ {
			result = append(result, *info)
		}
	}
	return result
}

// UpdateStatus updates the status field of a registered component. Returns an
// error if the component is not found.
func (s *ComponentStore) UpdateStatus(name string, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.comps[name]
	if !exists {
		return fmt.Errorf("component %s not found", name)
	}

	info.Status = status
	return nil
}

// UpdatePID updates the PID field of a registered component. Returns an error
// if the component is not found.
func (s *ComponentStore) UpdatePID(name string, pid int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.comps[name]
	if !exists {
		return fmt.Errorf("component %s not found", name)
	}

	info.PID = pid
	return nil
}

// AppendApp appends an app name to a component's Apps list. Returns an error
// if the component is not found. Does nothing if the app is already present.
func (s *ComponentStore) AppendApp(name, appName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.comps[name]
	if !exists {
		return fmt.Errorf("component %s not found", name)
	}

	for _, a := range info.Apps {
		if a == appName {
			return nil // already present
		}
	}
	info.Apps = append(info.Apps, appName)
	return nil
}

// RemoveApp removes an app name from a component's Apps list. Returns an error
// if the component is not found. Returns the remaining count of apps using
// the component (or -1 if not found).
func (s *ComponentStore) RemoveApp(name, appName string) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.comps[name]
	if !exists {
		return -1
	}

	updated := make([]string, 0, len(info.Apps))
	for _, a := range info.Apps {
		if a != appName {
			updated = append(updated, a)
		}
	}
	info.Apps = updated
	return len(updated)
}
