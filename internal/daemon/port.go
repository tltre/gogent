package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PortFile represents the JSON content of a .port or .pid file.
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
	path := filepath.Join(dir, name+".port")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("app %s not running: %w", name, err)
	}
	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt app port file: %w", err)
	}
	return &pf, nil
}

// RemoveAppPortFile removes an app-level port file from ~/.gogent/.
func RemoveAppPortFile(name string) {
	dir := gogentDir()
	if dir == "" {
		return
	}
	os.Remove(filepath.Join(dir, name+".port"))
}

// ListAppPortFiles scans ~/.gogent/ for all port files and returns their names.
func ListAppPortFiles() ([]PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []PortFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".port") {
			if e.Name() != "daemon.pid" {
				continue
			}
		}
		name := strings.TrimSuffix(e.Name(), ".port")
		name = strings.TrimSuffix(name, ".pid")
		if name == "daemon" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pf PortFile
		if err := json.Unmarshal(data, &pf); err != nil {
			continue
		}
		result = append(result, pf)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Per-component port files (~/.gogent/<app_name>.<comp_name>.port)
// ---------------------------------------------------------------------------

// WriteComponentPortFile writes a component-level port file.
func WriteComponentPortFile(appName, compName, port string, pid int) error {
	dir := gogentDir()
	if dir == "" {
		return fmt.Errorf("write component port file: cannot determine home directory")
	}
	filename := appName + "." + compName + ".port"
	path := filepath.Join(dir, filename)
	pf := PortFile{
		PID:  pid,
		Port: port,
		Name: compName,
	}
	data, err := json.Marshal(&pf)
	if err != nil {
		return fmt.Errorf("marshal component port file: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// ReadComponentPortFile reads a specific component port file.
func ReadComponentPortFile(appName, compName string) (*PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("read component port file: cannot determine home directory")
	}
	filename := appName + "." + compName + ".port"
	path := filepath.Join(dir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read component port file %s: %w", filename, err)
	}
	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt component port file: %w", err)
	}
	return &pf, nil
}

// RemoveComponentPortFile removes a component-level port file.
func RemoveComponentPortFile(appName, compName string) {
	dir := gogentDir()
	if dir == "" {
		return
	}
	filename := appName + "." + compName + ".port"
	os.Remove(filepath.Join(dir, filename))
}

// ListComponentPortFiles scans ~/.gogent/ for component port files matching
// the given appName.
func ListComponentPortFiles(appName string) ([]PortFile, error) {
	dir := gogentDir()
	if dir == "" {
		return nil, fmt.Errorf("cannot determine home directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	prefix := appName + "."
	var result []PortFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".port") {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var pf PortFile
		if err := json.Unmarshal(data, &pf); err != nil {
			continue
		}
		result = append(result, pf)
	}
	return result, nil
}
