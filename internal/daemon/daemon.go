package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/credentials"
	"github.com/tltre/gogent/internal/daemon/sandbox"
	"github.com/tltre/gogent/internal/daemon/tool"
	"github.com/tltre/gogent/pkg/component"
	"gopkg.in/yaml.v3"
)

// Daemon manages the lifecycle of App instances. It holds an AppStore,
// allocates ports, and launches independent OS child processes for each app.
type Daemon struct {
	store         *AppStore
	compStore     *ComponentStore
	basePort      int
	mu            sync.Mutex
	reservedPorts map[int]bool // tracks ports allocated but not yet confirmed

	// Component auto-restart tracking.
	compRetryCount        map[string]int       // retry count per component name
	compRetryFirstAttempt map[string]time.Time // timestamp of first retry attempt
	compRetryMu           sync.Mutex           // protects retry maps

	// v0.12.1: Centralized tool management.
	toolReg       *tool.ToolRegistry    // registered tool definitions (child tools)
	manifestStore *tool.ManifestStore  // per-app manifest registrations
	serverStore   *tool.ServerStore     // v0.12.8: MCP server metadata
	resolver      *tool.EnvResolver     // v0.12.6: credential resolver
	grpcSrv       *GrpcServer           // gRPC ToolService server
	grpcReady     bool                  // true after StartGrpc() completes

	// v0.13.2: Sandbox management.
	sandboxMgr      *sandbox.SandboxManager
	sandboxDefaults *sandbox.DefaultMappings
}

// NewDaemon creates a new Daemon with an empty AppStore and the given base
// port for allocation. It scans ~/.gogent/ for surviving app instances from
// a previous daemon crash and recovers them.
func NewDaemon(basePort int, credentialsPath ...string) *Daemon {
	// Create ServerStore and ToolRegistry.
	serverStore := tool.NewServerStore()
	toolReg := tool.NewToolRegistry()

	// Load credentials (default ~/.gogent/credentials.yaml).
	var credPath string
	if len(credentialsPath) > 0 {
		credPath = credentialsPath[0]
	}
	creds, _ := credentials.LoadCredentials(credPath)
	resolver := credentials.NewResolver(creds)

	// Step 1: Load built-in tools into both ServerStore and ToolRegistry.
	toolReg.LoadDefault(serverStore)

	// Step 2: Load process/http servers from tools.yaml into ToolRegistry.
	// (ServerStore entries are added by the caller after NewDaemon returns.)
	if err := toolReg.LoadFromFile(resolver); err != nil {
		fmt.Fprintf(os.Stderr, "[daemon] warn: load tools.yaml: %v\n", err)
	}

	d := &Daemon{
		store:                 NewAppStore(),
		compStore:             NewComponentStore(),
		basePort:              basePort,
		reservedPorts:         make(map[int]bool),
		compRetryCount:        make(map[string]int),
		compRetryFirstAttempt: make(map[string]time.Time),
		toolReg:               toolReg,
		manifestStore:         tool.NewManifestStore(),
		resolver:              resolver,
		serverStore:           serverStore,
	}

	// v0.13.2: Initialize sandbox system.
	d.sandboxMgr = sandbox.NewSandboxManager()
	if sf, err := sandbox.LoadSandboxFile(); err != nil {
		fmt.Fprintf(os.Stderr, "[daemon] warn: load sandbox.yaml: %v\n", err)
	} else if sf != nil {
		// Load credentials from ~/.gogent/credentials.yaml for ${VAR} resolution.
		credPath := credentials.DefaultCredentialsPath()
		creds, _ := credentials.LoadCredentials(credPath)
		resolver := credentials.NewResolver(creds)
		defaults, err := sandbox.ApplySandboxFile(d.sandboxMgr, sf, resolver)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] warn: apply sandbox.yaml: %v\n", err)
		} else {
			d.sandboxDefaults = defaults
		}
	}

	d.RecoverApps()
	return d
}

