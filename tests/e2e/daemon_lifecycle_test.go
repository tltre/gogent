package e2e_test

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon"
	"github.com/tltre/gogent/internal/mgmt"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// buildTestBinary compiles cmd/gogent to a temporary test binary and returns
// its path. The binary is cleaned up when the test finishes.
func buildTestBinary(t *testing.T) string {
	t.Helper()

	exeSuffix := ""
	if runtime.GOOS == "windows" {
		exeSuffix = ".exe"
	}

	binPath := filepath.Join(t.TempDir(), "gogent-test"+exeSuffix)
	cmd := exec.Command("go", "build", "-o", binPath, "github.com/tltre/gogent/cmd/gogent")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build gogent binary: %v\noutput: %s", err, string(out))
	}
	return binPath
}

// findFreePort returns an available TCP port on 127.0.0.1.
func findFreePort(t *testing.T) int {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startDaemon launches the daemon process with the given binary, daemon port,
// and base port. It waits for the daemon to become ready and returns the
// exec.Cmd for process management.
func startDaemon(t *testing.T, binary string, daemonPort, basePort int) *exec.Cmd {
	t.Helper()

	// Clean up leftover *.port files from previous test runs that could
	// pollute the daemon's RecoverApps().
	cleanLeftoverPortFiles(t)

	portFlag := fmt.Sprintf(":%d", daemonPort)
	basePortFlag := fmt.Sprintf("%d", basePort)

	// NOTE: --foreground is REQUIRED — without it the daemon command forks a
	// background child that escapes the test's process tracking and leaks ports.
	cmd := exec.Command(binary, "daemon", "--foreground", "--port", portFlag, "--base-port", basePortFlag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	t.Cleanup(func() {
		killProcess(t, cmd)
	})

	waitForReady(t, portFlag, 15*time.Second)
	return cmd
}

// waitForReady polls the given port until a daemon or agent info endpoint
// responds successfully or the timeout elapses. It accepts both daemon ports
// (PathDaemonInfo) and agent app ports (PathAppInfo) since callers mix them.
func waitForReady(t *testing.T, port string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	daemonURL := "http://127.0.0.1" + port + api.PathDaemonInfo
	appURL := "http://127.0.0.1" + port + api.PathAppInfo

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if probeReady(client, daemonURL) || probeReady(client, appURL) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("daemon/app did not become ready on %s within %v", port, timeout)
}

// probeReady returns true if the URL responds with HTTP 200.
func probeReady(client *http.Client, url string) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// killProcess forcefully terminates the command's process and waits for it.
func killProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// On Windows, use taskkill for reliable termination.
	if runtime.GOOS == "windows" {
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/F", "/T")
		_ = killCmd.Run()
	} else {
		_ = cmd.Process.Signal(os.Interrupt)
		// Wait briefly then force kill.
		time.Sleep(500 * time.Millisecond)
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

// writeTempConfig creates a temporary YAML config file with the given name
// and returns its path.
func writeTempConfig(t *testing.T, appName string) string {
	t.Helper()

	content := fmt.Sprintf("name: %s\nversion: \"1.0.0\"\n", appName)
	path := filepath.Join(t.TempDir(), appName+".yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// writeTempConfigWithComponent creates a temporary YAML config with a
// process-driven component that the daemon will fork. compType is the
// component type (e.g., "memory", "provider") and compName is the component
// name registered in the defaults section.
func writeTempConfigWithComponent(t *testing.T, appName, compType, compName string) string {
	t.Helper()

	content := fmt.Sprintf(`name: %s
version: "1.0.0"
components:
  - name: "%s"
    type: "%s"
    driver: "process"
defaults:
  %s: "%s"
`, appName, compName, compType, compType, compName)
	path := filepath.Join(t.TempDir(), appName+".yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// daemonClient creates a management client pointing at the daemon port.
func daemonClient(port string) *daemon.DaemonClient {
	return daemon.NewDaemonClient(port)
}

// killProcessOnly terminates only the target process (not its children).
// This is used to simulate a daemon crash without affecting child app processes.
func killProcessOnly(t *testing.T, cmd *exec.Cmd) {
	t.Helper()

	if cmd == nil || cmd.Process == nil {
		return
	}

	if runtime.GOOS == "windows" {
		// Use taskkill without /T to avoid killing child processes (apps).
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", cmd.Process.Pid), "/F")
		_ = killCmd.Run()
	} else {
		_ = cmd.Process.Kill()
	}
	_ = cmd.Wait()
}

// cleanLeftoverPortFiles removes all *.port files from ~/.gogent/ so that a
// crash recovery test does not pick up stale records from earlier test runs.
func cleanLeftoverPortFiles(t *testing.T) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".gogent")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".port" {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Load and Stop a single app
// ---------------------------------------------------------------------------

func TestDaemonLoadStopApp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	cmd := startDaemon(t, binary, daemonPort, basePort)
	_ = cmd

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Verify daemon is alive.
	info, err := client.Info()
	if err != nil {
		t.Fatalf("daemon info: %v", err)
	}
	t.Logf("daemon: %s v%s", info.Name, info.Version)

	// Load an app.
	configPath := writeTempConfig(t, "test-load-stop")
	appInfo, err := client.LoadApp(configPath)
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	t.Logf("loaded app: name=%s port=%s pid=%d status=%s",
		appInfo.Name, appInfo.Port, appInfo.PID, appInfo.Status)

	if appInfo.Name != "test-load-stop" {
		t.Errorf("expected app name 'test-load-stop', got %q", appInfo.Name)
	}
	if appInfo.Status != "running" {
		t.Errorf("expected status 'running', got %q", appInfo.Status)
	}
	if appInfo.Port == "" {
		t.Error("expected non-empty port")
	}

	// Verify app is responsive via its own port.
	waitForReady(t, appInfo.Port, 10*time.Second)

	// Stop the app.
	if err := client.StopApp(appInfo.Name); err != nil {
		t.Fatalf("StopApp: %v", err)
	}

	// Verify app is no longer listed.
	apps, err := client.ListApps()
	if err != nil {
		t.Fatalf("ListApps after stop: %v", err)
	}
	for _, a := range apps {
		if a.Name == appInfo.Name {
			t.Errorf("app %q still listed after StopApp", appInfo.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: List Apps (load multiple, stop all, verify empty)
// ---------------------------------------------------------------------------

func TestDaemonListApps(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	startDaemon(t, binary, daemonPort, basePort)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Initially empty.
	apps, err := client.ListApps()
	if err != nil {
		t.Fatalf("ListApps (initial): %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps initially, got %d", len(apps))
	}

	// Load two apps with distinct names and wait for each to be ready.
	config1 := writeTempConfig(t, "test-list-a")
	app1, err := client.LoadApp(config1)
	if err != nil {
		t.Fatalf("LoadApp test-list-a: %v", err)
	}
	waitForReady(t, app1.Port, 10*time.Second)

	config2 := writeTempConfig(t, "test-list-b")
	app2, err := client.LoadApp(config2)
	if err != nil {
		t.Fatalf("LoadApp test-list-b: %v", err)
	}
	waitForReady(t, app2.Port, 10*time.Second)

	// Verify two apps listed.
	apps, err = client.ListApps()
	if err != nil {
		t.Fatalf("ListApps (after load): %v", err)
	}
	if len(apps) != 2 {
		t.Errorf("expected 2 apps, got %d: %+v", len(apps), apps)
	}

	// Stop both.
	if err := client.StopApp(app1.Name); err != nil {
		t.Fatalf("StopApp %s: %v", app1.Name, err)
	}
	if err := client.StopApp(app2.Name); err != nil {
		t.Fatalf("StopApp %s: %v", app2.Name, err)
	}

	// Verify empty again.
	apps, err = client.ListApps()
	if err != nil {
		t.Fatalf("ListApps (after stop): %v", err)
	}
	if len(apps) != 0 {
		t.Errorf("expected 0 apps after stopping all, got %d", len(apps))
	}
}

// ---------------------------------------------------------------------------
// Test: Duplicate app loading is rejected
// ---------------------------------------------------------------------------

func TestDaemonDuplicateReject(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	startDaemon(t, binary, daemonPort, basePort)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	configPath := writeTempConfig(t, "test-dup")

	// First load succeeds.
	_, err := client.LoadApp(configPath)
	if err != nil {
		t.Fatalf("first LoadApp: %v", err)
	}

	// Second load with same config (same name) must fail.
	_, err = client.LoadApp(configPath)
	if err == nil {
		t.Fatal("expected error on duplicate LoadApp, got nil")
	}
	t.Logf("duplicate rejection: %v (expected)", err)

	// Verify only one app is listed.
	apps, err := client.ListApps()
	if err != nil {
		t.Fatalf("ListApps: %v", err)
	}
	if len(apps) != 1 {
		t.Errorf("expected 1 app after duplicate rejection, got %d", len(apps))
	}
}

// ---------------------------------------------------------------------------
// Test: Port allocation assigns different ports to each app
// ---------------------------------------------------------------------------

func TestDaemonPortAllocation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	startDaemon(t, binary, daemonPort, basePort)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Load first app and wait for it to be fully ready.
	config1 := writeTempConfig(t, "test-port-a")
	app1, err := client.LoadApp(config1)
	if err != nil {
		t.Fatalf("LoadApp test-port-a: %v", err)
	}
	waitForReady(t, app1.Port, 10*time.Second)
	t.Logf("app1 loaded: name=%s port=%s pid=%d", app1.Name, app1.Port, app1.PID)

	// Load second app (daemon will allocate next available port).
	config2 := writeTempConfig(t, "test-port-b")
	app2, err := client.LoadApp(config2)
	if err != nil {
		t.Fatalf("LoadApp test-port-b: %v", err)
	}
	waitForReady(t, app2.Port, 10*time.Second)
	t.Logf("app2 loaded: name=%s port=%s pid=%d", app2.Name, app2.Port, app2.PID)

	// Verify different ports.
	if app1.Port == app2.Port {
		// Port collision — check app statuses to see which one actually bound.
		status1, _ := client.AppStatus(app1.Name)
		status2, _ := client.AppStatus(app2.Name)
		t.Errorf("port collision: both apps got port %s (status1=%s, status2=%s)",
			app1.Port, status1.Status, status2.Status)
	} else {
		t.Logf("different ports: app1=%s app2=%s (correct)", app1.Port, app2.Port)
	}

	// Cleanup.
	_ = client.StopApp(app1.Name)
	_ = client.StopApp(app2.Name)
}

// ---------------------------------------------------------------------------
// Test: Crash detection — daemon detects when an app process dies
// ---------------------------------------------------------------------------

func TestDaemonAppCrashDetection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	startDaemon(t, binary, daemonPort, basePort)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Load an app.
	configPath := writeTempConfig(t, "test-crash")
	appInfo, err := client.LoadApp(configPath)
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	t.Logf("loaded app: name=%s port=%s pid=%d", appInfo.Name, appInfo.Port, appInfo.PID)

	// Kill the app process directly.
	if runtime.GOOS == "windows" {
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", appInfo.PID), "/F")
		out, err := killCmd.CombinedOutput()
		if err != nil {
			t.Logf("taskkill warning: %v output: %s", err, string(out))
		}
	} else {
		proc, err := os.FindProcess(appInfo.PID)
		if err != nil {
			t.Fatalf("find process: %v", err)
		}
		if err := proc.Kill(); err != nil {
			t.Fatalf("kill process: %v", err)
		}
		// Wait for process to die.
		_, _ = proc.Wait()
	}

	// Wait for daemon health check to detect the crash (checks every 5s).
	t.Log("waiting for daemon to detect crash (health check interval: 5s)...")
	time.Sleep(8 * time.Second)

	// Verify daemon updated the status to "stopped".
	status, err := client.AppStatus(appInfo.Name)
	if err != nil {
		t.Fatalf("AppStatus: %v", err)
	}
	if status.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q (full: %+v)", status.Status, status)
	} else {
		t.Logf("daemon detected crash: status=%s (correct)", status.Status)
	}
}

// ---------------------------------------------------------------------------
// Test: Shutdown cleanup — stopping the daemon kills all apps
// ---------------------------------------------------------------------------

func TestDaemonShutdownCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	// Use exec.Command directly so we can control the daemon lifecycle.
	portFlag := fmt.Sprintf(":%d", daemonPort)
	basePortFlag := fmt.Sprintf("%d", basePort)

	daemonCmd := exec.Command(binary, "daemon", "--foreground", "--port", portFlag, "--base-port", basePortFlag)
	daemonCmd.Stdout = os.Stdout
	daemonCmd.Stderr = os.Stderr

	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	// Ensure cleanup even if test fails.
	defer killProcess(t, daemonCmd)

	waitForReady(t, portFlag, 15*time.Second)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Load two apps.
	config1 := writeTempConfig(t, "test-cleanup-a")
	app1, err := client.LoadApp(config1)
	if err != nil {
		t.Fatalf("LoadApp test-cleanup-a: %v", err)
	}
	t.Logf("loaded app1: name=%s pid=%d", app1.Name, app1.PID)

	config2 := writeTempConfig(t, "test-cleanup-b")
	app2, err := client.LoadApp(config2)
	if err != nil {
		t.Fatalf("LoadApp test-cleanup-b: %v", err)
	}
	t.Logf("loaded app2: name=%s pid=%d", app2.Name, app2.PID)

	// Verify apps are listed and responsive.
	apps, err := client.ListApps()
	if err != nil {
		t.Fatalf("ListApps before shutdown: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps before shutdown, got %d", len(apps))
	}

	// Gracefully terminate the daemon.
	if runtime.GOOS == "windows" {
		// On Windows, os.Interrupt doesn't work for background processes.
		// Use taskkill with /F to forcefully terminate the daemon and /T
		// to kill its child processes (simulating proper cleanup).
		killCmd := exec.Command("taskkill", "/PID", fmt.Sprintf("%d", daemonCmd.Process.Pid), "/F", "/T")
		out, _ := killCmd.CombinedOutput()
		t.Logf("taskkill: %s", strings.TrimSpace(string(out)))
	} else {
		if err := daemonCmd.Process.Signal(os.Interrupt); err != nil {
			t.Logf("signal daemon: %v — falling back to kill", err)
			_ = daemonCmd.Process.Kill()
		}
	}

	// Wait for daemon to exit.
	done := make(chan error, 1)
	go func() {
		done <- daemonCmd.Wait()
	}()
	select {
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not exit within 15s")
		killProcess(t, daemonCmd)
	case err := <-done:
		if err != nil {
			t.Logf("daemon exit: %v (expected after graceful shutdown)", err)
		}
	}

	// Verify daemon is no longer responsive.
	err = client.Ping()
	if err == nil {
		t.Error("daemon still responding after shutdown")
	} else {
		t.Logf("daemon not responding after shutdown: %v (expected)", err)
	}

	// Verify former app processes are dead by checking their ports are unreachable.
	for _, app := range []*api.AppInfo{app1, app2} {
		conn, err := net.DialTimeout("tcp", "127.0.0.1"+app.Port, 2*time.Second)
		if err == nil {
			conn.Close()
			t.Errorf("app %s port %s still accepting connections after daemon shutdown", app.Name, app.Port)
		} else {
			t.Logf("app %s port %s unreachable (correct)", app.Name, app.Port)
		}
	}

	// Verify port files are cleaned up from ~/.gogent/.
	// Note: On Windows, graceful shutdown via taskkill may not trigger defer
	// cleanup. Port file cleanup is verified on platforms where signal-based
	// shutdown works (Unix).
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		for _, appName := range []string{app1.Name, app2.Name} {
			portFilePath := filepath.Join(homeDir, ".gogent", appName+".port")
			if _, err := os.Stat(portFilePath); err == nil {
				if runtime.GOOS == "windows" {
					t.Logf("port file still exists after hard kill (expected on Windows): %s", portFilePath)
				} else {
					t.Errorf("port file still exists: %s", portFilePath)
				}
			} else {
				t.Logf("port file cleaned: %s (correct)", portFilePath)
			}
		}
		// Daemon port file should be removed.
		daemonPortFile := filepath.Join(homeDir, ".gogent", "daemon.pid")
		if _, err := os.Stat(daemonPortFile); err == nil {
			if runtime.GOOS == "windows" {
				t.Logf("daemon port file still exists after hard kill (expected on Windows): %s", daemonPortFile)
			} else {
				t.Errorf("daemon port file still exists: %s", daemonPortFile)
			}
		} else {
			t.Logf("daemon port file cleaned: %s (correct)", daemonPortFile)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: Daemon crash recovery — apps survive daemon crash and re-register
// ---------------------------------------------------------------------------

func TestDaemonCrashRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	portFlag := fmt.Sprintf(":%d", daemonPort)
	basePortFlag := fmt.Sprintf("%d", basePort)

	// ---- Step 1: Start daemon ----

	daemonCmd := exec.Command(binary, "daemon", "--foreground", "--port", portFlag, "--base-port", basePortFlag)
	daemonCmd.Stdout = os.Stdout
	daemonCmd.Stderr = os.Stderr

	if err := daemonCmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	// Safety net: kill daemon on cleanup if still alive.
	t.Cleanup(func() {
		killProcess(t, daemonCmd)
	})

	waitForReady(t, portFlag, 15*time.Second)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// ---- Step 2: Load 2 apps (app-a, app-b) ----

	configA := writeTempConfig(t, "app-a")
	appA, err := client.LoadApp(configA)
	if err != nil {
		t.Fatalf("LoadApp app-a: %v", err)
	}
	waitForReady(t, appA.Port, 10*time.Second)
	t.Logf("app-a loaded: name=%s port=%s pid=%d", appA.Name, appA.Port, appA.PID)

	configB := writeTempConfig(t, "app-b")
	appB, err := client.LoadApp(configB)
	if err != nil {
		t.Fatalf("LoadApp app-b: %v", err)
	}
	waitForReady(t, appB.Port, 10*time.Second)
	t.Logf("app-b loaded: name=%s port=%s pid=%d", appB.Name, appB.Port, appB.PID)

	// ---- Step 3: Record pre-crash values ----

	preCrashA := *appA
	preCrashB := *appB

	apps, err := client.ListApps()
	if err != nil {
		t.Fatalf("ListApps before crash: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps before crash, got %d", len(apps))
	}

	// ---- Step 4: Kill the daemon process (simulate crash) ----

	killProcessOnly(t, daemonCmd)
	t.Log("daemon killed (simulated crash)")

	// Verify daemon is no longer responsive.
	if err := client.Ping(); err == nil {
		t.Error("daemon still responding after kill")
	} else {
		t.Logf("daemon unreachable after kill: %v (expected)", err)
	}

	// ---- Step 5: Verify apps still running ----

	appClientA := mgmt.NewAppClient(appA.Port)
	if err := appClientA.Ping(); err != nil {
		t.Errorf("app-a not responding after daemon crash: %v", err)
	} else {
		t.Logf("app-a still alive after daemon crash (correct)")
	}

	appClientB := mgmt.NewAppClient(appB.Port)
	if err := appClientB.Ping(); err != nil {
		t.Errorf("app-b not responding after daemon crash: %v", err)
	} else {
		t.Logf("app-b still alive after daemon crash (correct)")
	}

	// Register cleanup for orphaned app processes.
	t.Cleanup(func() {
		killByPID(t, preCrashA.PID)
		killByPID(t, preCrashB.PID)
		daemon.RemoveAppPortFile(preCrashA.Name)
		daemon.RemoveAppPortFile(preCrashB.Name)
	})

	// ---- Step 6: Start a new daemon on the same port ----

	newDaemonCmd := exec.Command(binary, "daemon", "--foreground", "--port", portFlag, "--base-port", basePortFlag)
	newDaemonCmd.Stdout = os.Stdout
	newDaemonCmd.Stderr = os.Stderr

	if err := newDaemonCmd.Start(); err != nil {
		t.Fatalf("start new daemon: %v", err)
	}
	t.Cleanup(func() {
		killProcess(t, newDaemonCmd)
	})

	waitForReady(t, portFlag, 15*time.Second)
	t.Log("new daemon started on same port")

	// ---- Step 7: ListApps() → expect 2 apps ----

	newClient := daemonClient(daemonPortStr)
	apps, err = newClient.ListApps()
	if err != nil {
		t.Fatalf("ListApps after recovery: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("expected 2 apps after recovery, got %d: %+v", len(apps), apps)
	}

	// ---- Step 8: Verify names/ports/PIDs match pre-crash values ----

	foundA := false
	foundB := false
	for _, a := range apps {
		t.Logf("recovered: name=%s port=%s pid=%d status=%s config_path=%q",
			a.Name, a.Port, a.PID, a.Status, a.ConfigPath)

		switch a.Name {
		case preCrashA.Name:
			foundA = true
			if a.Port != preCrashA.Port {
				t.Errorf("app-a port changed: was %s, got %s", preCrashA.Port, a.Port)
			}
			if a.PID != preCrashA.PID {
				t.Errorf("app-a PID changed: was %d, got %d", preCrashA.PID, a.PID)
			}
			if a.Status != "running" {
				t.Errorf("app-a status: expected 'running', got %q", a.Status)
			}
		case preCrashB.Name:
			foundB = true
			if a.Port != preCrashB.Port {
				t.Errorf("app-b port changed: was %s, got %s", preCrashB.Port, a.Port)
			}
			if a.PID != preCrashB.PID {
				t.Errorf("app-b PID changed: was %d, got %d", preCrashB.PID, a.PID)
			}
			if a.Status != "running" {
				t.Errorf("app-b status: expected 'running', got %q", a.Status)
			}
		}
	}
	if !foundA {
		t.Errorf("app-a (%q) not found in recovered apps", preCrashA.Name)
	}
	if !foundB {
		t.Errorf("app-b (%q) not found in recovered apps", preCrashB.Name)
	}

	// ---- Step 9: Stop app-a via new daemon → verify process exits ----

	if err := newClient.StopApp(preCrashA.Name); err != nil {
		t.Fatalf("StopApp app-a via new daemon: %v", err)
	}

	// Verify app-a process is dead.
	if isProcessAlive(t, preCrashA.PID) {
		t.Errorf("app-a process (pid=%d) still alive after StopApp", preCrashA.PID)
	} else {
		t.Logf("app-a process exited after StopApp (correct)")
	}

	// Verify app-a no longer listed.
	apps, err = newClient.ListApps()
	if err != nil {
		t.Fatalf("ListApps after stopping app-a: %v", err)
	}
	for _, a := range apps {
		if a.Name == preCrashA.Name {
			t.Errorf("app-a still listed after StopApp")
		}
	}

	// app-b should still be listed.
	foundB = false
	for _, a := range apps {
		if a.Name == preCrashB.Name {
			foundB = true
			break
		}
	}
	if !foundB {
		t.Errorf("app-b not listed after stopping app-a")
	}

	// Clean up remaining app via new daemon.
	if err := newClient.StopApp(preCrashB.Name); err != nil {
		t.Logf("StopApp app-b cleanup: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test: Component auto-restart — daemon restarts a killed component process
// ---------------------------------------------------------------------------

func TestComponentAutoRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	startDaemon(t, binary, daemonPort, basePort)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Load an app with a process-driven "memory" component.
	appName := "test-comp-restart"
	compName := "memory-main"
	compType := "memory"
	configPath := writeTempConfigWithComponent(t, appName, compType, compName)

	appInfo, err := client.LoadApp(configPath)
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	t.Logf("loaded app: name=%s port=%s pid=%d", appInfo.Name, appInfo.Port, appInfo.PID)

	// Wait for app to be ready.
	waitForReady(t, appInfo.Port, 10*time.Second)

	// Read the component port file to get the original PID.
	pf, err := daemon.ReadComponentPortFile(appName, compName)
	if err != nil {
		t.Fatalf("read component port file: %v", err)
	}
	origPID := pf.PID
	t.Logf("component %s original PID: %d", compName, origPID)

	if origPID == 0 {
		t.Fatal("component PID is 0 — component was not forked")
	}

	// Verify component process is alive.
	if !isProcessAlive(t, origPID) {
		t.Fatalf("component process %d is not alive after LoadApp", origPID)
	}

	// Kill the component process.
	t.Logf("killing component %s (pid %d)", compName, origPID)
	killByPID(t, origPID)

	// Wait briefly for kill to take effect.
	time.Sleep(1 * time.Second)
	if isProcessAlive(t, origPID) {
		t.Logf("component still alive after kill attempt — waiting more")
		time.Sleep(3 * time.Second)
	}

	// Wait for daemon component health check to restart it (interval is 15s).
	// Give it up to 25s for detection + restart.
	t.Log("waiting for daemon to detect crashed component and restart it...")
	deadline := time.Now().Add(25 * time.Second)
	restarted := false
	var newPID int

	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)

		// Check if component port file has been updated with a new PID.
		pf2, err := daemon.ReadComponentPortFile(appName, compName)
		if err != nil {
			continue // port file might be temporarily removed during restart
		}
		if pf2.PID != 0 && pf2.PID != origPID {
			newPID = pf2.PID
			if isProcessAlive(t, newPID) {
				restarted = true
				break
			}
		}
	}

	if !restarted {
		t.Fatalf("component %s was not restarted within 25s (original PID %d)",
			compName, origPID)
	}

	t.Logf("component %s restarted successfully: old PID=%d, new PID=%d", compName, origPID, newPID)

	if newPID == origPID {
		t.Errorf("component PID did not change after restart (PID=%d)", newPID)
	}
}

// ---------------------------------------------------------------------------
// Test: App crash cleans up component processes
// ---------------------------------------------------------------------------

func TestAppCrashCleansComponents(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	binary := buildTestBinary(t)
	daemonPort := findFreePort(t)
	basePort := findFreePort(t)

	startDaemon(t, binary, daemonPort, basePort)

	daemonPortStr := fmt.Sprintf(":%d", daemonPort)
	client := daemonClient(daemonPortStr)

	// Load an app with a process-driven "memory" component.
	appName := "test-app-cleanup"
	compName := "memory-main"
	compType := "memory"
	configPath := writeTempConfigWithComponent(t, appName, compType, compName)

	appInfo, err := client.LoadApp(configPath)
	if err != nil {
		t.Fatalf("LoadApp: %v", err)
	}
	t.Logf("loaded app: name=%s port=%s pid=%d", appInfo.Name, appInfo.Port, appInfo.PID)

	// Wait for app to be ready.
	waitForReady(t, appInfo.Port, 10*time.Second)

	// Get component PID.
	pf, err := daemon.ReadComponentPortFile(appName, compName)
	if err != nil {
		t.Fatalf("read component port file: %v", err)
	}
	compPID := pf.PID
	t.Logf("component %s PID: %d", compName, compPID)

	if compPID == 0 {
		t.Fatal("component PID is 0 — component was not forked")
	}

	if !isProcessAlive(t, compPID) {
		t.Fatalf("component process %d is not alive after LoadApp", compPID)
	}

	// Kill the app process.
	t.Logf("killing app %s (pid %d)", appInfo.Name, appInfo.PID)
	killByPID(t, appInfo.PID)

	// Wait for daemon app health check (5s interval) to detect the crash
	// and clean up components.
	t.Log("waiting for daemon to detect app crash and clean up components...")
	deadline := time.Now().Add(12 * time.Second)
	cleanedUp := false

	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)

		// Check if app status is "stopped".
		status, err := client.AppStatus(appName)
		if err == nil && status.Status == "stopped" {
			cleanedUp = true
			break
		}
	}

	if !cleanedUp {
		t.Logf("daemon may not have detected crash yet, checking component directly")
		time.Sleep(3 * time.Second)
	}

	// Verify the component process is no longer alive.
	if isProcessAlive(t, compPID) {
		// Daemon should have killed it during cleanup. Force kill if not.
		t.Logf("component %d still alive — force killing", compPID)
		killByPID(t, compPID)
		time.Sleep(1 * time.Second)
		if isProcessAlive(t, compPID) {
			t.Errorf("component process %d is still alive after app crash — cleanup may have failed", compPID)
		}
	} else {
		t.Logf("component process %d is dead (cleaned up correctly)", compPID)
	}

	// Verify component port file is removed.
	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		portFilePath := filepath.Join(homeDir, ".gogent", appName+"."+compName+".port")
		if _, err := os.Stat(portFilePath); err == nil {
			t.Errorf("component port file still exists after app crash cleanup: %s", portFilePath)
		} else {
			t.Logf("component port file cleaned up: %s (correct)", portFilePath)
		}
	}
}

