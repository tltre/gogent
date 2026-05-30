package tool

// ToolRegistry is a centralized registry for tool definitions managed by
// the daemon. It will be populated in v0.12.x with built-in tools and
// support for process/http-driver tool registration.
//
// Design notes:
//   - Stored in daemon memory, backed by ~/.gogent/tools.yaml
//   - Each tool has a Name, Driver type, SecurityLevel, and optional env vars
//   - Application tools are resolved at runtime via RegisterManifest
type ToolRegistry struct {
	// TODO(v0.12.x): tools map[string]*ToolDefinition
}

// ToolDefinition describes a single tool known to the daemon.
type ToolDefinition struct {
	Name        string `yaml:"name"`
	Driver      string `yaml:"driver"`      // "builtin" | "process" | "http"
	Command     string `yaml:"command,omitempty"`
	Endpoint    string `yaml:"endpoint,omitempty"`
	DefaultLvl  int    `yaml:"defaultLevel"`
	Description string `yaml:"description"`
	// Env holds environment variables for process-driver tools.
	// Values support ${VAR} syntax for host environment variable resolution.
	Env map[string]string `yaml:"env,omitempty"`
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{}
}

// Register adds a tool definition to the registry.
func (r *ToolRegistry) Register(def *ToolDefinition) error {
	// TODO(v0.12.x)
	return nil
}

// Unregister removes a tool definition by name.
func (r *ToolRegistry) Unregister(name string) error {
	// TODO(v0.12.x)
	return nil
}

// List returns all registered tool definitions.
func (r *ToolRegistry) List() []ToolDefinition {
	// TODO(v0.12.x)
	return nil
}
