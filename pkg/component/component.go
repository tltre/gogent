package component

import "context"

type ComponentType string

const (
	ComponentChannel        ComponentType = "channel"
	ComponentAgentCore      ComponentType = "agentcore"
	ComponentProvider       ComponentType = "provider"
	ComponentTool           ComponentType = "tool"
	ComponentHook           ComponentType = "hook"
	ComponentEventBus       ComponentType = "eventbus"
	ComponentContextManager ComponentType = "contextmanager"
	ComponentMemory         ComponentType = "memory"
	ComponentSandbox        ComponentType = "sandbox"
)

type DependencySpec struct {
	Type     ComponentType
	Name     string
	Required bool
	Multiple bool
}

type Component interface {
	Name() string
	Type() ComponentType
	Initialize(ctx context.Context, deps Dependencies) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Dependencies() map[string]DependencySpec
}

type Dependencies interface {
	Get(name string) Component
	GetByType(typ ComponentType) []Component
	GetDefault(typ ComponentType) Component
}
