//go:build !windows

package mgmt

import (
	"syscall"
)

// killProcess sends SIGTERM to the process with the given PID.
func (d *Daemon) killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// isProcessAlive checks whether a process with the given PID exists by
// sending signal 0 (null signal).
func (d *Daemon) isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}
