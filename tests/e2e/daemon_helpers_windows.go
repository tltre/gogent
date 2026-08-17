//go:build windows

package e2e_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// isProcessAlive checks whether a process with the given PID exists using tasklist.
func isProcessAlive(t *testing.T, pid int) bool {
	t.Helper()
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), strconv.Itoa(pid))
}

// killByPID terminates a process by its PID using taskkill.
func killByPID(t *testing.T, pid int) {
	t.Helper()
	killCmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
	_ = killCmd.Run()
}
