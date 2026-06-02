package api

// =============================================================================
// App-level mgmt API paths
// =============================================================================
// An in-process agent's mgmt server (mgmt.Listen) registers these.
// mgmt.AppClient calls these to inspect a running agent.
const (
	PathAppRegistry  = "/api/v1/app/registry"
	PathAppHealth    = "/api/v1/app/health"
	PathAppLogs      = "/api/v1/app/logs"
	PathAppInfo      = "/api/v1/app/info"
	PathAppToolsExec = "/api/v1/app/tools/exec"
	PathAppSessions  = "/api/v1/app/sessions"
	PathAppAgentRun  = "/api/v1/app/agent/run"
)

// =============================================================================
// Daemon-level mgmt API paths
// =============================================================================
// The daemon process's HTTP server (daemon.NewDaemonServer) registers these.
// daemon.DaemonClient calls these to manage app lifecycle and daemon state.
const (
	PathDaemonApps             = "/api/v1/daemon/apps"
	PathDaemonAppsPath         = "/api/v1/daemon/apps/" // + {name}
	PathDaemonComponentsHealth = "/api/v1/daemon/components/health"
	PathDaemonInfo             = "/api/v1/daemon/info"
	// v0.12.1: Tool management endpoints (for CLI inspection)
	PathDaemonTools      = "/api/v1/daemon/tools"
	PathDaemonToolsPath  = "/api/v1/daemon/tools/"
	PathDaemonToolsRestart = "/api/v1/daemon/tools/" // + {name}/restart
)
