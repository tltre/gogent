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
	ComponentLogger         ComponentType = "logger"
)

type DependencySpec struct {
	Type     ComponentType
	Name     string
	Required bool
	Multiple bool
}

type Component interface {
	GetName() string
	GetType() ComponentType
	Initialize(ctx context.Context, deps *Registry) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Dependencies() map[string]DependencySpec
}

type Dependencies interface {
	Get(name string) Component
	GetByType(typ ComponentType) []Component
	GetDefault(typ ComponentType) Component
}

type BasicComponent struct {
	name string
	reg  *Registry
}

func NewBasicComponent(name string) BasicComponent {
	return BasicComponent{name: name}
}

func (b *BasicComponent) GetName() string      { return b.name }
func (b *BasicComponent) Registry() *Registry  { return b.reg }
func (b *BasicComponent) SetRegistry(r *Registry) { b.reg = r }
