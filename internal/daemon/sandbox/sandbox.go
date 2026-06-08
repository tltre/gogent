// Package sandbox provides the daemon-side sandbox management system.
//
// It defines the core interfaces and types for creating, managing, and
// executing commands within isolated sandbox environments. The package
// uses a provider pattern where different sandbox backends (E2B, builtin,
// agent-sandbox, etc.) implement the SandboxProvider interface.
//
// Configuration is loaded from ~/.gogent/sandbox.yaml, which declares
// providers, profiles, and default tool-to-sandbox mappings.
package sandbox

import (
	"context"
	"time"
)

// ---------------------------------------------------------------------------
// Sandbox types
// ---------------------------------------------------------------------------

// SandboxType identifies the execution backend for a sandbox.
type SandboxType string

const (
	SandboxE2B    SandboxType = "e2b"           // E2B-compatible REST API
	SandboxBuiltin SandboxType = "builtin"       // bwrap/seatbelt (future)
	SandboxAgent   SandboxType = "agent-sandbox" // agent-sandbox K8s (future)
)

// ---------------------------------------------------------------------------
// Lifecycle & Storage
// ---------------------------------------------------------------------------

// LifecycleMode controls the sandbox's lifetime semantics.
type LifecycleMode string

const (
	LifecyclePersistent LifecycleMode = "persistent" // snapshot on stop, restore on start
	LifecycleEphemeral  LifecycleMode = "ephemeral"  // auto-destroy after timeout
	LifecycleSession    LifecycleMode = "session"    // tied to gRPC connection
)

// StorageBackend identifies the filesystem storage mechanism.
type StorageBackend string

const (
	StorageDirectory  StorageBackend = "directory"  // local filesystem directory
	StorageDatabase   StorageBackend = "database"   // sqlite-backed (future)
	StorageHostPath   StorageBackend = "host-path"  // host directory mount (future)
	StorageTmpfs      StorageBackend = "tmpfs"      // in-memory (future)
)

// LifecycleConfig controls sandbox lifecycle behavior.
type LifecycleConfig struct {
	Mode     LifecycleMode  `yaml:"mode"`
	Timeout  time.Duration  `yaml:"timeout,omitempty"`
	Snapshot SnapshotConfig `yaml:"snapshot,omitempty"`
}

// SnapshotConfig controls snapshot behavior for persistent sandboxes.
type SnapshotConfig struct {
	Enabled bool   `yaml:"enabled"`
	OnStop  string `yaml:"onStop,omitempty"`  // "save" | "none"
	OnStart string `yaml:"onStart,omitempty"` // "load" | "fresh"
}

// StorageConfig controls the sandbox's filesystem storage.
type StorageConfig struct {
	Backend   StorageBackend `yaml:"backend"`
	Lifecycle LifecycleMode  `yaml:"lifecycle,omitempty"`
}

// ---------------------------------------------------------------------------
// Resource limits
// ---------------------------------------------------------------------------

// ResourceLimits constrains sandbox resource usage.
type ResourceLimits struct {
	MaxMemoryMB int           `yaml:"maxMemoryMB,omitempty"`
	MaxCPUTime  time.Duration `yaml:"maxCPUTime,omitempty"`
	MaxFileSize int64         `yaml:"maxFileSize,omitempty"`
	MaxProcesses int          `yaml:"maxProcesses,omitempty"`
}

// ---------------------------------------------------------------------------
// Execution types
// ---------------------------------------------------------------------------

