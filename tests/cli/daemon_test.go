package cli_test

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tltre/gogent/internal/api"
)

// gogentExe holds the path to the compiled gogent binary, built once in TestMain.
var gogentExe string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "gogent-cli-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	ext := ""
	if os.PathSeparator == '\\' {
		ext = ".exe"
	}
	gogentExe = filepath.Join(tmpDir, "gogent"+ext)

	buildCmd := exec.Command("go", "build", "-o", gogentExe, "../../cmd/gogent")
	out, err := buildCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build gogent: %v\n%s\n", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// startDaemon starts a gogent daemon process on the given port and waits for
// it to become ready. The daemon is killed via t.Cleanup.
// NOTE: --foreground is REQUIRED — without it the daemon command forks a
// background child that escapes the test's process tracking and leaks ports.
// NOTE: Stdout/Stderr are left nil so the daemon inherits the test's
// stdout/stderr directly.  This avoids pipe goroutines that would block
// cmd.Wait() after Kill on Windows.
func startDaemon(t *testing.T, port string) {
	t.Helper()
	rawPort := strings.TrimPrefix(port, ":")
	cmd := exec.Command(gogentExe, "daemon", "--foreground", "--port", rawPort)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}

	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		// Clean up daemon port file so subsequent tests don't see a stale entry.
		cleanDaemonPortFile()
	})

	waitForDaemonReady(t, port)
}

// waitForDaemonReady polls the daemon info endpoint until it responds OK.
func waitForDaemonReady(t *testing.T, port string) {
	t.Helper()
	url := "http://127.0.0.1" + port + api.PathDaemonInfo
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
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
	t.Fatal("daemon did not become ready within timeout")
}

// cleanDaemonPortFile removes the daemon port file from ~/.gogent/.
func cleanDaemonPortFile() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	os.Remove(filepath.Join(home, ".gogent", "daemon.pid"))
}

// writeMinimalConfig writes a minimal YAML config for a test app and returns
// the path. The file is cleaned up via t.Cleanup.
func writeMinimalConfig(t *testing.T, appName string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, appName+".yaml")
	content := fmt.Sprintf("name: %s\n", appName)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// runGogent runs the gogent binary with the given arguments and returns
// stdout, stderr, and any error.
func runGogent(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(gogentExe, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// ---------------------------------------------------------------------------
// Test: gogent list with daemon up but no apps
// ---------------------------------------------------------------------------

func TestCLIListNoApps(t *testing.T) {
	const port = ":19090"
	startDaemon(t, port)

	stdout, _, err := runGogent(t, "list", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent list: %v", err)
	}
	if !strings.Contains(stdout, "no apps running") {
		t.Errorf("expected 'no apps running', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent list with daemon + apps
// ---------------------------------------------------------------------------

func TestCLIListWithApps(t *testing.T) {
	const port = ":19091"
	startDaemon(t, port)

	cfgPath := writeMinimalConfig(t, "test-list-app")

	// Serve an app through the daemon.
	serveOut, serveErr, err := runGogent(t, "serve", cfgPath, "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent serve: %v\nstderr: %s", err, serveErr)
	}
	t.Logf("serve output: %s", serveOut)

	// List should now show the app.
	stdout, _, err := runGogent(t, "list", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent list: %v", err)
	}
	if !strings.Contains(stdout, "test-list-app") {
		t.Errorf("expected list to contain 'test-list-app', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent stop <name> — success
// ---------------------------------------------------------------------------

func TestCLIStopSuccess(t *testing.T) {
	const port = ":19092"
	startDaemon(t, port)

	cfgPath := writeMinimalConfig(t, "test-stop-app")

	// Serve the app.
	_, _, err := runGogent(t, "serve", cfgPath, "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent serve: %v", err)
	}

	// Stop the app.
	stdout, _, err := runGogent(t, "stop", "test-stop-app", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent stop: %v", err)
	}
	if !strings.Contains(stdout, "stopped") {
		t.Errorf("expected 'stopped', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent stop <name> — app not found
// ---------------------------------------------------------------------------

func TestCLIStopNotFound(t *testing.T) {
	const port = ":19093"
	startDaemon(t, port)

	stdout, _, err := runGogent(t, "stop", "nonexistent-app", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent stop: %v", err)
	}
	if !strings.Contains(stdout, "not found") {
		t.Errorf("expected 'not found', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent restart <name> — success
// ---------------------------------------------------------------------------

func TestCLIRestartSuccess(t *testing.T) {
	const port = ":19094"
	startDaemon(t, port)

	cfgPath := writeMinimalConfig(t, "test-restart-app")

	// Serve the app first.
	_, _, err := runGogent(t, "serve", cfgPath, "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent serve: %v", err)
	}

	// Restart the app.
	stdout, _, err := runGogent(t, "restart", "test-restart-app", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent restart: %v", err)
	}
	if !strings.Contains(stdout, "restarted") {
		t.Errorf("expected 'restarted', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent status (no args) — daemon mode
// ---------------------------------------------------------------------------

func TestCLIStatusNoArg(t *testing.T) {
	const port = ":19095"
	startDaemon(t, port)

	stdout, _, err := runGogent(t, "status", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent status: %v", err)
	}
	// When daemon is running with no apps, status reports "no apps running".
	// If daemon is detected, it always prints the app table header or "no apps running".
	if !strings.Contains(stdout, "no apps running") && !strings.Contains(stdout, "NAME") {
		t.Errorf("expected daemon status output, got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent restart <name> — app not found
// ---------------------------------------------------------------------------

func TestCLIRestartNotFound(t *testing.T) {
	const port = ":19096"
	startDaemon(t, port)

	stdout, _, err := runGogent(t, "restart", "nonexistent-app", "--port", strings.TrimPrefix(port, ":"))
	if err != nil {
		t.Fatalf("gogent restart: %v", err)
	}
	if !strings.Contains(stdout, "not found") {
		t.Errorf("expected 'not found', got: %s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent --help does NOT show "agent" command (hidden)
// ---------------------------------------------------------------------------

func TestCLIDaemonHidden(t *testing.T) {
	stdout, _, err := runGogent(t, "--help")
	if err != nil {
		t.Fatalf("gogent --help: %v", err)
	}
	if strings.Contains(stdout, "  agent") {
		t.Errorf("'agent' subcommand should be hidden from --help output:\n%s", stdout)
	}
	// But "daemon" should be visible.
	if !strings.Contains(stdout, "  daemon") {
		t.Errorf("'daemon' subcommand should be visible in --help output:\n%s", stdout)
	}
}

// ---------------------------------------------------------------------------
// Test: gogent daemon --help shows flags
// ---------------------------------------------------------------------------

func TestCLIDaemonHelp(t *testing.T) {
	stdout, _, err := runGogent(t, "daemon", "--help")
	if err != nil {
		t.Fatalf("gogent daemon --help: %v", err)
	}
	if !strings.Contains(stdout, "--base-port") {
		t.Errorf("expected --base-port flag in daemon --help:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--port") {
		t.Errorf("expected --port flag in daemon --help:\n%s", stdout)
	}
}
