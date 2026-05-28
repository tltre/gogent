package mgmt

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// EnsureDaemon detects or starts the daemon process and returns a connected
// Client pointing to the daemon's HTTP API.
func EnsureDaemon(mgmtPort string) (*Client, error) {
	daemonPort, err := detectOrStartDaemon(mgmtPort)
	if err != nil {
		return nil, fmt.Errorf("daemon unavailable: %w", err)
	}
	return NewClient(daemonPort), nil
}

// detectOrStartDaemon tries to find a running daemon via the port file, then
// via a ping. If no daemon is found, it forks a new daemon process and waits
// for it to become ready. Returns the daemon's port on success.
func detectOrStartDaemon(mgmtPort string) (string, error) {
	// 1. Try to read the daemon port file.
	if pf, err := ReadDaemonPortFile(); err == nil {
		c := NewClient(pf.Port)
		if err := c.Ping(); err == nil {
			return pf.Port, nil
		}
		// Port file is stale — remove it.
		RemoveDaemonPortFile()
	}

	// 2. Fork a new daemon process.
	if err := startDaemonBackground(mgmtPort); err != nil {
		return "", fmt.Errorf("start daemon: %w", err)
	}

	// 3. Wait for the daemon to become ready (max 5s, 500ms interval).
	if err := waitForDaemon(mgmtPort, 5*time.Second); err != nil {
		return "", fmt.Errorf("daemon not ready: %w", err)
	}

	return mgmtPort, nil
}

// startDaemonBackground forks a gogent daemon process. It uses the current
// executable path and passes the --port flag.
func startDaemonBackground(daemonPort string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Strip the ":" prefix for the --port flag so the new process's
	// PersistentPreRunE can normalize it again.
	rawPort := strings.TrimPrefix(daemonPort, ":")

	cmd := exec.Command(exePath, "daemon", "--port", rawPort)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	detachDaemon(cmd)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon process: %w", err)
	}

	return nil
}

// waitForDaemon polls the daemon's /api/v1/info endpoint until it responds
// successfully or the timeout elapses.
func waitForDaemon(daemonPort string, timeout time.Duration) error {
	url := "http://127.0.0.1" + daemonPort + "/api/v1/info"
	client := &http.Client{Timeout: 500 * time.Millisecond}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not start within %v", timeout)
}
