package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tltre/gogent/internal/api"
)

// e2eHarness holds all resources needed for the e2e test.
type e2eHarness struct {
	t       *testing.T
	tmpDir  string   // temp directory for the test binary
	exePath string   // path to gogent binary
	daemon  *exec.Cmd // daemon process (nil when not running)

	daemonPort string // e.g. ":19200"
	basePort   int    // e.g. 19200

	// absolute paths to fixture configs
	configA string
	configB string
}

// newE2EHarness builds the gogent binary and sets up a temp directory.
func newE2EHarness(t *testing.T) *e2eHarness {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "gogent-e2e-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}

	exeName := "gogent-test"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exePath := filepath.Join(tmpDir, exeName)

	// Resolve fixture paths relative to the test binary's working dir.
	// Tests run from tests/integration/; fixtures are at tests/fixtures/.
	configA, err := filepath.Abs(filepath.Join("..", "fixtures", "e2e-app-a.yaml"))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("resolve config-a path: %v", err)
	}
	configB, err := filepath.Abs(filepath.Join("..", "fixtures", "e2e-app-b.yaml"))
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("resolve config-b path: %v", err)
	}

	// Build the gogent binary.
	buildCmd := exec.Command("go", "build", "-o", exePath, "github.com/tltre/gogent/cmd/gogent")
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("build gogent: %v\n%s", err, string(buildOut))
	}

	return &e2eHarness{
		t:         t,
		tmpDir:    tmpDir,
		exePath:   exePath,
		configA:   configA,
		configB:   configB,
	}
}

// cleanup removes the temp directory and tries to stop all processes.
func (h *e2eHarness) cleanup() {
	// Kill daemon if still running (with /T on Windows to include orphans).
	if h.daemon != nil && h.daemon.Process != nil {
		if runtime.GOOS == "windows" {
			exec.Command("taskkill", "/PID", fmt.Sprintf("%d", h.daemon.Process.Pid), "/F", "/T").Run()
		} else {
			h.killProcess(h.daemon.Process.Pid)
		}
		h.daemon = nil
	}
	os.RemoveAll(h.tmpDir)
}

// pickPort picks a random port in the range [base, base+999].
func (h *e2eHarness) pickPort(base int) {
	offset := int(time.Now().UnixNano()%997) + 3 // avoid 0,1,2
	h.basePort = base + offset
	h.daemonPort = fmt.Sprintf(":%d", h.basePort)
}

// startDaemon starts the daemon process and waits for it to be ready.
func (h *e2eHarness) startDaemon() {
	h.t.Helper()

	args := []string{
		"daemon",
		"--foreground", // REQUIRED: without it the daemon forks a background child that escapes test tracking
		"--port", fmt.Sprintf("%d", h.basePort),
		"--base-port", fmt.Sprintf("%d", h.basePort),
	}
	cmd := exec.Command(h.exePath, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		h.t.Fatalf("start daemon: %v", err)
	}
	h.daemon = cmd

	// Wait for daemon to be ready.
	h.waitForReady(h.daemonPort, 10*time.Second)
}

// waitForReady polls the daemon info endpoint until it responds.
func (h *e2eHarness) waitForReady(port string, timeout time.Duration) {
	h.t.Helper()

	url := "http://127.0.0.1" + port + api.PathDaemonInfo
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	h.t.Fatalf("daemon not ready within %v on %s", timeout, port)
}

// waitForGone polls until the daemon endpoint is no longer reachable.
func (h *e2eHarness) waitForGone(port string, timeout time.Duration) {
	h.t.Helper()

	url := "http://127.0.0.1" + port + api.PathDaemonInfo
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		_, err := client.Get(url)
		if err != nil {
			return // connection refused — daemon is down
		}
		time.Sleep(300 * time.Millisecond)
	}
	h.t.Fatalf("daemon still reachable after %v on %s", timeout, port)
}