// ExecRequest is the input to a single sandboxed command execution.
type ExecRequest struct {
	Code     string            `yaml:"code"`
	Language string            `yaml:"language,omitempty"` // "sh", "bash", "python"; "" defaults to "sh"
	Timeout  time.Duration     `yaml:"timeout,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
}

// ExecResult is the output of a single sandboxed command execution.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Error    error
}

// ---------------------------------------------------------------------------
// Profile & Config types
// ---------------------------------------------------------------------------

// SandboxProfile is a security-boundary template defined by the daemon admin.
// It specifies what backend to use, what template/image, resource limits,
// network access, and command restrictions.
type SandboxProfile struct {
	Name        string            `yaml:"-"` // key in profiles map
	Provider    string            `yaml:"provider"`              // references sandbox-providers entry
	Template    string            `yaml:"template"`              // E2B template ID
	Network     *bool             `yaml:"network,omitempty"`     // nil = default (false)
	MaxMemoryMB int               `yaml:"maxMemoryMB,omitempty"`
	MaxCPUTime  time.Duration     `yaml:"maxCPUTime,omitempty"`
	ReadOnlyRoot bool             `yaml:"readOnlyRoot,omitempty"`
	AllowCommands []string        `yaml:"allowCommands,omitempty"` // command whitelist
	AllowReadPaths []string       `yaml:"allowReadPaths,omitempty"`
	Egress       *EgressConfig    `yaml:"egress,omitempty"`
	Lifecycle    LifecycleConfig  `yaml:"lifecycle,omitempty"`
	Snapshot     SnapshotConfig   `yaml:"snapshot,omitempty"`
	E2B          map[string]any   `yaml:"e2b,omitempty"`         // raw passthrough to E2B API
}

// EgressConfig controls outbound network access.
type EgressConfig struct {
	AllowDomains []string `yaml:"allowDomains,omitempty"`
	DenyDomains  []string `yaml:"denyDomains,omitempty"`
}

// SandboxConfig is the app-side customization of a SandboxProfile.
// It references a profile and may narrow its constraints.
type SandboxConfig struct {
	Name            string          `yaml:"-"` // key in app's sandboxes map
	ProfileName     string          `yaml:"profile"`         // references sandbox-profiles entry
	Template        string          `yaml:"template,omitempty"` // overrides profile template
	WorkDir         string          `yaml:"workDir,omitempty"`
	AllowedCommands []string        `yaml:"allowedCommands,omitempty"` // subset of profile.AllowCommands
	Lifecycle       LifecycleConfig `yaml:"lifecycle,omitempty"`
	SnapshotID      string          `yaml:"-"` // runtime-only: snapshot to restore from
	Storage         StorageConfig   `yaml:"storage,omitempty"`
	E2B             map[string]any  `yaml:"e2b,omitempty"` // per-sandbox E2B passthrough overrides
}

// ---------------------------------------------------------------------------
// Sandbox instance interface
// ---------------------------------------------------------------------------

// ISandbox is a running sandbox instance. It provides command execution
// and resource cleanup. Implementations wrap specific backends
// (E2B SDK, bwrap process, etc.).
type ISandbox interface {
	// Execute runs a command inside this sandbox and returns the result.
	Execute(ctx context.Context, req ExecRequest) (ExecResult, error)

	// Close releases all resources held by this sandbox. After Close,
	// Execute must return an error.
	Close(ctx context.Context) error
}

// Refreshable is an optional interface for sandbox backends that support
// TTL refresh (e.g., E2B sandboxes have a timeout that must be periodically
// refreshed to prevent auto-destruction).
type Refreshable interface {
	// Refresh resets the sandbox's TTL. Called periodically by SandboxManager.
	Refresh(ctx context.Context) error
}

// SnapshotProvider is an optional interface for backends that can capture
// point-in-time snapshots (e.g., Firecracker MicroVM snapshots in E2B).
type SnapshotProvider interface {
	// Snapshot captures the current sandbox state and returns a snapshot ID.
	Snapshot(ctx context.Context, sb ISandbox) (string, error)

	// CreateFromSnapshot creates a new sandbox from a previously saved snapshot.
	CreateFromSnapshot(ctx context.Context, snapshotID string, cfg *SandboxConfig) (ISandbox, error)
}

// ---------------------------------------------------------------------------
// Defaults & fallback
// ---------------------------------------------------------------------------

// DefaultMappings defines the fallback profiles for tools that don't
// specify an explicit sandbox. Defined in sandbox.yaml under the "defaults" key.
type DefaultMappings struct {
	Builtin ProfileRef `yaml:"builtin"` // built-in tools (shell, filesystem.read)
	Process ProfileRef `yaml:"process"` // process/http MCP tools
}

// ProfileRef references a SandboxProfile by name.
type ProfileRef struct {
	Profile string `yaml:"profile"`
}
