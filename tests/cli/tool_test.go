package cli_test

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon"
	"github.com/tltre/gogent/internal/daemon/tool"
)

func TestToolCLI(t *testing.T) {
	port := "9597"

	cmd := exec.Command(gogentExe, "daemon", "--port", port, "--foreground")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	client := daemon.NewDaemonClient(":" + port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := client.Info(); err == nil { break }
		time.Sleep(300 * time.Millisecond)
	}

	t.Run("list-builtin", func(t *testing.T) {
		tools, err := client.ToolsList()
		if err != nil { t.Fatalf("ToolsList: %v", err) }
		if len(tools) == 0 { t.Fatal("expected tools, got empty") }
		found := false
		for _, tl := range tools { if tl.Name == "calculator" { found = true; break } }
		if !found { t.Error("calculator not found") }
	})

	t.Run("register-and-list", func(t *testing.T) {
		err := client.ToolRegister(&api.RegisterToolRequest{Name: "test-reg", Driver: "builtin", DefaultLevel: 1})
		if err != nil { t.Fatalf("ToolRegister: %v", err) }
		tools, _ := client.ToolsList()
		found := false
		for _, tl := range tools { if tl.Name == "test-reg" { found = true; break } }
		if !found { t.Error("not found after register") }
	})

	t.Run("register-duplicate", func(t *testing.T) {
		err := client.ToolRegister(&api.RegisterToolRequest{Name: "test-reg", Driver: "builtin"})
		if err == nil { t.Error("expected error for duplicate") }
	})

	t.Run("status", func(t *testing.T) {
		info, err := client.ToolsStatus("calculator")
		if err != nil { t.Fatalf("ToolsStatus: %v", err) }
		if info["name"] != "calculator" { t.Errorf("name=%v", info["name"]) }
	})

	t.Run("status-not-found", func(t *testing.T) {
		_, err := client.ToolsStatus("nonexistent")
		if err == nil { t.Error("expected error") }
	})

	t.Run("unregister-force", func(t *testing.T) {
		if err := client.ToolUnregister("test-reg", true); err != nil { t.Fatalf("Unregister: %v", err) }
		tools, _ := client.ToolsList()
		for _, tl := range tools { if tl.Name == "test-reg" { t.Error("found after unregister") } }
	})

	t.Run("unregister-nonexistent", func(t *testing.T) {
		err := client.ToolUnregister("nonexistent", false)
		if err == nil { t.Error("expected error") }
	})

	t.Run("cli-register-flags", func(t *testing.T) {
		out, err := exec.Command(gogentExe, "--port", port, "tool", "register", "cli-flag-tool", "--driver", "builtin", "--level", "2").CombinedOutput()
		if err != nil { t.Fatalf("register: %v\n%s", err, out) }
		if !strings.Contains(string(out), "registered") { t.Errorf("no confirmation, got: %s", out) }
		client.ToolUnregister("cli-flag-tool", true)
	})

	t.Run("cli-unregister-force", func(t *testing.T) {
		client.ToolRegister(&api.RegisterToolRequest{Name: "cli-unreg", Driver: "builtin"})
		out, err := exec.Command(gogentExe, "--port", port, "tool", "unregister", "cli-unreg", "--force").CombinedOutput()
		if err != nil { t.Fatalf("unregister: %v\n%s", err, out) }
		if !strings.Contains(string(out), "unregistered") { t.Errorf("no confirmation, got: %s", out) }
	})
}

func TestToolRegistrySource(t *testing.T) {
	reg := tool.NewToolRegistry()
	reg.LoadDefault()
	if src := reg.GetSource("calculator"); src != tool.SourceBuiltin {
		t.Errorf("calculator source=%q, want %q", src, tool.SourceBuiltin)
	}
	reg.Register(&tool.ToolDefinition{Name: "usr", Driver: "builtin", DefaultLvl: 0})
	if src := reg.GetSource("usr"); src != tool.SourceUser {
		t.Errorf("usr source=%q, want %q", src, tool.SourceUser)
	}
}
