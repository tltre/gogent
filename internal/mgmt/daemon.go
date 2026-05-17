package mgmt

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

	"github.com/tltre/gogent/pkg/component"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
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
}

// NewDaemon creates a new Daemon with an empty AppStore and the given base
// port for allocation. It scans ~/.gogent/ for surviving app instances from
// a previous daemon crash and recovers them.
func NewDaemon(basePort int) *Daemon {
	d := &Daemon{
		store:                  NewAppStore(),
		compStore:              NewComponentStore(),
		basePort:               basePort,
		reservedPorts:          make(map[int]bool),
		compRetryCount:         make(map[string]int),
		compRetryFirstAttempt:  make(map[string]time.Time),
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

		// Verify HTTP endpoint is responsive.
		client := NewClient(pf.Port)
		if err := client.Ping(); err != nil {
			RemoveAppPortFile(pf.Name)
			continue
		}

		// Re-register. ConfigPath and StartedAt are lost after a daemon crash.
		info := &AppInfo{
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

// GRPCHealthCheck performs a public gRPC health check against the target.
// Exposed for the HTTP handler to use for real-time per-component probing.
func (d *Daemon) GRPCHealthCheck(target string) error {
	return d.grpcHealthCheck(target)
}

// ListApps returns a slice of all registered AppInfo entries.
func (d *Daemon) ListApps() []AppInfo {
	return d.store.List()
}

// GetApp retrieves an app by name. Returns the AppInfo and true if found.
func (d *Daemon) GetApp(name string) (*AppInfo, bool) {
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
	Name       string                   `yaml:"name"`
	Components []daemonComponentConfig  `yaml:"components,omitempty"`
	Defaults   map[string]string        `yaml:"defaults,omitempty"`
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
// then forks the App child process. Returns the AppInfo on success.
func (d *Daemon) LoadApp(configPath string) (*AppInfo, error) {
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
				info := &ComponentInfo{
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

		exePath, err := os.Executable()
		if err != nil {
			d.releasePort(port)
			return "", fmt.Errorf("get executable path: %w", err)
		}

		attr := &os.ProcAttr{
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		}
		proc, err := os.StartProcess(exePath, []string{
			exePath, "component", "--name", compName, "--port", portStr, "--type", compType,
		}, attr)
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
		info := &ComponentInfo{
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

		// Write port file.
		if err := WriteComponentPortFile(cfg.Name, compName, ":"+portStr); err != nil {
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

	// 4. Allocate a port for the App process.
	port, err := d.allocatePort()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("allocate app port: %w", err)
	}
	portStr := strconv.Itoa(port)

	// 5. Fork the App child process with env vars injected.
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
	pid := proc.Pid

	// 6. Wait for agent to become healthy (max 10s, 200ms interval).
	client := NewClient(":" + portStr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	ready := false
	for !ready {
		select {
		case <-ctx.Done():
			// Timeout – kill the forked process, release the port, and clean up components.
			d.killProcess(pid)
			d.releasePort(port)
			cleanup()
			return nil, fmt.Errorf("agent %s did not become healthy within 10s", cfg.Name)
		case <-ticker.C:
			if err := client.Ping(); err == nil {
				ready = true
			}
		}
	}

	// Port is now confirmed occupied by the agent — remove from reserved set.
	d.releasePort(port)

	// 7. Create AppInfo and register in store.
	now := time.Now().Unix()
	info := &AppInfo{
		Name:       cfg.Name,
		Port:       ":" + portStr,
		PID:        pid,
		Status:     "running",
		ConfigPath: absConfig,
		StartedAt:  now,
		Components: allCompNames,
	}
	if err := d.store.Register(info); err != nil {
		d.killProcess(pid)
		cleanup()
		return nil, fmt.Errorf("register app: %w", err)
	}

	// Note: the agent process writes its own port file (agent.go:62) with the
	// correct PID. Do NOT write it here with the daemon's PID — it would
	// break crash recovery (RecoverApps would see the daemon PID as dead).

	return info, nil
}

// waitForComponentHealth dials the gRPC target and polls the health check
// until SERVING or timeout.
func (d *Daemon) waitForComponentHealth(target string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("component %s did not become healthy within %v", target, timeout)
		case <-ticker.C:
			if err := d.grpcHealthCheck(target); err == nil {
				return nil
			}
		}
	}
}

// grpcHealthCheck performs a one-shot gRPC health check against the target.
func (d *Daemon) grpcHealthCheck(target string) error {
	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	hc := grpc_health_v1.NewHealthClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	resp, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		return fmt.Errorf("status %s", resp.Status.String())
	}
	return nil
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

	// Kill the app process.
	if err := d.killProcess(info.PID); err != nil {
		return fmt.Errorf("kill process: %w", err)
	}

	// Wait up to 5s for the process to exit.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	exited := false
	for !exited {
		select {
		case <-ctx.Done():
			return fmt.Errorf("process %d did not exit within 5s", info.PID)
		case <-ticker.C:
			if !d.isProcessAlive(info.PID) {
				exited = true
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

	return nil
}

// RestartApp stops a running app and then reloads it using its original
// config path. Returns the new AppInfo on success.
func (d *Daemon) RestartApp(name string) (*AppInfo, error) {
	info, exists := d.store.Get(name)
	if !exists {
		return nil, fmt.Errorf("app %s not found", name)
	}
	configPath := info.ConfigPath

	if err := d.StopApp(name); err != nil {
		return nil, fmt.Errorf("stop for restart: %w", err)
	}

	return d.LoadApp(configPath)
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

// StartHealthCheck launches a background goroutine that periodically checks
// whether managed app processes are still alive. If interval is zero or
// negative, a default of 10 seconds is used. The goroutine exits when ctx
// is cancelled.
func (d *Daemon) StartHealthCheck(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.runHealthCheck()
			}
		}
	}()
}

// runHealthCheck iterates over all registered apps and checks their health.
// When an app is detected as stopped, its component processes are also
// cleaned up (killed, unregistered, port files removed).
func (d *Daemon) runHealthCheck() {
	apps := d.store.List()
	for _, info := range apps {
		if !d.isProcessAlive(info.PID) {
			// Process no longer alive – mark as stopped.
			fmt.Fprintf(os.Stderr, "[daemon] app %s stopped (pid %d no longer alive)\n", info.Name, info.PID)
			d.store.UpdateStatus(info.Name, "stopped")
			RemoveAppPortFile(info.Name)

			// Clean up all components belonging to this app.
			d.cleanupAppComponents(info.Name)
		} else {
			// Best-effort health endpoint check with short timeout.
			d.tryHealthEndpoint(info.Port)
		}
	}
}

// cleanupAppComponents uses the bidirectional App→Component mapping to
// remove an app from each component's Apps list. When no more apps use a
// component, the component process is killed and unregistered.
func (d *Daemon) cleanupAppComponents(appName string) {
	info, exists := d.store.Get(appName)
	if !exists {
		// Fallback: use ListByApp for backward compatibility.
		comps := d.compStore.ListByApp(appName)
		for _, comp := range comps {
			if comp.PID > 0 {
				if err := d.killProcess(comp.PID); err != nil {
					fmt.Fprintf(os.Stderr, "[daemon] warn: kill component %s (pid %d): %v\n",
						comp.Name, comp.PID, err)
				}
			}
			d.compStore.Unregister(comp.Name)
			RemoveComponentPortFile(appName, comp.Name)
		}
		return
	}

	for _, compName := range info.Components {
		compInfo, ok := d.compStore.Get(compName)
		if !ok {
			continue
		}
		remaining := d.compStore.RemoveApp(compName, appName)
		if remaining == 0 {
			// No more apps using this component — shut it down.
			if compInfo.Driver == component.DriverProcess && compInfo.PID > 0 {
				if err := d.killProcess(compInfo.PID); err != nil {
					fmt.Fprintf(os.Stderr, "[daemon] warn: kill component %s (pid %d): %v\n",
						compName, compInfo.PID, err)
				}
				d.releasePort(parsePortFromTarget(compInfo.Target))
			}
			d.compStore.Unregister(compName)
			RemoveComponentPortFile(appName, compName)
		}
	}
}

// tryHealthEndpoint performs a best-effort ping to the app's health endpoint.
// Uses a short timeout and never blocks the health check loop.
func (d *Daemon) tryHealthEndpoint(port string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1"+port+"/api/v1/info", nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Component health check (v0.9 — separate goroutine from app health check)
// ---------------------------------------------------------------------------

// StartComponentHealthCheck launches a background goroutine that periodically
// checks whether managed component processes are still alive and responsive.
// For process-driver components: checks PID liveness, then gRPC health ping.
// If dead/unreachable, attempts auto-restart (max 3 retries). Default interval
// is 15 seconds. The goroutine exits when ctx is cancelled.
func (d *Daemon) StartComponentHealthCheck(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				d.runComponentHealthCheck()
			}
		}
	}()
}

// runComponentHealthCheck iterates over all registered components and checks
// their health. Behavior varies by driver type:
//   - process: PID liveness + gRPC health ping; dead → auto-restart (max 3 retries)
//   - http: gRPC health ping (no PID check); unreachable → mark "error"
//   - native: status stays "running" (App healthy = component healthy)
// Components with status "error" are skipped (retries exhausted).
func (d *Daemon) runComponentHealthCheck() {
	comps := d.compStore.List()
	for _, info := range comps {
		// Skip components that have exceeded retry limits.
		if info.Status == "error" {
			continue
		}

		switch info.Driver {
		case component.DriverProcess:
			if info.PID == 0 {
				continue
			}
			alive := d.isProcessAlive(info.PID)
			if alive {
				if err := d.grpcHealthCheck(info.Target); err != nil {
					fmt.Fprintf(os.Stderr, "[daemon] component %s (pid %d) gRPC health failed: %v — restarting\n",
						info.Name, info.PID, err)
					d.killProcess(info.PID)
					alive = false
				}
			}
			if !alive {
				fmt.Fprintf(os.Stderr, "[daemon] component %s (pid %d) is dead — attempting restart\n",
					info.Name, info.PID)
				if err := d.restartComponent(&info); err != nil {
					fmt.Fprintf(os.Stderr, "[daemon] component %s restart failed: %v\n", info.Name, err)
				}
			}

		case component.DriverHTTP:
			// gRPC health check (no PID check) — mark as "error" if unreachable.
			if info.Target == "" {
				continue
			}
			if err := d.grpcHealthCheck(info.Target); err != nil {
				fmt.Fprintf(os.Stderr, "[daemon] component %s http health failed: %v\n",
					info.Name, err)
				d.compStore.UpdateStatus(info.Name, "error")
			}

		case component.DriverNative:
			// Native components are tied to App lifecycle — status stays "running".
			// No independent health check possible.
		}
	}
}

// restartComponent attempts to restart a dead component process. It tracks
// retry count per component name. Max 3 retries within a 30-second window.
// If retries are exhausted, sets the component status to "error".
//
// On success: re-forks with same config, waits for gRPC health, updates
// ComponentStore with new PID.
func (d *Daemon) restartComponent(info *ComponentInfo) error {
	name := info.Name

	d.compRetryMu.Lock()
	count := d.compRetryCount[name]
	firstAttempt, hasFirst := d.compRetryFirstAttempt[name]
	now := time.Now()

	// Reset retry window if 30s have elapsed since first attempt.
	if hasFirst && now.Sub(firstAttempt) > 30*time.Second {
		count = 0
		delete(d.compRetryFirstAttempt, name)
	}

	if count >= 3 {
		d.compRetryMu.Unlock()
		// Update status to "error" and stop trying.
		d.compStore.UpdateStatus(name, "error")
		return fmt.Errorf("component %s exceeded max retries (3) — marked as error", name)
	}

	count++
	if !hasFirst || now.Sub(firstAttempt) > 30*time.Second {
		d.compRetryFirstAttempt[name] = now
	}
	d.compRetryCount[name] = count
	d.compRetryMu.Unlock()

	// Remove old port file (daemon writes a new one on success).
	RemoveComponentPortFile(info.AppName, info.Name)

	// Re-fork with same target port and type.
	portNum := 0
	if info.Target != "" {
		// Parse port from "localhost:XXXX"
		parts := strings.Split(info.Target, ":")
		if len(parts) >= 2 {
			portNum, _ = strconv.Atoi(parts[len(parts)-1])
		}
	}
	if portNum == 0 {
		return fmt.Errorf("cannot determine port for component %s from target %q", name, info.Target)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	attr := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	proc, err := os.StartProcess(exePath, []string{
		exePath, "component", "--name", info.Name, "--port", strconv.Itoa(portNum), "--type", info.Type,
	}, attr)
	if err != nil {
		return fmt.Errorf("re-fork component %s: %w", name, err)
	}

	fmt.Fprintf(os.Stderr, "[daemon] restarting component %s (attempt %d/3)\n", name, count)

	// Wait for gRPC health check (max 10s).
	if err := d.waitForComponentHealth(info.Target, 10*time.Second); err != nil {
		d.killProcess(proc.Pid)
		return fmt.Errorf("component %s health check after restart: %w", name, err)
	}

	// Update ComponentStore with new PID and status.
	d.compStore.UpdatePID(name, proc.Pid)
	d.compStore.UpdateStatus(name, "running")

	// Write new port file.
	if err := WriteComponentPortFile(info.AppName, info.Name, ":"+strconv.Itoa(portNum)); err != nil {
		fmt.Fprintf(os.Stderr, "[daemon] warn: write port file for %s: %v\n", name, err)
	}

	// Reset retry count on success.
	d.compRetryMu.Lock()
	delete(d.compRetryCount, name)
	delete(d.compRetryFirstAttempt, name)
	d.compRetryMu.Unlock()

	fmt.Fprintf(os.Stderr, "[daemon] component %s restarted successfully (new pid %d)\n", name, proc.Pid)
	return nil
}

// Shutdown gracefully stops all managed apps and their components. It iterates
// over all registered apps, sends a termination signal to each process,
// unregisters them from the store, removes port files, and cleans up component
// processes. Errors are aggregated and returned.
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

		// Unregister from store (best-effort).
		if err := d.store.Unregister(info.Name); err != nil {
			errs = append(errs, fmt.Errorf("unregister app %s: %w", info.Name, err))
		}

		// Clean up port file.
		RemoveAppPortFile(info.Name)
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}
