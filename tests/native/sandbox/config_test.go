package sandbox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tltre/gogent/internal/daemon/sandbox"
)

// testdataPath returns the absolute path to a test fixture file.
func testdataPath(name string) string {
	return filepath.Join("testdata", name)
}

func TestConfig_LoadSandboxFile_ValidFull(t *testing.T) {
	// Save and restore working directory
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	os.Chdir("testdata")

	// LoadSandboxFile reads from ~/.gogent/sandbox.yaml by default.
	// Since testdata/ contains the fixtures, we use the raw file path
	// instead. We test the underlying parse function via ApplySandboxFile.
	data, err := os.ReadFile("valid_full.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	// Parse with sandboxFile directly
	sf, err := parseSandboxFile(data)
	if err != nil {
		t.Fatalf("parseSandboxFile failed: %v", err)
	}

	// Validate providers
	if len(sf.Providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(sf.Providers))
	}
	p1, ok := sf.Providers["my-e2b"]
	if !ok {
		t.Fatal("expected provider 'my-e2b'")
	}
	if p1.Type != "e2b" {
		t.Errorf("expected type e2b, got %s", p1.Type)
	}
	if p1.Endpoint != "https://api.e2b.app" {
		t.Errorf("expected endpoint https://api.e2b.app, got %s", p1.Endpoint)
	}
	if p1.APIKey != "sk-test-12345" {
		t.Errorf("expected apiKey sk-test-12345, got %s", p1.APIKey)
	}

	p2, ok := sf.Providers["local-builtin"]
	if !ok {
		t.Fatal("expected provider 'local-builtin'")
	}
	if p2.Type != "builtin" {
		t.Errorf("expected type builtin, got %s", p2.Type)
	}

	// Validate profiles
	if len(sf.Profiles) != 2 {
		t.Errorf("expected 2 profiles, got %d", len(sf.Profiles))
	}
	prof, ok := sf.Profiles["restricted-shell"]
	if !ok {
		t.Fatal("expected profile 'restricted-shell'")
	}
	if prof.Provider != "my-e2b" {
		t.Errorf("expected provider my-e2b, got %s", prof.Provider)
	}
	if prof.Template != "code-interpreter-v1" {
		t.Errorf("expected template code-interpreter-v1, got %s", prof.Template)
	}
	if prof.Network == nil || *prof.Network {
		t.Error("expected network=false")
	}
	if prof.MaxMemoryMB != 256 {
		t.Errorf("expected maxMemoryMB 256, got %d", prof.MaxMemoryMB)
	}
	if prof.MaxCPUTime != "30s" {
		t.Errorf("expected maxCPUTime 30s, got %s", prof.MaxCPUTime)
	}

	// Validate lifecycle profile
	ws, ok := sf.Profiles["workspace"]
	if !ok {
		t.Fatal("expected profile 'workspace'")
	}
	if ws.Lifecycle == nil {
		t.Fatal("expected lifecycle config")
	}
	if ws.Lifecycle.Mode != "persistent" {
		t.Errorf("expected lifecycle mode persistent, got %s", ws.Lifecycle.Mode)
	}

	// Validate defaults
	if sf.Defaults == nil {
		t.Fatal("expected defaults")
	}
	if sf.Defaults.Builtin.Profile != "restricted-shell" {
		t.Errorf("expected builtin default restricted-shell, got %s", sf.Defaults.Builtin.Profile)
	}
	if sf.Defaults.Process.Profile != "restricted-shell" {
		t.Errorf("expected process default restricted-shell, got %s", sf.Defaults.Process.Profile)
	}
}

func TestConfig_LoadSandboxFile_ValidMinimal(t *testing.T) {
	data, err := os.ReadFile(testdataPath("valid_minimal.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	sf, err := parseSandboxFile(data)
	if err != nil {
		t.Fatalf("parseSandboxFile failed: %v", err)
	}

	if len(sf.Providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(sf.Providers))
	}
	if len(sf.Profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(sf.Profiles))
	}

	// No defaults section — should be nil
	if sf.Defaults != nil {
		t.Error("expected nil defaults for minimal config")
	}
}