// RecoverApps scans ~/.gogent/ for surviving app instances from a previous
// daemon crash. It checks PID liveness and HTTP health before re-registering
// each app in the store. Corrupt or dead entries are cleaned up silently.
func (d *Daemon) RecoverApps() {
	portFiles, err := ListAppPortFiles()
	if err != nil {
		return // directory doesn't exist or can't be read (fresh install)
	}

	for _, pf := range portFiles {
		// Skip already registered (shouldn't happen at startup).
		if _, exists := d.store.Get(pf.Name); exists {
			continue
		}

		// Check PID alive — clean up stale entries.
		if !d.isProcessAlive(pf.PID) {
			RemoveAppPortFile(pf.Name)
			continue
		}

		// Verify HTTP endpoint is responsive (agent's mgmt server).
		if err := pingApp(pf.Port); err != nil {
			RemoveAppPortFile(pf.Name)
			continue
		}

		// Re-register. ConfigPath and StartedAt are lost after a daemon crash.
		info := &api.AppInfo{
			Name:   pf.Name,
			Port:   pf.Port,
			PID:    pf.PID,
			Status: "running",
		}
		if err := d.store.Register(info); err != nil {
			continue
		}

		// Reserve the port.
		portNum, err := strconv.Atoi(strings.TrimPrefix(pf.Port, ":"))
		if err == nil {
			d.mu.Lock()
			d.reservedPorts[portNum] = true
			d.mu.Unlock()
		}

		fmt.Fprintf(os.Stderr,
			"[daemon] recovered app %s (port=%s pid=%d)\n",
			pf.Name, pf.Port, pf.PID)
	}
}

// Store returns the daemon's AppStore.
func (d *Daemon) Store() *AppStore {
	return d.store
}

// ComponentStore returns the daemon's ComponentStore for centralized services.
func (d *Daemon) ComponentStore() *ComponentStore {
	return d.compStore
}

// ToolRegistry returns the daemon's tool registry (v0.12.1).
func (d *Daemon) ToolRegistry() *tool.ToolRegistry {
	return d.toolReg
}

// ManifestStore returns the daemon's manifest store (v0.12.2).
func (d *Daemon) ManifestStore() *tool.ManifestStore {
	return d.manifestStore
}

// ServerStore returns the daemon's server store (v0.12.8).
func (d *Daemon) ServerStore() *tool.ServerStore {
	return d.serverStore
}

// StartAllServers starts all registered MCP servers and discovers their tools.
// Called after gRPC server is created.
func (d *Daemon) StartAllServers(ctx context.Context) {
	if d.grpcSrv != nil {
		if h := d.grpcSrv.Handler(); h != nil {
			h.StartAllServers(ctx)
		}
	}
}

// Handler returns the ToolService gRPC handler, or nil if not available.
func (d *Daemon) Handler() *tool.Handler {
	if d.grpcSrv != nil {
		return d.grpcSrv.Handler()
	}
	return nil
}

// StartLifecycle begins the MCP server health check loop.
func (d *Daemon) StartLifecycle(ctx context.Context) {
	if d.grpcSrv != nil {
		if h := d.grpcSrv.Handler(); h != nil {
			h.StartLifecycle(ctx)
		}
	}
}

// SandboxManager returns the daemon's sandbox manager (v0.13.2).
func (d *Daemon) SandboxManager() *sandbox.SandboxManager {
	return d.sandboxMgr
}

// Resolver returns the daemon's credential resolver (v0.12.6).
func (d *Daemon) Resolver() *credentials.Resolver {
	return d.resolver
}

// StartCredentialWatch starts the fsnotify credential watcher in a goroutine.
// The watcher reloads credentials.yaml on changes.
func (d *Daemon) StartCredentialWatch(ctx context.Context) {
	if d.resolver == nil {
		return
	}
	go func() {
		if err := d.resolver.Watch(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] credential watch ended: %v\n", err)
		}
	}()
}

// StartGrpc initializes and starts the gRPC server for ToolService.
// The gRPC port is basePort+1. This is called explicitly from runDaemon
// after other subsystems are ready.
func (d *Daemon) StartGrpc() {
	d.grpcSrv = NewGrpcServer(d.basePort, d.toolReg, d.manifestStore, d.resolver, d.serverStore, d.sandboxMgr, d.sandboxDefaults)
	if d.grpcSrv != nil {
		d.grpcSrv.Start()
		d.grpcReady = true
	}
}

// GrpcAddr returns the gRPC server address (e.g. "127.0.0.1:9091").
// Returns "" if the gRPC server was not started or failed to bind.
func (d *Daemon) GrpcAddr() string {
	if d.grpcSrv == nil {
		return ""
	}
	return d.grpcSrv.Addr()
}

