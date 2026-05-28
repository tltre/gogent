//go:build windows

package mgmt

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// killProcess forcefully terminates a process by PID using taskkill.
func (d *Daemon) killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
}

// isProcessAlive checks whether a process with the given PID exists using
// tasklist.
func (d *Daemon) isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	cmd := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(pid), "/NH")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), strconv.Itoa(pid))
}

// detachDaemon configures the daemon process to run in a new process group
// so that Ctrl+C sent to the parent console does not propagate to it.
func detachDaemon(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP
}
