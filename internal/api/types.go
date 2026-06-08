package api

import (
	"github.com/tltre/gogent/pkg/component"
)

// ComponentInfo represents metadata for a managed component within an app
// (or standalone for centralized services).
type ComponentInfo struct {
	Name    string               `json:"name"`
	AppName string               `json:"app_name"`
	Type    string               `json:"type"`
	Driver  component.DriverType `json:"driver"`
	Target  string               `json:"target"`
	PID     int                  `json:"pid"`
	Status  string               `json:"status"`
	Apps    []string             `json:"apps,omitempty"`
}

// HealthResult represents the health status of a single component.
type HealthResult struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// ComponentHealthResult represents the health status of a daemon-managed component.
type ComponentHealthResult struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Driver    string `json:"driver"`
	Status    string `json:"status"` // "ok"|"unhealthy"|"dead"|"unknown"
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

// InfoResponse is returned by the /api/v1/app/info and /api/v1/daemon/info endpoints.
type InfoResponse struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	UptimeSec int64  `json:"uptime_seconds"`
}

// AppInfo represents metadata for a running app instance.
type AppInfo struct {
	Name       string   `json:"name"`
	Port       string   `json:"port"`
	PID        int      `json:"pid"`
	Status     string   `json:"status"`
	ConfigPath string   `json:"config_path"`
	StartedAt  int64    `json:"started_at"`
	Components []string `json:"components,omitempty"`
	Env        []string `json:"env,omitempty"`
}

// LoadAppRequest is the POST body for loading a new app into the daemon.
type LoadAppRequest struct {
	ConfigPath          string `json:"config_path"`
	NeedForkApplication *bool  `json:"need_fork_application,omitempty"`
}

// AgentRunRequest is the POST body for /api/v1/app/agent/run.
type AgentRunRequest struct {
	Messages []AgentMessage `json:"messages"`
	Context  map[string]any `json:"context,omitempty"`
}

// AgentMessage represents a single message in an agent run request.
type AgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AgentRunResponse is the response from /api/v1/app/agent/run.
type AgentRunResponse struct {
	Response string         `json:"response"`
	Actions  []AgentAction  `json:"actions,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// AgentAction represents a tool call action in an agent run response.
type AgentAction struct {
	ToolName string         `json:"tool_name"`
	Params   map[string]any `json:"params"`
	Result   any            `json:"result"`
}

// StatusToString converts a component status to its string representation.
func StatusToString(s component.ComponentStatus) string {
	switch s {
	case component.StatusUninitialized:
		return "uninitialized"
	case component.StatusInitialized:
		return "initialized"
	case component.StatusStarted:
		return "started"
	case component.StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ---------------------------------------------------------------------------
// Sandbox API types (v0.13.x)
// ---------------------------------------------------------------------------

// SandboxProviderRequest is the POST body for adding a provider.
type SandboxProviderRequest struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
}

// SandboxProfileRequest is the POST body for adding a profile.
type SandboxProfileRequest struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Template string `json:"template"`
	Network  *bool  `json:"network,omitempty"`
}

// SandboxProfileEditRequest is the PUT body for editing a profile.
type SandboxProfileEditRequest struct {
	Provider *string `json:"provider,omitempty"`
	Template *string `json:"template,omitempty"`
	Network  *bool   `json:"network,omitempty"`
}

// SandboxProfileView is the response item for listing profiles.
type SandboxProfileView struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Template string `json:"template"`
}

// SandboxProviderView is a provider entry in status/list responses.
type SandboxProviderView struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// SandboxStatusResponse is the response from /status.
type SandboxStatusResponse struct {
	Providers []SandboxProviderView `json:"providers"`
	Profiles  []SandboxProfileView  `json:"profiles"`
}

// RegisterToolRequest is the JSON body for POST /api/v1/daemon/tools.
type RegisterToolRequest struct {
	Name         string            `json:"name"`
	Driver       string            `json:"driver"`
	Command      string            `json:"command,omitempty"`
	Endpoint     string            `json:"endpoint,omitempty"`
	DefaultLevel int               `json:"defaultLevel"`
	Description  string            `json:"description,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
	// Credentials holds plaintext secret values to be written to credentials.yaml.
	// Keys use the format "tool-name.KEY". When set, the corresponding Env entry
	// should be a "${tool-name.KEY}" reference, not the plaintext value.
	Credentials map[string]string `json:"credentials,omitempty"`
}
