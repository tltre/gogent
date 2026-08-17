//go:build !windows

package e2e_test

import (
	"syscall"
	"testing"
)

// isProcessAlive checks whether a process with the given PID exists.
// On Unix, this sends signal 0 (null signal) and checks for an error.
func isProcessAlive(t *testing.T, pid int) bool {
	t.Helper()
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

// killByPID terminates a process by its PID using SIGKILL.
func killByPID(t *testing.T, pid int) {
	t.Helper()
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
