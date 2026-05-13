//go:build windows

package mgmt

import (
	"os/exec"
	"strconv"
	"strings"
)

// killProcess forcefully terminates a process by PID using taskkill.
func (d *Daemon) killProcess(pid int) error {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// isProcessAlive checks whether a process with the given PID exists using
// tasklist.
func (d *Daemon) isProcessAlive(pid int) bool {
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), strconv.Itoa(pid))
}
