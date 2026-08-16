package tool

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Builtin name collision protection (v0.15.x fix)
//
// A process/http MCP server must not be able to shadow a builtin tool name
// (e.g. registering a server named "calculator" would silently change tool
// semantics). These tests lock the behavior across both registration paths.
// ---------------------------------------------------------------------------

func TestRegisterOrUpdateCannotOverrideBuiltin(t *testing.T) {
	reg := NewToolRegistry()
	reg.LoadDefault() // registers calculator/think/todo/... as builtin

	// tools.yaml path: a process entry named "calculator" must be rejected.
	err := reg.RegisterOrUpdate(&ToolDefinition{
		Name:     "calculator",
		Driver:   string(DriverProcess),
		Command:  "node calc-server.js",
		DefaultLvl: 0,
	})
	if err == nil {
		t.Fatal("RegisterOrUpdate(process calculator) = nil error, want rejection")
	}
	if !strings.Contains(err.Error(), "cannot override builtin tool") {
		t.Errorf("error = %q, want 'cannot override builtin tool'", err.Error())
	}

	// The builtin must be untouched.
	def, ok := reg.Get("calculator")
	if !ok || def.Driver != string(DriverBuiltin) {
		t.Fatalf("calculator after rejected override = %+v, want intact builtin", def)
	}
}

func TestRegisterOrUpdateCannotOverrideBuiltinHTTP(t *testing.T) {
	reg := NewToolRegistry()
	reg.LoadDefault()

	err := reg.RegisterOrUpdate(&ToolDefinition{
		Name:     "think",
		Driver:   string(DriverHTTP),
		Endpoint: "http://localhost:9999",
	})
	if err == nil {
		t.Fatal("RegisterOrUpdate(http think) = nil error, want rejection")
	}
}

func TestRegisterRejectsDuplicateBuiltinName(t *testing.T) {
	reg := NewToolRegistry()
	reg.LoadDefault()

	// CLI/gRPC path: Register with a name that already exists (builtin) fails.
	err := reg.Register(&ToolDefinition{
		Name:       "calculator",
		Driver:     string(DriverProcess),
		Command:    "node calc-server.js",
		DefaultLvl: 0,
	})
	if err == nil {
		t.Fatal("Register(process calculator) = nil error, want duplicate rejection")
	}
}

func TestRegisterOrUpdateAllowsSameDriverUpdate(t *testing.T) {
	reg := NewToolRegistry()
	reg.LoadDefault()

	// RegisterOrUpdate with a builtin driver should still be allowed
	// (used by McpRunner for child tools; builtins never update, but
	// same-driver updates are not the collision case).
	if err := reg.RegisterOrUpdate(&ToolDefinition{
		Name:        "calc-child",
		ServerName:  "calc",
		Driver:      string(DriverBuiltin),
		Description: "child",
	}); err != nil {
		t.Fatalf("RegisterOrUpdate(new tool) = %v, want success", err)
	}
	if !reg.Exists("calc-child") {
		t.Fatal("calc-child not registered")
	}
}

func TestLoadFromFileBuiltinConflictGuard(t *testing.T) {
	// LoadFromFile wraps RegisterOrUpdate (yaml.go:99), so the collision guard
	// exercised here is exactly the tools.yaml path. A process entry named
	// after a builtin tool must be rejected, not silently override it.
	reg := NewToolRegistry()
	reg.LoadDefault()

	err := reg.RegisterOrUpdate(&ToolDefinition{
		Name:       "calculator",
		Driver:     string(DriverProcess),
		Command:    "node calc.js",
		DefaultLvl: 0,
	})
	if err == nil || !strings.Contains(err.Error(), "cannot override builtin") {
		t.Fatalf("LoadFromFile conflict guard = %v, want 'cannot override builtin'", err)
	}
}