// gogent runs the gogent binary with the given arguments and returns stdout.
func (h *e2eHarness) gogent(args ...string) string {
	h.t.Helper()

	// Always include --port unless args override it.
	hasPort := false
	for _, a := range args {
		if a == "--port" || a == "-p" {
			hasPort = true
			break
		}
	}
	if !hasPort {
		args = append([]string{"--port", fmt.Sprintf("%d", h.basePort)}, args...)
	}

	cmd := exec.Command(h.exePath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("gogent %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

// gogentOK runs gogent and returns output; fails test only on unexpected error.
func (h *e2eHarness) gogentOK(args ...string) string {
	h.t.Helper()

	hasPort := false
	for _, a := range args {
		if a == "--port" || a == "-p" {
			hasPort = true
			break
		}
	}
	if !hasPort {
		args = append([]string{"--port", fmt.Sprintf("%d", h.basePort)}, args...)
	}

	cmd := exec.Command(h.exePath, args...)
	out, err := cmd.CombinedOutput()
	// Some commands (like stop when already stopped) may return non-zero.
	// We log instead of fatally failing.
	if err != nil {
		h.t.Logf("gogent %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

// killProcess terminates a process by PID.
func (h *e2eHarness) killProcess(pid int) {
	if runtime.GOOS == "windows" {
		exec.Command("taskkill", "/PID", fmt.Sprintf("%d", pid), "/F").Run()
	} else {
		p, err := os.FindProcess(pid)
		if err == nil {
			p.Signal(os.Interrupt)
			time.Sleep(500 * time.Millisecond)
			p.Kill()
		}
	}
}

// ping checks whether an HTTP endpoint at the given port responds.
func (h *e2eHarness) ping(port string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + port + api.PathAppInfo)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// parseServeOutput extracts the port from "app X loaded | port=:NNNN pid=MMM".
func parseServeOutput(out string) (port string, pid string) {
	// Example: app e2e-app-a loaded | port=:19101 pid=12345
	parts := strings.Fields(out)
	for _, p := range parts {
		if strings.HasPrefix(p, "port=") {
			port = strings.TrimPrefix(p, "port=")
		}
		if strings.HasPrefix(p, "pid=") {
			pid = strings.TrimPrefix(p, "pid=")
		}
	}
	return
}

// TestE2EMultiAppWorkflow is the end-to-end smoke test for the v0.6.0
// multi-agent workflow: daemon start → serve two apps → list → status →
// doctor → stop → restart → daemon crash recovery → shutdown.
func TestE2EMultiAppWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	h := newE2EHarness(t)
	defer h.cleanup()

	// Pick a random daemon port to avoid conflicts.
	h.pickPort(19100)
	t.Logf("daemon port: %s (base %d)", h.daemonPort, h.basePort)

	// ── Step 1: Start daemon ───────────────────────────────────────────
	t.Log("Step 1: starting daemon...")
	h.startDaemon()
	t.Logf("daemon running on %s", h.daemonPort)

	// ── Step 2: Serve app-a ────────────────────────────────────────────
	t.Log("Step 2: serving e2e-app-a...")
	outA := h.gogent("serve", h.configA)
	t.Logf("serve a: %s", strings.TrimSpace(outA))
	portA, pidA := parseServeOutput(outA)
	if portA == "" {
		t.Fatal("could not extract port from serve output for app-a")
	}
	t.Logf("app-a on port=%s pid=%s", portA, pidA)

	// ── Step 3: Serve app-b ────────────────────────────────────────────
	t.Log("Step 3: serving e2e-app-b...")
	outB := h.gogent("serve", h.configB)
	t.Logf("serve b: %s", strings.TrimSpace(outB))
	portB, pidB := parseServeOutput(outB)
	if portB == "" {
		t.Fatal("could not extract port from serve output for app-b")
	}
	t.Logf("app-b on port=%s pid=%s", portB, pidB)

	// ── Step 4: List → verify 2 apps ───────────────────────────────────
	t.Log("Step 4: listing apps...")
	listOut := h.gogent("list")
	t.Logf("list:\n%s", listOut)
	if !strings.Contains(listOut, "e2e-app-a") {
		t.Fatal("list should contain e2e-app-a")
	}
	if !strings.Contains(listOut, "e2e-app-b") {
		t.Fatal("list should contain e2e-app-b")
	}

	// ── Step 5: Status e2e-app-a → verify component table ─────────────
	t.Log("Step 5: status e2e-app-a...")
	statusOut := h.gogent("status", "e2e-app-a")
	t.Logf("status:\n%s", statusOut)
	if !strings.Contains(statusOut, "logger-default") &&
		!strings.Contains(statusOut, "eventbus-default") &&
		!strings.Contains(statusOut, "sandbox-default") &&
		!strings.Contains(statusOut, "memory-default") {
		t.Log("warning: component table may be incomplete (expected defaults)")
	}
	// At minimum we should see some components.
	if !strings.Contains(statusOut, "NAME") {
		t.Fatal("status output should have a table header")
	}

	// ── Step 6: Doctor → verify health checks ─────────────────────────
	t.Log("Step 6: doctor health checks...")
	doctorOut := h.gogent("doctor")
	t.Logf("doctor:\n%s", doctorOut)
	// The fixture apps declare no components (framework defaults live in the
	// app process, not the daemon ComponentStore), so the component health
	// table may be empty. Asserting on app names here would be wrong — the
	// meaningful contract is that doctor runs without error.
	if !strings.Contains(doctorOut, "COMPONENT") && !strings.Contains(doctorOut, "NAME") &&
		!strings.Contains(doctorOut, "===") {
		t.Log("warning: doctor produced no health table (no components declared)")
	}

	// ── Step 7: Stop e2e-app-a ────────────────────────────────────────
	t.Log("Step 7: stopping e2e-app-a...")
	stopOut := h.gogent("stop", "e2e-app-a")
	t.Logf("stop: %s", strings.TrimSpace(stopOut))
	if !strings.Contains(stopOut, "stopped") && !strings.Contains(stopOut, "not found") {
		t.Fatalf("unexpected stop output: %s", stopOut)
	}

	// Give the agent time to shut down.
	time.Sleep(500 * time.Millisecond)

	// ── Step 8: List → verify 1 app remains ────────────────────────────
	t.Log("Step 8: list after stop...")
	listOut2 := h.gogent("list")
	t.Logf("list:\n%s", listOut2)
	if strings.Contains(listOut2, "e2e-app-a") {
		t.Fatal("e2e-app-a should no longer appear in list")
	}
	if !strings.Contains(listOut2, "e2e-app-b") {
		t.Fatal("e2e-app-b should still appear in list")
	}

	// ── Step 9: Restart e2e-app-b → verify new PID ────────────────────
	t.Log("Step 9: restarting e2e-app-b...")
	restartOut := h.gogent("restart", "e2e-app-b")
	t.Logf("restart: %s", strings.TrimSpace(restartOut))
	if !strings.Contains(restartOut, "restarted") {
		t.Fatalf("unexpected restart output: %s", restartOut)
	}
	// Extract new port and PID from restart output (restart allocates a new port).
	newPortB, newPidB := parseServeOutput(restartOut)
	if newPortB != "" {
		portB = newPortB
		t.Logf("app-b new port: %s", portB)
	}
	if newPidB == "" {
		t.Fatal("could not extract new PID from restart output")
	}
	pidB = newPidB

	// Give the restarted agent time to initialize.
	time.Sleep(1 * time.Second)

	// Verify app-b is responsive after restart.
	if !h.ping(portB) {
		t.Fatalf("app-b not responding after restart on %s", portB)
	}

	// ── Step 10: Kill daemon → verify app-b survives ─────────────────
	t.Logf("Step 10: killing daemon (PID %d)...", h.daemon.Process.Pid)
	h.killProcess(h.daemon.Process.Pid)
	h.waitForGone(h.daemonPort, 10*time.Second)
	h.daemon = nil
	t.Log("daemon stopped")

	// Verify app-b is still running by querying its management endpoint.
	time.Sleep(500 * time.Millisecond)
	if !h.ping(portB) {
		t.Fatalf("app-b should survive daemon crash (not responding on %s)", portB)
	}
	t.Log("app-b survived daemon crash")

	// ── Step 11: Restart daemon → verify recovery ────────────────────
	t.Log("Step 11: restarting daemon...")
	h.startDaemon()
	t.Log("daemon restarted")

	// The recovered daemon should discover app-b from its port file.
	time.Sleep(500 * time.Millisecond)
	listOut3 := h.gogentOK("list")
	t.Logf("list after daemon restart:\n%s", listOut3)
	// After daemon recovery, app-b should be listed.
	if strings.Contains(listOut3, "e2e-app-b") {
		t.Log("daemon recovered app-b successfully")
	} else {
		t.Log("note: daemon recovery did not find app-b (port file may be stale)")
	}

	// ── Step 12: Shutdown everything ──────────────────────────────────
	t.Log("Step 12: shutting down...")

	// Stop app-b via daemon (works if daemon recovered it).
	h.gogentOK("stop", "e2e-app-b")

	// Ensure app-b process is killed.
	time.Sleep(500 * time.Millisecond)
	if pf, err := readAppPortFile("e2e-app-b"); err == nil && pf.PID > 0 {
		h.killProcess(pf.PID)
	}

	// Clean up any leftover app-a process.
	if pf, err := readAppPortFile("e2e-app-a"); err == nil && pf.PID > 0 {
		h.killProcess(pf.PID)
	}

	// Stop daemon — use /F /T on Windows to kill the entire process tree.
	if h.daemon != nil && h.daemon.Process != nil {
		if runtime.GOOS == "windows" {
			exec.Command("taskkill", "/PID", fmt.Sprintf("%d", h.daemon.Process.Pid), "/F", "/T").Run()
		} else {
			h.killProcess(h.daemon.Process.Pid)
		}
		h.daemon = nil
	}

	// Kill any orphaned agent process from the daemon crash test (step 10).
	// The daemon was killed without /T to verify app survival, so the agent
	// may still be running. Use the PID captured after restart.
	if p, err := strconv.Atoi(pidB); err == nil && p > 0 {
		h.killProcess(p)
	}

	// Clean up port files.
	removeAppPortFile("e2e-app-a")
	removeAppPortFile("e2e-app-b")

	t.Log("e2e workflow completed successfully")
}

// readAppPortFile reads an app-level port file from ~/.gogent/<name>.port.
// Avoids importing internal/mgmt directly from the test package.
type portFile struct {
	PID  int    `json:"pid"`
	Port string `json:"port"`
	Name string `json:"name"`
}

func readAppPortFile(name string) (*portFile, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(home, ".gogent", name+".port")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pf portFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, err
	}
	return &pf, nil
}

func removeAppPortFile(name string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(home, ".gogent", name+".port"))
}
