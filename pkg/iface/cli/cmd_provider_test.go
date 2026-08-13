package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/provider"
)

// testProviderInfo returns provider info fixtures matching the engine
// capability declarations.
func testProviderInfo() []provider.ProviderInfo {
	return []provider.ProviderInfo{
		{
			Name:         "openai",
			DisplayName:  "OpenAI",
			DefaultModel: "gpt-4o",
			Models:       []string{"gpt-4o", "gpt-4o-mini", "o3-mini"},
		},
		{
			Name:         "deepseek",
			DisplayName:  "DeepSeek",
			DefaultModel: "deepseek-chat",
			Models:       []string{"deepseek-chat", "deepseek-reasoner"},
		},
	}
}

// ---------------------------------------------------------------------------
// parseProviderCommand
// ---------------------------------------------------------------------------

func TestParseProviderCommand(t *testing.T) {
	cases := []struct {
		line string
		kind string
		name string
		ok   bool
	}{
		{line: "/provider", kind: "list", ok: true},
		{line: "/provider openai", kind: "switch-provider", name: "openai", ok: true},
		{line: "/provider deepseek", kind: "switch-provider", name: "deepseek", ok: true},
		{line: "/model", kind: "show-model", ok: true},
		{line: "/model gpt-4o-mini", kind: "switch-model", name: "gpt-4o-mini", ok: true},
		{line: "hello", ok: false},
		{line: "/providerx", ok: false},
		{line: "/provider openai extra", kind: "list", ok: true}, // multi-word → list
	}
	for _, c := range cases {
		pc, ok := parseProviderCommand(c.line)
		if ok != c.ok {
			t.Errorf("parseProviderCommand(%q) ok = %v, want %v", c.line, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if pc.kind != c.kind {
			t.Errorf("parseProviderCommand(%q) kind = %q, want %q", c.line, pc.kind, c.kind)
		}
		if pc.name != c.name {
			t.Errorf("parseProviderCommand(%q) name = %q, want %q", c.line, pc.name, c.name)
		}
	}
}

// ---------------------------------------------------------------------------
// renderProviderList
// ---------------------------------------------------------------------------

func TestRenderProviderList(t *testing.T) {
	infos := testProviderInfo()
	out := renderProviderList(infos, "openai", "gpt-4o")

	for _, want := range []string{"openai", "deepseek", "gpt-4o", "deepseek-reasoner", "Current: openai (model: gpt-4o)"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderProviderList() missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "> 1.") {
		t.Errorf("renderProviderList() should mark current provider with >:\n%s", out)
	}
}

func TestRenderProviderListEmpty(t *testing.T) {
	if got := renderProviderList(nil, "", ""); !strings.Contains(got, "no providers") {
		t.Errorf("renderProviderList(empty) = %q, want no providers message", got)
	}
}

// ---------------------------------------------------------------------------
// resolveProvider / resolveModel
// ---------------------------------------------------------------------------

func newTestManager(t *testing.T, infos []provider.ProviderInfo) *provider.ProviderManager {
	t.Helper()
	mgr := provider.NewManagerComponent("provider-manager")
	for _, info := range infos {
		mi := provider.ModelInfo{
			Name:        info.DefaultModel,
			Provider:    info.Name,
			DisplayName: info.DisplayName,
			Models:      info.Models,
		}
		if err := mgr.Register(info.Name, &testProvider{info: mi}); err != nil {
			t.Fatalf("Register(%s) = %v", info.Name, err)
		}
	}
	return mgr
}

type testProvider struct {
	info provider.ModelInfo
}

func (p *testProvider) Generate(_ context.Context, _ []provider.ProviderMessage) (provider.Response, error) {
	return provider.Response{Content: "ok"}, nil
}

func (p *testProvider) Stream(_ context.Context, _ []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Delta: "ok", Done: true}
	close(ch)
	return ch, nil
}

func (p *testProvider) ModelInfo() provider.ModelInfo {
	return p.info
}

func TestResolveProvider(t *testing.T) {
	mgr := newTestManager(t, testProviderInfo())

	info, err := resolveProvider(mgr, "openai")
	if err != nil {
		t.Fatalf("resolveProvider(openai) = %v", err)
	}
	if info.Name != "openai" {
		t.Errorf("resolveProvider name = %q, want openai", info.Name)
	}

	if _, err := resolveProvider(mgr, "nope"); err == nil {
		t.Error("resolveProvider(nope) = nil error, want error")
	}
}

func TestResolveModel(t *testing.T) {
	infos := testProviderInfo()

	// Empty model → default.
	got, err := resolveModel(&infos[0], "")
	if err != nil || got != "gpt-4o" {
		t.Errorf("resolveModel(empty) = %q, %v; want gpt-4o, nil", got, err)
	}

	// Valid model.
	got, err = resolveModel(&infos[0], "gpt-4o-mini")
	if err != nil || got != "gpt-4o-mini" {
		t.Errorf("resolveModel(gpt-4o-mini) = %q, %v", got, err)
	}

	// Invalid model.
	if _, err := resolveModel(&infos[0], "claude"); err == nil {
		t.Error("resolveModel(claude) = nil error, want error")
	}
}

// ---------------------------------------------------------------------------
// Session state (handleProviderCommand)
// ---------------------------------------------------------------------------

func TestHandleProviderSwitch(t *testing.T) {
	mgr := newTestManager(t, testProviderInfo())
	currentProvider, currentModel := "openai", "gpt-4o"

	// Switch provider resets model to default.
	handled, cont := handleProviderCommand(mgr, "/provider deepseek", &currentProvider, &currentModel)
	if !handled || !cont {
		t.Fatalf("handleProviderCommand = %v, %v; want true, true", handled, cont)
	}
	if currentProvider != "deepseek" {
		t.Errorf("currentProvider = %q, want deepseek", currentProvider)
	}
	if currentModel != "deepseek-chat" {
		t.Errorf("currentModel = %q, want deepseek-chat (default)", currentModel)
	}

	// Switch model within provider.
	handled, _ = handleProviderCommand(mgr, "/model deepseek-reasoner", &currentProvider, &currentModel)
	if !handled {
		t.Fatal("handleProviderCommand(/model) not handled")
	}
	if currentModel != "deepseek-reasoner" {
		t.Errorf("currentModel = %q, want deepseek-reasoner", currentModel)
	}

	// Invalid model leaves state unchanged.
	handled, _ = handleProviderCommand(mgr, "/model claude", &currentProvider, &currentModel)
	if !handled {
		t.Fatal("handleProviderCommand(/model bad) not handled")
	}
	if currentModel != "deepseek-reasoner" {
		t.Errorf("currentModel changed to %q on invalid model", currentModel)
	}

	// Unknown provider leaves state unchanged.
	handled, _ = handleProviderCommand(mgr, "/provider nope", &currentProvider, &currentModel)
	if !handled {
		t.Fatal("handleProviderCommand(/provider bad) not handled")
	}
	if currentProvider != "deepseek" {
		t.Errorf("currentProvider changed to %q on invalid provider", currentProvider)
	}
}

func TestProviderManagerFromNil(t *testing.T) {
	cli := New("", "> ")
	// No provider component registered → nil manager.
	if mgr := providerManagerFrom(cli); mgr != nil {
		t.Fatalf("providerManagerFrom() = %v, want nil", mgr)
	}
}

func TestProviderManagerFromRegistered(t *testing.T) {
	mgr := provider.NewManagerComponent("provider-manager")
	reg := component.NewRegistry()
	if err := reg.Register(mgr); err != nil {
		t.Fatalf("Register() = %v", err)
	}
	reg.SetDefault(component.ComponentProvider, mgr.GetName())

	cli := New("", "> ")
	cli.reg = reg
	if got := providerManagerFrom(cli); got == nil {
		t.Fatal("providerManagerFrom() = nil, want manager")
	}
}