// GRPCHealthCheck performs a public gRPC health check against the target.
// Exposed for the HTTP handler to use for real-time per-component probing.
func (d *Daemon) GRPCHealthCheck(target string) error {
	return d.grpcHealthCheck(target)
}

// ListApps returns a slice of all registered api.AppInfo entries.
func (d *Daemon) ListApps() []api.AppInfo {
	return d.store.List()
}

// GetApp retrieves an app by name. Returns the api.AppInfo and true if found.
func (d *Daemon) GetApp(name string) (*api.AppInfo, bool) {
	return d.store.Get(name)
}

// allocatePort reserves and returns an available port. It checks both OS-level
// availability and the daemon's internal reserved-port set to prevent races
// between allocation and the agent process binding.
func (d *Daemon) allocatePort() (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i := 1; i <= 100; i++ {
		port := d.basePort + i
		if d.reservedPorts[port] {
			continue
		}
		if !IsPortAvailable(port) {
			continue
		}
		d.reservedPorts[port] = true
		return port, nil
	}
	return 0, fmt.Errorf("no available port after 100 attempts starting from %d", d.basePort)
}

// releasePort removes a port from the reserved set so it can be allocated again.
func (d *Daemon) releasePort(port int) {
	d.mu.Lock()
	delete(d.reservedPorts, port)
	d.mu.Unlock()
}

// ---------------------------------------------------------------------------
// YAML component config parsing (daemon-local, mirrors pkg/app/ComponentConfig)
// ---------------------------------------------------------------------------

// daemonComponentConfig is a daemon-local struct for parsing component entries
// from a YAML config file.
type daemonComponentConfig struct {
	Name   string         `yaml:"name"`
	Type   string         `yaml:"type"`
	Driver string         `yaml:"driver"`
	Config map[string]any `yaml:"config,omitempty"`
}

// getEndpoint extracts the endpoint from Config["endpoint"] if present.
// Returns an empty string when no endpoint is configured.
func (cc daemonComponentConfig) getEndpoint() string {
	if cc.Config == nil {
		return ""
	}
	ep, ok := cc.Config["endpoint"]
	if !ok {
		return ""
	}
	eps, ok := ep.(string)
	if !ok {
		return ""
	}
	return eps
}

// daemonAppConfig is a daemon-local struct for parsing the full app config.
type daemonAppConfig struct {
	Name       string                  `yaml:"name"`
	Components []daemonComponentConfig `yaml:"components,omitempty"`
	Defaults   map[string]string       `yaml:"defaults,omitempty"`
}

// parseComponentConfig parses the YAML data and extracts the component list
// and defaults map.
func parseComponentConfig(data []byte) (*daemonAppConfig, error) {
	var cfg daemonAppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse component config: %w", err)
	}
	return &cfg, nil
}

// singletonTypes defines component types that are managed as singletons by
// the daemon (one per daemon, shared across all apps).
var singletonTypes = map[string]bool{
	"provider":       true,
	"memory":         true,
	"contextmanager": true,
	"agentcore":      true,
}

// appLocalTypes defines component types that the daemon does NOT manage.
var appLocalTypes = map[string]bool{
	"hook":     true,
	"channel":  true,
	"eventbus": true,
	"logger":   true,
	"sandbox":  true,
}

// typeToEnvVar maps a component type to its environment variable name suffix.
func typeToEnvVar(typ string) string {
	return "GOGENT_" + strings.ToUpper(typ) + "_TARGET"
}

// spawnedComp tracks a component that was spawned during the current LoadApp
// call, for cleanup on failure.
type spawnedComp struct {
	name string
	pid  int
	port int
}

// ---------------------------------------------------------------------------
// LoadApp (v0.9 — with component pre-forking)
// ---------------------------------------------------------------------------

