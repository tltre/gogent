package tool

import (
	"fmt"
	"sync"
	"time"
)

// ToolDriver represents the execution driver type for a tool.
type ToolDriver string

const (
	DriverBuiltin ToolDriver = "builtin"
	DriverProcess ToolDriver = "process"
	DriverHTTP    ToolDriver = "http"
)

// ToolStatus represents the lifecycle state of a tool.
type ToolStatus string

const (
	StatusRegistered ToolStatus = "registered"
	StatusActive     ToolStatus = "active"
	StatusUnhealthy  ToolStatus = "unhealthy"
	StatusStopped    ToolStatus = "stopped"
	StatusRemoved    ToolStatus = "removed"
)

// ValidDrivers lists all valid driver strings.
var ValidDrivers = map[string]bool{
	string(DriverBuiltin): true,
	string(DriverProcess): true,
	string(DriverHTTP):    true,
}

// ToolDefinition describes a single tool known to the daemon.
type ToolDefinition struct {
	Name        string            `yaml:"name"`
	Driver      string            `yaml:"driver"` // "builtin" | "process" | "http"
	Command     string            `yaml:"command,omitempty"`
	Endpoint    string            `yaml:"endpoint,omitempty"`
	DefaultLvl  int               `yaml:"defaultLevel"`
	Description string            `yaml:"description"`
	Env         map[string]string `yaml:"env,omitempty"`
}

// ToolStats holds runtime statistics for a tool.
type ToolStats struct {
	Invocations int64
	Failures    int64
	LastUsed    time.Time
}

// ToolSource identifies where a tool definition came from.
type ToolSource string

const (
	SourceBuiltin ToolSource = "builtin" // loaded from DefaultBuiltinTools
	SourceUser    ToolSource = "user"    // registered via CLI or gRPC
	SourceFile    ToolSource = "file"    // loaded from ~/.gogent/tools.yaml
)

// toolEntry is the internal runtime representation of a registered tool.
type toolEntry struct {
	def     *ToolDefinition
	source  ToolSource
	status  ToolStatus
	stats   ToolStats
	created time.Time
}

// ToolRegistry is a centralized registry for tool definitions managed by
// the daemon. All public methods are thread-safe.
type ToolRegistry struct {
	mu      sync.RWMutex
	entries map[string]*toolEntry
}

// NewToolRegistry creates an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		entries: make(map[string]*toolEntry),
	}
}

// Register adds a tool definition. Returns error if name already exists or
// validation fails. The optional source parameter indicates where the tool
// definition came from (defaults to SourceUser).
func (r *ToolRegistry) Register(def *ToolDefinition, source ...ToolSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validateLocked(def); err != nil {
		return err
	}
	if _, exists := r.entries[def.Name]; exists {
		return fmt.Errorf("tool %q already registered", def.Name)
	}
	return r.insertLocked(def, source...)
}

// MustRegister is like Register but panics on error (for built-in tools).
func (r *ToolRegistry) MustRegister(def *ToolDefinition) {
	if err := r.Register(def, SourceBuiltin); err != nil {
		panic(fmt.Sprintf("register builtin tool %q: %v", def.Name, err))
	}
}

// registerOrUpdate registers a new tool or updates an existing one.
// Used when loading from tools.yaml (file overrides defaults).
func (r *ToolRegistry) registerOrUpdate(def *ToolDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.validateLocked(def); err != nil {
		return err
	}
	if _, exists := r.entries[def.Name]; exists {
		// Update in place — preserve status and stats
		r.entries[def.Name].def = def
		r.entries[def.Name].source = SourceFile
		return nil
	}
	return r.insertLocked(def, SourceFile)
}

// Unregister removes a tool definition by name.
func (r *ToolRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.entries[name]; !exists {
		return fmt.Errorf("tool %q not found", name)
	}
	delete(r.entries, name)
	return nil
}

// Get retrieves a tool definition by name.
func (r *ToolRegistry) Get(name string) (*ToolDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[name]
	if !exists {
		return nil, false
	}
	return entry.def, true
}

// List returns a copy of all registered tool definitions.
func (r *ToolRegistry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ToolDefinition, 0, len(r.entries))
	for _, entry := range r.entries {
		result = append(result, *entry.def)
	}
	return result
}

// Exists checks if a tool name is registered.
func (r *ToolRegistry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.entries[name]
	return exists
}

// Count returns the number of registered tools.
func (r *ToolRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.entries)
}

// SetStatus updates the status of a registered tool.
func (r *ToolRegistry) SetStatus(name string, status ToolStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return fmt.Errorf("tool %q not found", name)
	}
	entry.status = status
	return nil
}

// GetStatus returns the current status of a tool.
func (r *ToolRegistry) GetStatus(name string) ToolStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[name]
	if !exists {
		return StatusRemoved
	}
	return entry.status
}

// Sanitized returns a copy of the definition with sensitive fields removed.
// Safe for exposing to CLI/HTTP/gRPC callers. Env is deliberately omitted
// for credential isolation.
func (d *ToolDefinition) Sanitized() ToolDefinition {
	return ToolDefinition{
		Name:        d.Name,
		Driver:      d.Driver,
		Command:     d.Command,
		Endpoint:    d.Endpoint,
		DefaultLvl:  d.DefaultLvl,
		Description: d.Description,
	}
}

// GetSource returns the source of a registered tool ("builtin" | "user" | "file").
func (r *ToolRegistry) GetSource(name string) ToolSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, exists := r.entries[name]
	if !exists {
		return ""
	}
	return entry.source
}

// GetStats returns runtime statistics for a tool.
func (r *ToolRegistry) GetStats(name string) ToolStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.entries[name]
	if !exists {
		return ToolStats{}
	}
	return entry.stats
}

// RecordInvocation increments the invocation counter for a tool.
func (r *ToolRegistry) RecordInvocation(name string, success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.entries[name]
	if !exists {
		return
	}
	entry.stats.Invocations++
	entry.stats.LastUsed = time.Now()
	if !success {
		entry.stats.Failures++
	}
}

// --- internal helpers ---

func (r *ToolRegistry) validateLocked(def *ToolDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	if !ValidDrivers[def.Driver] {
		return fmt.Errorf("tool %q: invalid driver %q (must be builtin/process/http)", def.Name, def.Driver)
	}
	if def.DefaultLvl < 0 || def.DefaultLvl > 2 {
		return fmt.Errorf("tool %q: defaultLevel must be 0-2, got %d", def.Name, def.DefaultLvl)
	}
	if def.Driver == string(DriverProcess) && def.Command == "" {
		return fmt.Errorf("tool %q: command is required for process driver", def.Name)
	}
	if def.Driver == string(DriverHTTP) && def.Endpoint == "" {
		return fmt.Errorf("tool %q: endpoint is required for http driver", def.Name)
	}
	return nil
}

func (r *ToolRegistry) insertLocked(def *ToolDefinition, source ...ToolSource) error {
	src := SourceUser
	if len(source) > 0 {
		src = source[0]
	}
	r.entries[def.Name] = &toolEntry{
		def:     def,
		source:  src,
		status:  StatusRegistered,
		created: time.Now(),
	}
	return nil
}
