package mgmt

type ComponentInfo struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type HealthResult struct {
	Component string `json:"component"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
}

type InfoResponse struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	GoVersion  string `json:"go_version"`
	UptimeSec  int64  `json:"uptime_seconds"`
}
