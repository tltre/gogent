package mgmt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type PortFile struct {
	PID  int    `json:"pid"`
	Port string `json:"port"`
	Name string `json:"name"`
}

// gogentDir returns the path to ~/.gogent/, creating the directory if it
// does not exist (0700, private to user). Returns empty string on failure.
func gogentDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".gogent")
	os.MkdirAll(dir, 0700)
	return dir
}

// ---------------------------------------------------------------------------
// Daemon port file (~/.gogent/daemon.pid)
// ---------------------------------------------------------------------------

// WriteDaemonPortFile writes the daemon port file to ~/.gogent/daemon.pid.
func WriteDaemonPortFile(port string) error {
	dir := gogentDir()
	if dir == "" {
		return fmt.Errorf("write daemon port file: cannot determine home directory")
	}
	path := filepath.Join(dir, "daemon.pid")
	pf := PortFile{
		PID:  os.Getpid(),
		Port: port,
		Name: "daemon",
	}
	data, err := json.Marshal(&pf)
	if err != nil {
		return fmt.Errorf("marshal daemon port file: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ReadDaemonPortFile reads the daemon port file from ~/.gogent/daemon.pid.
func ReadDaemonPortFile() (*PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("read daemon port file: cannot determine home directory")
	}
	path := filepath.Join(dir, "daemon.pid")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no daemon running (no daemon.pid found)")
	}
	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt daemon port file: %w", err)
	}
	return &pf, nil
}

// RemoveDaemonPortFile removes the daemon port file from ~/.gogent/.
func RemoveDaemonPortFile() {
	dir := gogentDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, "daemon.pid"))
}

// ---------------------------------------------------------------------------
// Per-app port files (~/.gogent/<name>.port)
// ---------------------------------------------------------------------------

// WriteAppPortFile writes an app-level port file to ~/.gogent/<name>.port.
func WriteAppPortFile(name, port string) error {
	dir := gogentDir()
	if dir == "" {
		return fmt.Errorf("write app port file: cannot determine home directory")
	}
	filename := name + ".port"
	path := filepath.Join(dir, filename)
	pf := PortFile{
		PID:  os.Getpid(),
		Port: port,
		Name: name,
	}
	data, err := json.Marshal(&pf)
	if err != nil {
		return fmt.Errorf("marshal app port file: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ReadAppPortFile reads an app-level port file from ~/.gogent/<name>.port.
func ReadAppPortFile(name string) (*PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("read app port file: cannot determine home directory")
	}
	filename := name + ".port"
	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no app port file for %q (no %s found)", name, filename)
	}
	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt app port file: %w", err)
	}
	return &pf, nil
}

// ListAppPortFiles scans ~/.gogent/ for all *.port files (excluding
// daemon.pid) and returns their parsed PortFile contents.
func ListAppPortFiles() ([]PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("list app port files: cannot determine home directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list app port files: %w", err)
	}
	var result []PortFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".port" {
			continue
		}
		// Skip daemon.pid (named with .pid but safety check)
		if name == "daemon.pid" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue // skip unreadable
		}
		var pf PortFile
		if err := json.Unmarshal(data, &pf); err != nil {
			continue // skip corrupt
		}
		result = append(result, pf)
	}
	return result, nil
}

// RemoveAppPortFile removes <name>.port from ~/.gogent/.
func RemoveAppPortFile(name string) {
	dir := gogentDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, name+".port"))
}

// ---------------------------------------------------------------------------
// Per-component port files (~/.gogent/<appName>.<compName>.port)
// ---------------------------------------------------------------------------

// WriteComponentPortFile writes a component-level port file to
// ~/.gogent/<appName>.<compName>.port.
func WriteComponentPortFile(appName, compName, port string) error {
	dir := gogentDir()
	if dir == "" {
		return fmt.Errorf("write component port file: cannot determine home directory")
	}
	filename := appName + "." + compName + ".port"
	path := filepath.Join(dir, filename)
	pf := PortFile{
		PID:  os.Getpid(),
		Port: port,
		Name: compName,
	}
	data, err := json.Marshal(&pf)
	if err != nil {
		return fmt.Errorf("marshal component port file: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ReadComponentPortFile reads a component-level port file from
// ~/.gogent/<appName>.<compName>.port.
func ReadComponentPortFile(appName, compName string) (*PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("read component port file: cannot determine home directory")
	}
	filename := appName + "." + compName + ".port"
	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("no component port file for %s/%s (no %s found)", appName, compName, filename)
	}
	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt component port file: %w", err)
	}
	return &pf, nil
}

// RemoveComponentPortFile removes <appName>.<compName>.port from ~/.gogent/.
func RemoveComponentPortFile(appName, compName string) {
	dir := gogentDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, appName+"."+compName+".port"))
}

// ListComponentPortFiles scans ~/.gogent/ for all component-level port files
// (files matching *.*.port — two dots). App-level *.port (one dot) and
// daemon.pid are excluded.
func ListComponentPortFiles() ([]PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("list component port files: cannot determine home directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list component port files: %w", err)
	}
	var result []PortFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".port") {
			continue
		}
		// Component-level files have two dots: <app>.<comp>.port
		// App-level files have one dot:  <app>.port — skip those.
		if strings.Count(name, ".") < 2 {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue // skip unreadable
		}
		var pf PortFile
		if err := json.Unmarshal(data, &pf); err != nil {
			continue // skip corrupt
		}
		result = append(result, pf)
	}
	return result, nil
}


