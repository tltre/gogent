package mgmt

import "github.com/tltre/gogent/pkg/component"

// ComponentInfo represents metadata for a managed component within an app
// (or standalone for centralized services). Each process-driver component
// has an OS PID; native components have Target = "".
type ComponentInfo struct {
	Name    string               `json:"name"`           // e.g. "provider-main" or "tool-service"
	AppName string               `json:"app_name"`       // owning App (empty for centralized service)
	Type    string               `json:"type"`           // "provider"|"tool"|"memory"|...
	Driver  component.DriverType `json:"driver"`         // "process"|"http"|"native"
	Target  string               `json:"target"`         // dial target e.g. "localhost:54321" (native = "")
	PID     int                  `json:"pid"`            // OS PID (only for process driver; 0 otherwise)
	Status  string               `json:"status"`         // "running"|"stopped"|"error"
	Apps    []string             `json:"apps,omitempty"` // app names using this component
}

type HealthResult struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type ComponentHealthResult struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Driver    string `json:"driver"`
	Status    string `json:"status"` // "ok"|"unhealthy"|"dead"|"unknown"
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

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
	Status     string   `json:"status"` // "running" | "stopped" | "error"
	ConfigPath string   `json:"config_path"`
	StartedAt  int64    `json:"started_at"`           // unix timestamp
	Components []string `json:"components,omitempty"` // component names used by this app
}

// LoadAppRequest is the request body for loading a new app into the daemon.
type LoadAppRequest struct {
	ConfigPath string `json:"config_path"`
}

// AgentRunRequest is the request body for POST /api/v1/agent/run.
type AgentRunRequest struct {
	Messages []AgentMessage `json:"messages"`
	Context  map[string]any `json:"context,omitempty"`
}

// AgentMessage mirrors agentcore.Message for JSON transport.
type AgentMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// AgentRunResponse is the response body for POST /api/v1/agent/run.
type AgentRunResponse struct {
	Response AgentMessage   `json:"response"`
	Actions  []AgentAction  `json:"actions,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// AgentAction mirrors agentcore.Action for JSON transport.
type AgentAction struct {
	ToolName string         `json:"tool_name"`
	Params   map[string]any `json:"params,omitempty"`
	Result   any            `json:"result,omitempty"`
}
