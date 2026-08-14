package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tltre/gogent/pkg/provider"
)

// ---------------------------------------------------------------------------
// parseKeyCommand
// ---------------------------------------------------------------------------

func TestParseKeyCommand(t *testing.T) {
	cases := []struct {
		line   string
		kind   string
		name   string
		apiKey string
		ok     bool
	}{
		{line: "/key", kind: "list", ok: true},
		{line: "/key openai", kind: "get", name: "openai", ok: true},
		{line: "/key openai sk-abc123", kind: "set", name: "openai", apiKey: "sk-abc123", ok: true},
		{line: "/key openai --delete", kind: "delete", name: "openai", ok: true},
		{line: "hello", ok: false},
		{line: "/keyboard", ok: false},
	}
	for _, c := range cases {
		kc, ok := parseKeyCommand(c.line)
		if ok != c.ok {
			t.Errorf("parseKeyCommand(%q) ok = %v, want %v", c.line, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if kc.kind != c.kind {
			t.Errorf("parseKeyCommand(%q) kind = %q, want %q", c.line, kc.kind, c.kind)
		}
		if kc.name != c.name {
			t.Errorf("parseKeyCommand(%q) name = %q, want %q", c.line, kc.name, c.name)
		}
		if kc.apiKey != c.apiKey {
			t.Errorf("parseKeyCommand(%q) apiKey = %q, want %q", c.line, kc.apiKey, c.apiKey)
		}
	}
}

// ---------------------------------------------------------------------------
// maskKey
// ---------------------------------------------------------------------------

func TestMaskKey(t *testing.T) {
	if got := maskKey(""); got != "(not configured)" {
		t.Errorf("maskKey(empty) = %q", got)
	}
	if got := maskKey("sk-abc"); !strings.Contains(got, "****") {
		t.Errorf("maskKey(short) = %q, want masked", got)
	}
	if got := maskKey("sk-abcdefghijkl"); !strings.HasSuffix(got, "ijkl") {
		t.Errorf("maskKey(long) = %q, want suffix ijkl", got)
	}
	if strings.Contains(maskKey("sk-abcdefghijkl"), "abcdef") {
		t.Error("maskKey leaks middle of the key")
	}
}

// ---------------------------------------------------------------------------
// handleKeyCommand end-to-end against a FileCredentialStore
// ---------------------------------------------------------------------------

func TestHandleKeySetGetDelete(t *testing.T) {
	store := provider.NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.yaml"))
	mgr := newTestManager(t, testProviderInfo())

	// Set.
	handled, cont := handleKeyCommand(mgr, store, "/key openai sk-secret123")
	if !handled || !cont {
		t.Fatalf("set handled=%v cont=%v", handled, cont)
	}
	got, err := store.Get("openai")
	if err != nil || got != "sk-secret123" {
		t.Fatalf("store.Get(openai) = %q, %v; want sk-secret123", got, err)
	}

	// Get (no panic; masked output goes to stdout).
	handled, _ = handleKeyCommand(mgr, store, "/key openai")
	if !handled {
		t.Fatal("get not handled")
	}

	// Unknown provider on set is rejected.
	handled, _ = handleKeyCommand(mgr, store, "/key nope sk-x")
	if !handled {
		t.Fatal("set unknown not handled")
	}
	if v, _ := store.Get("nope"); v != "" {
		t.Errorf("store.Get(nope) = %q, want empty (rejected)", v)
	}

	// Delete.
	handled, _ = handleKeyCommand(mgr, store, "/key openai --delete")
	if !handled {
		t.Fatal("delete not handled")
	}
	if v, _ := store.Get("openai"); v != "" {
		t.Errorf("store.Get(openai) after delete = %q, want empty", v)
	}
}

func TestHandleKeyCommandNoStore(t *testing.T) {
	// nil store → handled, graceful message, no panic.
	handled, cont := handleKeyCommand(nil, nil, "/key")
	if !handled || !cont {
		t.Fatalf("handled=%v cont=%v", handled, cont)
	}
	handled, _ = handleKeyCommand(nil, nil, "/key openai sk-x")
	if !handled {
		t.Fatal("set without store not handled")
	}
}

func TestRenderKeyList(t *testing.T) {
	store := provider.NewFileCredentialStore(filepath.Join(t.TempDir(), "credentials.yaml"))
	if err := store.Set("openai", "sk-abcdef1234"); err != nil {
		t.Fatalf("store.Set = %v", err)
	}
	mgr := newTestManager(t, testProviderInfo())

	out := renderKeyList(mgr, store)
	if !strings.Contains(out, "openai") || !strings.Contains(out, "****") {
		t.Errorf("renderKeyList missing provider or masked key:\n%s", out)
	}
	if strings.Contains(out, "sk-abcdef1234") {
		t.Error("renderKeyList leaks the full key")
	}
}
