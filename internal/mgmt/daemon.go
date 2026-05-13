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

	"gopkg.in/yaml.v3"
)

// Daemon manages the lifecycle of App instances. It holds an AppStore,
// allocates ports, and launches independent OS child processes for each app.
type Daemon struct {
	store         *AppStore
	basePort      int
	mu            sync.Mutex
	reservedPorts map[int]bool // tracks ports allocated but not yet confirmed
}

// NewDaemon creates a new Daemon with an empty AppStore and the given base
// port for allocation. It scans ~/.gogent/ for surviving app instances from
// a previous daemon crash and recovers them.
func NewDaemon(basePort int) *Daemon {
	d := &Daemon{
		store:         NewAppStore(),
		basePort:      basePort,
		reservedPorts: make(map[int]bool),
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

// LoadApp reads the YAML config at configPath, allocates a port, forks a
// child process running "gogent agent", waits for it to become healthy, and
// registers it in the store. Returns the AppInfo on success.
func (d *Daemon) LoadApp(configPath string) (*AppInfo, error) {
	// 1. Read and parse YAML config to extract the app name.
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg struct {
		Name string `yaml:"name"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Name == "" {
		return nil, fmt.Errorf("config missing 'name' field")
	}

	// 2. Check for duplicate name.
	if _, exists := d.store.Get(cfg.Name); exists {
		return nil, fmt.Errorf("app %s already running", cfg.Name)
	}

	// 3. Allocate a port starting from basePort+1.
	port, err := d.allocatePort()
	if err != nil {
		return nil, fmt.Errorf("allocate port: %w", err)
	}
	portStr := strconv.Itoa(port)

	// 4. Fork a child process to run the agent.
	exePath, err := os.Executable()
	if err != nil {
		d.releasePort(port)
		return nil, fmt.Errorf("get executable path: %w", err)
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		d.releasePort(port)
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	attr := &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	proc, err := os.StartProcess(exePath, []string{
		exePath, "agent", "--config", absConfig, "--port", portStr,
	}, attr)
	if err != nil {
		d.releasePort(port)
		return nil, fmt.Errorf("fork agent: %w", err)
	}
	pid := proc.Pid

	// 5. Wait for agent to become healthy (max 10s, 200ms interval).
	client := NewClient(":" + portStr)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	ready := false
	for !ready {
		select {
		case <-ctx.Done():
			// Timeout – kill the forked process and release the port.
			d.killProcess(pid)
			d.releasePort(port)
			return nil, fmt.Errorf("agent %s did not become healthy within 10s", cfg.Name)
		case <-ticker.C:
			if err := client.Ping(); err == nil {
				ready = true
			}
		}
	}

	// Port is now confirmed occupied by the agent — remove from reserved set.
	d.releasePort(port)

	// 6. Create AppInfo and register in store.
	now := time.Now().Unix()
	info := &AppInfo{
		Name:       cfg.Name,
		Port:       ":" + portStr,
		PID:        pid,
		Status:     "running",
		ConfigPath: absConfig,
		StartedAt:  now,
	}
	if err := d.store.Register(info); err != nil {
		d.killProcess(pid)
		return nil, fmt.Errorf("register app: %w", err)
	}

	// Note: the agent process writes its own port file (agent.go:62) with the
	// correct PID. Do NOT write it here with the daemon's PID — it would
	// break crash recovery (RecoverApps would see the daemon PID as dead).

	return info, nil
}

// StopApp stops a running app by name. It sends a termination signal, waits
// up to 5s for the process to exit, unregisters it from the store, and
// removes the port file.
func (d *Daemon) StopApp(name string) error {
	info, exists := d.store.Get(name)
	if !exists {
		return fmt.Errorf("app %s not found", name)
	}

	// Kill the process.
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
func (d *Daemon) runHealthCheck() {
	apps := d.store.List()
	for _, info := range apps {
		if !d.isProcessAlive(info.PID) {
			// Process no longer alive – mark as stopped.
			fmt.Fprintf(os.Stderr, "[daemon] app %s stopped (pid %d no longer alive)\n", info.Name, info.PID)
			d.store.UpdateStatus(info.Name, "stopped")
			RemoveAppPortFile(info.Name)
		} else {
			// Best-effort health endpoint check with short timeout.
			d.tryHealthEndpoint(info.Port)
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

// Shutdown gracefully stops all managed apps. It iterates over all registered
// apps, sends a termination signal to each process, unregisters them from the
// store, and removes port files. Errors are aggregated and returned.
func (d *Daemon) Shutdown(ctx context.Context) error {
	apps := d.store.List()
	var errs []error

	for _, info := range apps {
		// Kill the process.
		if err := d.killProcess(info.PID); err != nil {
			errs = append(errs, fmt.Errorf("kill app %s (pid %d): %w", info.Name, info.PID, err))
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
