package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/pkg/component"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

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
		// Resolve real PID from port file when daemon only has PID=0
		// (in-process agents register via LoadApp with needForkApplication=false).
		if info.PID <= 0 {
			if pf, err := ReadAppPortFile(info.Name); err == nil && pf.PID > 0 {
				d.store.UpdatePID(info.Name, pf.PID)
				info.PID = pf.PID
			}
		}

		if !d.isProcessAlive(info.PID) {
			// Process no longer alive – mark as stopped and clean up.
			// Only act on first detection; subsequent cycles skip already-stopped apps.
			if info.Status != "stopped" {
				fmt.Fprintf(os.Stderr, "[daemon] app %s stopped (pid %d no longer alive)\n", info.Name, info.PID)
				d.store.UpdateStatus(info.Name, "stopped")
				RemoveAppPortFile(info.Name)
				d.cleanupAppComponents(info.Name)
			}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1"+port+api.PathAppInfo, nil)
	if err != nil {
		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

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
//
// Components with status "error" are skipped (retries exhausted).
func (d *Daemon) runComponentHealthCheck() {
	comps := d.compStore.List()
	now := time.Now()

	for _, comp := range comps {
		// Skip native components (no independent process to check).
		if comp.Driver == component.DriverNative {
			continue
		}

		// Skip components already in error state (retries exhausted).
		if comp.Status == "error" {
			continue
		}

		var alive bool
		switch comp.Driver {
		case component.DriverProcess:
			alive = d.isProcessAlive(comp.PID)
		case component.DriverHTTP:
			alive = true // HTTP components are checked via gRPC ping below
		}

		var healthy bool
		if !alive {
			healthy = false
		} else {
			err := d.grpcHealthCheck(comp.Target)
			healthy = err == nil
		}

		if healthy {
			d.compStore.mu.Lock()
			comp.Status = "running"
			d.compStore.mu.Unlock()
			d.resetCompRetry(comp.Name)
		} else {
			d.handleCompFailure(comp, now)
		}
	}
}

// resetCompRetry clears the retry counters for a component on successful health check.
func (d *Daemon) resetCompRetry(name string) {
	d.compRetryMu.Lock()
	defer d.compRetryMu.Unlock()
	delete(d.compRetryCount, name)
	delete(d.compRetryFirstAttempt, name)
}

// handleCompFailure handles a failed health check for a component.
// It increments the retry counter and restarts if under max retries (3).
func (d *Daemon) handleCompFailure(comp api.ComponentInfo, now time.Time) {
	d.compRetryMu.Lock()
	d.compRetryCount[comp.Name]++
	if d.compRetryFirstAttempt[comp.Name].IsZero() {
		d.compRetryFirstAttempt[comp.Name] = now
	}
	retryCount := d.compRetryCount[comp.Name]
	d.compRetryMu.Unlock()

	maxRetries := 3
	if retryCount >= maxRetries {
		fmt.Fprintf(os.Stderr, "[daemon] component %s unhealthy after %d retries, giving up\n",
			comp.Name, maxRetries)
		d.compStore.mu.Lock()
		comp.Status = "error"
		d.compStore.mu.Unlock()
		return
	}

	// Attempt auto-restart for process-driver components.
	if comp.Driver == component.DriverProcess {
		fmt.Fprintf(os.Stderr, "[daemon] component %s unhealthy, restarting (attempt %d/%d)\n",
			comp.Name, retryCount, maxRetries)

		// Kill existing process if somehow still alive.
		if comp.PID > 0 {
			d.killProcess(comp.PID)
		}

		// Re-fork.
		port, err := d.allocatePort()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] restart %s: %v\n", comp.Name, err)
			return
		}
		portStr := strconv.Itoa(port)
		target := "localhost:" + portStr

		exePath, err := os.Executable()
		if err != nil {
			d.releasePort(port)
			fmt.Fprintf(os.Stderr, "[daemon] restart %s: %v\n", comp.Name, err)
			return
		}

		attr := &os.ProcAttr{
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		}
		args := []string{exePath, "component", "--name", comp.Name, "--port", portStr, "--type", comp.Type}
		proc, err := os.StartProcess(exePath, args, attr)
		if err != nil {
			d.releasePort(port)
			fmt.Fprintf(os.Stderr, "[daemon] restart %s: %v\n", comp.Name, err)
			return
		}

		// Update ComponentStore.
		d.compStore.mu.Lock()
		comp.PID = proc.Pid
		comp.Target = target
		comp.Status = "running"
		// Write back to the store's internal map.
		if existing, ok := d.compStore.Get(comp.Name); ok {
			existing.PID = proc.Pid
			existing.Target = target
			existing.Status = "running"
		}
		d.compStore.mu.Unlock()

		// Wait for gRPC health.
		if err := d.waitForComponentHealth(target, 10*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "[daemon] restart %s: health check after restart: %v\n",
				comp.Name, err)
		}
	}
}