// LoadApp reads the YAML config at configPath, parses the component list,
// forks singleton components (provider, memory, contextmanager, agentcore)
// and the shared ToolService, injects environment variables pointing to them,
// then forks the App child process. Returns the api.AppInfo on success.
func (d *Daemon) LoadApp(configPath string, needForkApplication bool) (*api.AppInfo, error) {
	// 1. Read and parse YAML config.
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg, err := parseComponentConfig(data)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("config missing 'name' field")
	}

	// 2. Check for duplicate app name.
	if _, exists := d.store.Get(cfg.Name); exists {
		return nil, fmt.Errorf("app %s already running", cfg.Name)
	}

	// Build a lookup: component name → config.
	compByName := make(map[string]daemonComponentConfig)
	for _, cc := range cfg.Components {
		compByName[cc.Name] = cc
	}

	// 3. Fork singleton components and collect targets for env injection.
	envVars := os.Environ()
	var spawned []spawnedComp
	var allCompNames []string // all component names used by this app (for bi-directional mapping)
	cleanup := func() {
		// Clean up newly-forked component processes.
		for _, sc := range spawned {
			d.killProcess(sc.pid)
			d.releasePort(sc.port)
			d.compStore.Unregister(sc.name)
			RemoveComponentPortFile(cfg.Name, sc.name)
		}
		// Remove this app from shared component Apps lists.
		for _, name := range allCompNames {
			isSpawned := false
			for _, sc := range spawned {
				if sc.name == name {
					isSpawned = true
					break
				}
			}
			if !isSpawned {
				d.compStore.RemoveApp(name, cfg.Name)
			}
		}
	}

	// Helper: fork a single component by name, using the provided type
	// (which may differ from the YAML type — e.g., tool → "tool-service").
	forkComp := func(compName, compType string) (string, error) {
		cc, ok := compByName[compName]
		if !ok {
			return "", fmt.Errorf("component %s referenced in defaults but not found in components list", compName)
		}
		if cc.Driver != string(component.DriverProcess) {
			// Non-process driver — register in ComponentStore for monitoring (no fork needed).
			if _, exists := d.compStore.Get(compName); !exists {
				info := &api.ComponentInfo{
					Name:    compName,
					AppName: cfg.Name,
					Type:    compType,
					Driver:  component.DriverType(cc.Driver),
					Target:  cc.getEndpoint(),
					Status:  "running",
					Apps:    []string{cfg.Name},
				}
				d.compStore.Register(info)
			} else {
				d.compStore.AppendApp(compName, cfg.Name)
			}
			allCompNames = append(allCompNames, compName)
			return "", nil
		}

		// Check if already registered in ComponentStore.
		if existing, exists := d.compStore.Get(compName); exists {
			if d.isProcessAlive(existing.PID) {
				// Shared component — add this app to its Apps list.
				d.compStore.AppendApp(compName, cfg.Name)
				allCompNames = append(allCompNames, compName)
				return existing.Target, nil
			}
			// Dead process — clean up and re-fork.
			d.compStore.Unregister(compName)
			RemoveComponentPortFile(cfg.Name, compName)
		}

		// Allocate port and fork.
		port, err := d.allocatePort()
		if err != nil {
			return "", fmt.Errorf("allocate port for %s: %w", compName, err)
		}
		portStr := strconv.Itoa(port)
		target := "localhost:" + portStr

		// Determine what executable to fork:
		//   config.command set → fork that binary (for e2e stubcomponents)
		//   otherwise            → fork self with "component" subcommand
		var exePath string
		var args []string
		if cmd, ok := cc.Config["command"].(string); ok && cmd != "" {
			exePath = cmd
			args = []string{exePath, "--port", portStr, "--type", compType}
		} else {
			exePath, err = os.Executable()
			if err != nil {
				d.releasePort(port)
				return "", fmt.Errorf("get executable path: %w", err)
			}
			args = []string{exePath, "component", "--name", compName, "--port", portStr, "--type", compType}
		}

		attr := &os.ProcAttr{
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		}
		proc, err := os.StartProcess(exePath, args, attr)
		if err != nil {
			d.releasePort(port)
			return "", fmt.Errorf("fork component %s: %w", compName, err)
		}

		spawned = append(spawned, spawnedComp{name: compName, pid: proc.Pid, port: port})
		allCompNames = append(allCompNames, compName)

		// Wait for gRPC health check (max 10s).
		if err := d.waitForComponentHealth(target, 10*time.Second); err != nil {
			return "", fmt.Errorf("component %s health check: %w", compName, err)
		}

		// Register in ComponentStore.
		info := &api.ComponentInfo{
			Name:    compName,
			AppName: cfg.Name,
			Type:    compType,
			Driver:  component.DriverProcess,
			Target:  target,
			PID:     proc.Pid,
			Status:  "running",
			Apps:    []string{cfg.Name},
		}
		if err := d.compStore.Register(info); err != nil {
			return "", fmt.Errorf("register component %s: %w", compName, err)
		}

		// Write port file with the component process PID.
		if err := WriteComponentPortFile(cfg.Name, compName, ":"+portStr, proc.Pid); err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] warn: write port file for %s: %v\n", compName, err)
		}

		return target, nil
	}

	// Fork singleton components: for each type in defaults, if the
	// referenced component has driver "process", fork it.
	for typ, name := range cfg.Defaults {
		if !singletonTypes[typ] && typ != "tool" {
			continue
		}
		if appLocalTypes[typ] {
			continue
		}

		// ToolService uses a fixed name "tool-service" regardless of YAML name.
		storeName := name
		storeType := typ
		if typ == "tool" {
			storeName = "tool-service"
			storeType = "tool"
		}

		target, err := forkComp(storeName, storeType)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("fork component %s (%s): %w", name, typ, err)
		}
		if target != "" {
			envVars = append(envVars, typeToEnvVar(typ)+"="+target)
		}
	}

	// Inject daemon gRPC address for agent-side ToolManager discovery (v0.12.1).
	if addr := d.GrpcAddr(); addr != "" {
		envVars = append(envVars, "GOGENT_DAEMON_GRPC_ADDR="+addr)
	}

	// 4. Allocate a port for the App process.
	port, err := d.allocatePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("allocate app port: %w", err)
	}
	portStr := strconv.Itoa(port)

	// 5. Fork the App child process with env vars injected (only when needed).
	var pid int
	if needForkApplication {
		exePath, err := os.Executable()
		if err != nil {
			cleanup()
			d.releasePort(port)
			return nil, fmt.Errorf("get executable path: %w", err)
		}
		absConfig, err := filepath.Abs(configPath)
		if err != nil {
			cleanup()
			d.releasePort(port)
			return nil, fmt.Errorf("resolve config path: %w", err)
		}

		attr := &os.ProcAttr{
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
			Env:   envVars,
		}
		proc, err := os.StartProcess(exePath, []string{
			exePath, "agent", "--config", absConfig, "--port", portStr,
		}, attr)
		if err != nil {
			cleanup()
			d.releasePort(port)
			return nil, fmt.Errorf("fork agent: %w", err)
		}
		pid = proc.Pid

		// 6. Wait for agent to become healthy (max 10s, 200ms interval).
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		ready := false
		for !ready {
			select {
			case <-ctx.Done():
				d.killProcess(pid)
				d.releasePort(port)
				cleanup()
				return nil, fmt.Errorf("agent %s did not become healthy within 10s", cfg.Name)
			case <-ticker.C:
				if err := pingApp(":" + portStr); err == nil {
					ready = true
				}
			}
		}

		// Port is now confirmed occupied by the agent — remove from reserved set.
		d.releasePort(port)
	}

	// 7. Create api.AppInfo and register in store.
	now := time.Now().Unix()
	info := &api.AppInfo{
		Name:       cfg.Name,
		Port:       ":" + portStr,
		PID:        pid,
		Status:     "running",
		ConfigPath: configPath,
		StartedAt:  now,
		Components: allCompNames,
	}
	if !needForkApplication {
		// Return env vars so the caller can set them before building the agent.
		info.Env = envVars
	}
	if err := d.store.Register(info); err != nil {
		if needForkApplication {
			d.killProcess(pid)
		}
		cleanup()
		return nil, fmt.Errorf("register app: %w", err)
	}

	return info, nil
}

