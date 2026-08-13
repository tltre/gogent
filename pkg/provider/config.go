package provider

import (
	"fmt"
	"sort"
	"sync"
)

// EngineFactory creates a provider instance for a given engine.
//
// Engines are framework built-ins (openai, gemini, anthropic, ...) registered
// via RegisterEngine. The registry follows the database/sql driver pattern:
// engine implementations register themselves, the builder enumerates all
// registered engines and instantiates them into the ProviderManager.
type EngineFactory func() (IProvider, error)

var engineRegistry = struct {
	sync.RWMutex
	factories map[string]EngineFactory
}{
	factories: make(map[string]EngineFactory),
}

// RegisterEngine registers an engine factory under the given engine name.
// Returns ErrEngineAlreadyRegistered if the name is already taken.
func RegisterEngine(name string, f EngineFactory) error {
	engineRegistry.Lock()
	defer engineRegistry.Unlock()

	if _, exists := engineRegistry.factories[name]; exists {
		return fmt.Errorf("%w: %s", ErrEngineAlreadyRegistered, name)
	}
	engineRegistry.factories[name] = f
	return nil
}

// CreateEngine instantiates a provider for the named engine.
// Returns ErrUnknownEngine if the engine is not registered.
func CreateEngine(name string) (IProvider, error) {
	engineRegistry.RLock()
	f, ok := engineRegistry.factories[name]
	engineRegistry.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownEngine, name)
	}
	return f()
}

// UnregisterEngine removes an engine factory by name. Mainly used by tests;
// no-op if the engine is not registered.
func UnregisterEngine(name string) {
	engineRegistry.Lock()
	defer engineRegistry.Unlock()
	delete(engineRegistry.factories, name)
}

// RegisteredEngines returns the sorted list of all registered engine names.
func RegisteredEngines() []string {
	engineRegistry.RLock()
	defer engineRegistry.RUnlock()

	names := make([]string, 0, len(engineRegistry.factories))
	for name := range engineRegistry.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsEngineRegistered reports whether the given engine name is registered.
func IsEngineRegistered(name string) bool {
	engineRegistry.RLock()
	defer engineRegistry.RUnlock()

	_, ok := engineRegistry.factories[name]
	return ok
}
