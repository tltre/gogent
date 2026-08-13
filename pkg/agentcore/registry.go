package agentcore

import (
	"fmt"
	"sort"
	"sync"
)

// ErrUnknownAgentType is returned when CreateAgent is called with a type that
// is not registered in the agent type registry.
var ErrUnknownAgentType = fmt.Errorf("unknown agent core type")

// ErrAgentTypeAlreadyRegistered is returned when RegisterAgentType is called
// with a type that is already registered.
var ErrAgentTypeAlreadyRegistered = fmt.Errorf("agent type already registered")

// AgentFactory creates an IAgentCore implementation for a given agent type.
type AgentFactory func() IAgentCore

var agentRegistry = struct {
	sync.RWMutex
	factories map[string]AgentFactory
}{
	factories: make(map[string]AgentFactory),
}

// RegisterAgentType registers an agent factory under the given type name
// (e.g. "react"). Returns ErrAgentTypeAlreadyRegistered if the type exists.
func RegisterAgentType(name string, f AgentFactory) error {
	agentRegistry.Lock()
	defer agentRegistry.Unlock()

	if _, exists := agentRegistry.factories[name]; exists {
		return fmt.Errorf("%w: %s", ErrAgentTypeAlreadyRegistered, name)
	}
	agentRegistry.factories[name] = f
	return nil
}

// CreateAgent instantiates an IAgentCore for the named type.
// Returns ErrUnknownAgentType if the type is not registered.
func CreateAgent(name string) (IAgentCore, error) {
	agentRegistry.RLock()
	f, ok := agentRegistry.factories[name]
	agentRegistry.RUnlock()

	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAgentType, name)
	}
	return f(), nil
}

// RegisteredAgentTypes returns the sorted list of registered agent type names.
func RegisteredAgentTypes() []string {
	agentRegistry.RLock()
	defer agentRegistry.RUnlock()

	names := make([]string, 0, len(agentRegistry.factories))
	for name := range agentRegistry.factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