// StopApp stops a running app by name. It kills the app process, then
// iterates over the app's component list. For each component, it removes
// the app from the component's Apps list. When no more apps use a component,
// the component process is killed and unregistered.
func (d *Daemon) StopApp(name string) error {
	info, exists := d.store.Get(name)
	if !exists {
		return fmt.Errorf("app %s not found", name)
	}

	// Read the real PID from the agent's port file.
	// In-process agents (gogent run) are registered with PID=0
	// but write the real PID via Listen → WriteAppPortFile.
	if pf, err := ReadAppPortFile(name); err == nil && pf.PID > 0 {
		info.PID = pf.PID
	}

	// Kill the app process and wait for it to exit (best-effort).
	// When PID is 0 (in-process agent already exited) or kill fails,
	// fall through to component cleanup instead of blocking.
	if info.PID > 0 {
		if err := d.killProcess(info.PID); err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] warn: kill app %s (pid %d): %v — continuing cleanup\n",
				name, info.PID, err)
		} else {
			// Wait up to 5s for the process to exit.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()

			exited := false
			for !exited {
				select {
				case <-ctx.Done():
					fmt.Fprintf(os.Stderr, "[daemon] warn: app %s (pid %d) did not exit within 5s\n",
						name, info.PID)
					exited = true
				case <-ticker.C:
					if !d.isProcessAlive(info.PID) {
						exited = true
					}
				}
			}
		}
	}

	// Clean up components using the bidirectional mapping.
	for _, compName := range info.Components {
		compInfo, ok := d.compStore.Get(compName)
		if !ok {
			continue
		}
		// Remove this app from the component's Apps list.
		remaining := d.compStore.RemoveApp(compName, name)
		if remaining == 0 {
			// No more apps using this component — shut it down.
			if compInfo.Driver == component.DriverProcess {
				d.killProcess(compInfo.PID)
				d.releasePort(parsePortFromTarget(compInfo.Target))
				RemoveComponentPortFile(name, compName)
			}
			d.compStore.Unregister(compName)
		}
	}

	// Unregister from store and remove port file.
	if err := d.store.Unregister(name); err != nil {
		return err
	}
	RemoveAppPortFile(name)

	// v0.13.2: Clean up sandbox instances.
	d.manifestStore.Unregister(name)
	if d.sandboxMgr != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := d.sandboxMgr.DestroyAppSandboxes(ctx, name); err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] warn: cleanup sandboxes for %s: %v\n", name, err)
		}
	}

	return nil
}

