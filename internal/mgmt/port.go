package mgmt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type PortFile struct {
	PID  int    `json:"pid"`
	Port string `json:"port"`
	Name string `json:"name"`
}

const portFileName = ".gogent.pid"

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
// Legacy backward-compatible functions (CWD .gogent.pid)
// ---------------------------------------------------------------------------

// WritePortFile writes the port file. For backward compatibility it writes
// both the new-style app port file in ~/.gogent/ and the legacy CWD
// .gogent.pid.
func WritePortFile(port, name string) error {
	// New-style app port file in ~/.gogent/ (best-effort)
	WriteAppPortFile(name, port)

	// Legacy: CWD .gogent.pid
	f, err := os.Create(portFileName)
	if err != nil {
		return fmt.Errorf("write port file: %w", err)
	}
	defer f.Close()

	pf := PortFile{
		PID:  os.Getpid(),
		Port: port,
		Name: name,
	}
	return json.NewEncoder(f).Encode(&pf)
}

// ReadPortFile reads the port file. It first tries the daemon port file in
// ~/.gogent/, falling back to the legacy CWD .gogent.pid.
func ReadPortFile() (*PortFile, error) {
	// Try daemon port file first (new-style)
	if pf, err := ReadDaemonPortFile(); err == nil {
		return pf, nil
	}

	// Fall back to legacy CWD
	data, err := os.ReadFile(portFileName)
	if err != nil {
		return nil, fmt.Errorf("no agent running (no %s found)", portFileName)
	}

	var pf PortFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return nil, fmt.Errorf("corrupt port file: %w", err)
	}
	return &pf, nil
}

// PortFromFile returns the port string from the port file.
func PortFromFile() (string, error) {
	pf, err := ReadPortFile()
	if err != nil {
		return "", err
	}
	return pf.Port, nil
}

// RemovePortFile removes the port file. It removes both the legacy CWD
// .gogent.pid and — if the legacy file contains a valid name — the
// corresponding new-style app port file from ~/.gogent/.
func RemovePortFile() {
	// Attempt to determine app name from legacy file for cleanup
	name := "agent"
	legacyData, err := os.ReadFile(portFileName)
	if err == nil {
		var legacyPf PortFile
		if err := json.Unmarshal(legacyData, &legacyPf); err == nil && legacyPf.Name != "" {
			name = legacyPf.Name
		}
	}
	RemoveAppPortFile(name)

	// Always remove legacy CWD file
	os.Remove(portFileName)
}