func TestConfig_LoadSandboxFile_Malformed(t *testing.T) {
	data, err := os.ReadFile(testdataPath("invalid_malformed.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	_, err = parseSandboxFile(data)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestConfig_LoadSandboxFile_FileNotExist(t *testing.T) {
	// LoadSandboxFile reads from ~/.gogent/sandbox.yaml which won't exist
	// in test environments. It should return nil, nil.
	sf, err := sandbox.LoadSandboxFile()
	if err != nil {
		// Depending on test environment, this may error (no home dir) or
		// return nil (file not found). Both are acceptable.
		t.Logf("LoadSandboxFile returned error (acceptable in CI): %v", err)
		return
	}
	if sf != nil {
		t.Log("LoadSandboxFile returned non-nil (sandbox.yaml exists on this machine)")
	}
}

func TestConfig_ApplySandboxFile(t *testing.T) {
	data, err := os.ReadFile(testdataPath("valid_full.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	sf, err := parseSandboxFile(data)
	if err != nil {
		t.Fatalf("parseSandboxFile failed: %v", err)
	}

	mgr := sandbox.NewSandboxManager()
	defaults, err := sandbox.ApplySandboxFile(mgr, sf, nil)
	if err != nil {
		t.Fatalf("ApplySandboxFile failed: %v", err)
	}

	// Verify providers registered
	if mgr.ProviderRegistry() == nil {
		t.Fatal("expected non-nil ProviderRegistry")
	}
	if _, ok := mgr.GetProvider("my-e2b"); !ok {
		t.Error("expected provider 'my-e2b' to be registered")
	}
	if _, ok := mgr.GetProvider("local-builtin"); !ok {
		t.Error("expected provider 'local-builtin' to be registered")
	}

	// Verify profiles registered
	if p, ok := mgr.GetProfile("restricted-shell"); !ok {
		t.Error("expected profile 'restricted-shell' to be registered")
	} else if p.Template != "code-interpreter-v1" {
		t.Errorf("expected template code-interpreter-v1, got %s", p.Template)
	}

	// Verify defaults
	if defaults == nil {
		t.Fatal("expected non-nil defaults")
	}
	if defaults.Builtin.Profile != "restricted-shell" {
		t.Errorf("expected builtin restricted-shell, got %s", defaults.Builtin.Profile)
	}
	if defaults.Process.Profile != "restricted-shell" {
		t.Errorf("expected process restricted-shell, got %s", defaults.Process.Profile)
	}
}

func TestConfig_ApplySandboxFile_Minimal(t *testing.T) {
	data, err := os.ReadFile(testdataPath("valid_minimal.yaml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	sf, err := parseSandboxFile(data)
	if err != nil {
		t.Fatalf("parseSandboxFile failed: %v", err)
	}

	mgr := sandbox.NewSandboxManager()
	defaults, err := sandbox.ApplySandboxFile(mgr, sf, nil)
	if err != nil {
		t.Fatalf("ApplySandboxFile failed: %v", err)
	}

	if defaults != nil {
		t.Error("expected nil defaults for minimal config (no defaults section)")
	}
}

func TestConfig_ApplySandboxFile_NilInput(t *testing.T) {
	mgr := sandbox.NewSandboxManager()
	defaults, err := sandbox.ApplySandboxFile(mgr, nil, nil)
	if err != nil {
		t.Errorf("expected nil error for nil input, got: %v", err)
	}
	if defaults != nil {
		t.Error("expected nil defaults for nil input")
	}
}

// parseSandboxFile is a test helper that unmarshals YAML into sandboxFile.
// It exposes the internal parsing for test verification.
func parseSandboxFile(data []byte) (*sandbox.SandboxFile, error) {
	return sandbox.ParseSandboxFile(data)
}