// Shutdown gracefully stops all managed apps and cleans up. Used when the
// daemon process itself is shutting down.
func (d *Daemon) Shutdown(ctx context.Context) error {
	apps := d.store.List()
	var errs []error

	for _, info := range apps {
		// Kill the app process.
		if err := d.killProcess(info.PID); err != nil {
			errs = append(errs, fmt.Errorf("kill app %s (pid %d): %w", info.Name, info.PID, err))
		}

		// Clean up all components belonging to this app.
		comps := d.compStore.ListByApp(info.Name)
		for _, comp := range comps {
			if comp.PID > 0 {
				if err := d.killProcess(comp.PID); err != nil {
					errs = append(errs, fmt.Errorf("kill component %s (pid %d): %w", comp.Name, comp.PID, err))
				}
			}
			d.compStore.Unregister(comp.Name)
			RemoveComponentPortFile(info.Name, comp.Name)
		}

		// v0.13.2: Clean up sandbox instances.
		d.manifestStore.Unregister(info.Name)
		if d.sandboxMgr != nil {
			if err := d.sandboxMgr.DestroyAppSandboxes(ctx, info.Name); err != nil {
				errs = append(errs, fmt.Errorf("cleanup sandboxes for %s: %w", info.Name, err))
			}
		}

		// Unregister from store (best-effort).
		if err := d.store.Unregister(info.Name); err != nil {
			errs = append(errs, fmt.Errorf("unregister app %s: %w", info.Name, err))
		}

		// Clean up port file.
		RemoveAppPortFile(info.Name)
	}

	// Stop gRPC server.
	if d.grpcSrv != nil {
		if err := d.grpcSrv.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("grpc shutdown: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}

// RestartApp stops a running app and then reloads it using its original
// config path. Returns the new api.AppInfo on success.
func (d *Daemon) RestartApp(name string) (*api.AppInfo, error) {
	info, exists := d.store.Get(name)
	if !exists {
		return nil, fmt.Errorf("app %s not found", name)
	}
	configPath := info.ConfigPath

	if err := d.StopApp(name); err != nil {
		return nil, fmt.Errorf("stop for restart: %w", err)
	}

	return d.LoadApp(configPath, true)
}

// pingApp performs a lightweight HTTP GET against an agent's app-info endpoint.
// Used by RecoverApps and LoadApp to verify an agent is responsive.
func pingApp(port string) error {
	url := "http://127.0.0.1" + port + api.PathAppInfo
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// parsePortFromTarget extracts the port number from a target string like
// "localhost:54321". Returns 0 if parsing fails.
func parsePortFromTarget(target string) int {
	parts := strings.Split(target, ":")
	if len(parts) < 2 {
		return 0
	}
	port, _ := strconv.Atoi(parts[len(parts)-1])
	return port
}
