package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/iface/cli"
	"github.com/tltre/gogent/pkg/provider"
)

// flowMockProvider is a CredentialStoreAware mock that records the last
// invocation (ctx + messages) so tests can assert routing, model pass-through
// and credential injection end-to-end.
type flowMockProvider struct {
	name        string
	LastCtx     context.Context
	LastMessages []provider.ProviderMessage
	store       provider.CredentialStore
}

func (p *flowMockProvider) Generate(ctx context.Context, msgs []provider.ProviderMessage) (provider.Response, error) {
	p.LastCtx = ctx
	p.LastMessages = msgs
	return provider.Response{Content: "flow-reply-" + p.name, FinishReason: "stop"}, nil
}

func (p *flowMockProvider) Stream(ctx context.Context, msgs []provider.ProviderMessage) (<-chan provider.StreamChunk, error) {
	p.LastCtx = ctx
	p.LastMessages = msgs
	ch := make(chan provider.StreamChunk, 1)
	ch <- provider.StreamChunk{Delta: "flow-reply-" + p.name, Done: true}
	close(ch)
	return ch, nil
}

func (p *flowMockProvider) ModelInfo() provider.ModelInfo {
	return provider.ModelInfo{
		Name:        "m-" + p.name,
		Provider:    p.name,
		DisplayName: p.name,
		Models:      []string{"m-" + p.name},
	}
}

func (p *flowMockProvider) SetCredentialStore(s provider.CredentialStore) {
	p.store = s
}

// registerFlowMocks registers two mock engines and returns their instances.
func registerFlowMocks(t *testing.T) (*flowMockProvider, *flowMockProvider) {
	t.Helper()
	a, b := &flowMockProvider{name: "flow-a"}, &flowMockProvider{name: "flow-b"}
	if err := provider.RegisterEngine("flow-a", func() (provider.IProvider, error) { return a, nil }); err != nil {
		t.Fatalf("RegisterEngine(flow-a) = %v", err)
	}
	if err := provider.RegisterEngine("flow-b", func() (provider.IProvider, error) { return b, nil }); err != nil {
		t.Fatalf("RegisterEngine(flow-b) = %v", err)
	}
	t.Cleanup(func() {
		provider.UnregisterEngine("flow-a")
		provider.UnregisterEngine("flow-b")
	})
	return a, b
}

// buildFlowApp assembles a full app from a YAML string and returns it plus
// its AgentRuntime.
func buildFlowApp(t *testing.T, yaml string) (*App, *agentcore.AgentRuntime) {
	t.Helper()
	cfg, err := ParseConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	b := NewBuilderFromConfig(cfg)
	app, err := b.Build()
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	ctx := context.Background()
	if err := app.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}
	comp := app.GetDefault(component.ComponentAgentCore)
	runtime, ok := comp.(*agentcore.AgentRuntime)
	if !ok {
		t.Fatalf("agentcore type = %T, want *AgentRuntime", comp)
	}
	return app, runtime
}

// ---------------------------------------------------------------------------
// 1.1 Default-open registration (no exclude)
// ---------------------------------------------------------------------------

func TestFlowDefaultOpenRegistration(t *testing.T) {
	a, b := registerFlowMocks(t)
	_ = a
	_ = b

	app, _ := buildFlowApp(t, `
name: flow-open
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)

	mgr := app.GetDefault(component.ComponentProvider).(*provider.ProviderManager)
	infos := mgr.List()
	names := map[string]bool{}
	for _, i := range infos {
		names[i.Name] = true
	}
	// All registered engines present with no exclude.
	if !names["flow-a"] || !names["flow-b"] || !names["openai"] || !names["deepseek"] {
		t.Fatalf("List() = %v, want flow-a/flow-b/openai/deepseek all present", names)
	}
}

// ---------------------------------------------------------------------------
// 1.4 End-to-end run routing through Builder→App→ReactAgent
// ---------------------------------------------------------------------------

func TestFlowRunRoutesToSelectedProvider(t *testing.T) {
	a, b := registerFlowMocks(t)

	app, runtime := buildFlowApp(t, `
name: flow-route
provider:
  exclude: ["openai", "deepseek"]
components:   # keep only mock engines
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)
	_ = app

	out, err := runtime.Run(context.Background(), agentcore.Input{
		Messages:     []agentcore.Message{{Role: "user", Content: "hi"}},
		ProviderName: "flow-b",
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if out.Response.Content != "flow-reply-flow-b" {
		t.Errorf("Response.Content = %q, want flow-reply-flow-b", out.Response.Content)
	}
	// Routing: flow-a untouched, flow-b invoked (its message list includes the
	// default system prompt added by BuildInput, so length is >= 1).
	if len(a.LastMessages) != 0 || len(b.LastMessages) == 0 {
		t.Fatalf("routing wrong: flow-a msgs=%d flow-b msgs=%d", len(a.LastMessages), len(b.LastMessages))
	}
}

func TestFlowRunDefaultProvider(t *testing.T) {
	_, b := registerFlowMocks(t)

	_, runtime := buildFlowApp(t, `
name: flow-default
provider:
  exclude: ["openai", "deepseek"]
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)

	// Empty provider → first registered alphabetically (flow-a).
	out, err := runtime.Run(context.Background(), agentcore.Input{
		Messages: []agentcore.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Run() = %v", err)
	}
	if !strings.HasPrefix(out.Response.Content, "flow-reply-flow-a") {
		t.Errorf("Response.Content = %q, want flow-reply-flow-a prefix", out.Response.Content)
	}
	_ = b
}

// ---------------------------------------------------------------------------
// 1.3 Credential injection end-to-end
// ---------------------------------------------------------------------------

func TestFlowCredentialStoreReachesEngine(t *testing.T) {
	a, _ := registerFlowMocks(t)

	credPath := filepath.Join(t.TempDir(), "credentials.yaml")
	store := provider.NewFileCredentialStore(credPath)
	if err := store.Set("flow-a", "sk-flow-a"); err != nil {
		t.Fatalf("store.Set = %v", err)
	}

	cfg, err := ParseConfig([]byte(`
name: flow-cred
provider:
  exclude: ["openai", "deepseek"]
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`))
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	b := NewBuilderFromConfig(cfg)
	app, err := b.Build(WithCredentialStore(store))
	if err != nil {
		t.Fatalf("Build() = %v", err)
	}
	ctx := context.Background()
	if err := app.Initialize(ctx); err != nil {
		t.Fatalf("Initialize() = %v", err)
	}

	if a.store == nil {
		t.Fatal("engine did not receive CredentialStore")
	}
	if v, _ := a.store.Get("flow-a"); v != "sk-flow-a" {
		t.Errorf("engine store Get(flow-a) = %q, want sk-flow-a", v)
	}
}

// ---------------------------------------------------------------------------
// 1.6 CLI run command with --provider/--model
// ---------------------------------------------------------------------------

func TestFlowCLIRunCommand(t *testing.T) {
	_, b := registerFlowMocks(t)

	app, _ := buildFlowApp(t, `
name: flow-cli
provider:
  exclude: ["openai", "deepseek"]
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)

	// Drive the CLI root command with `run -p ... --provider flow-b --model m-flow-b`.
	c := cli.New("", "> ")
	app.SetInterface(c)
	iface := app.Interface().(*cli.DefaultCLI)
	if err := iface.Run(context.Background(), app.Registry()); err != nil {
		t.Fatalf("iface.Run() = %v", err)
	}
	_ = c // root executes with empty args → chat; skip, use direct command below

	// Build root and run the `run` subcommand directly.
	root := iface.BuildRoot()
	root.SetArgs([]string{"run", "-p", "hello", "--provider", "flow-b", "--model", "m-flow-b"})
	root.SetOut(io.Discard)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("run command = %v", err)
	}
	if len(b.LastMessages) == 0 {
		t.Fatalf("flow-b not invoked, msgs=%d", len(b.LastMessages))
	}
	if got := provider.ModelFrom(b.LastCtx); got != "m-flow-b" {
		t.Errorf("model via ctx = %q, want m-flow-b", got)
	}
}

// ---------------------------------------------------------------------------
// 1.7 CLI chat REPL integration (stdin-driven)
// ---------------------------------------------------------------------------

func TestFlowChatREPLSwitchesProvider(t *testing.T) {
	_, b := registerFlowMocks(t)

	app, _ := buildFlowApp(t, `
name: flow-chat
provider:
  exclude: ["openai", "deepseek"]
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)

	// Simulate stdin: switch to flow-b, send a message, quit.
	// (stdout is not captured — the REPL writes directly to os.Stdout;
	// core assertions are on routing + ctx, not rendering.)
	input := "/provider flow-b\nhello\nquit\n"
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	_, _ = w.WriteString(input)
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	iface := cli.New("", "> ")
	app.SetInterface(iface)
	if err := iface.Run(context.Background(), app.Registry()); err != nil {
		t.Fatalf("iface.Run() = %v", err)
	}

	if len(b.LastMessages) == 0 {
		t.Fatalf("flow-b not invoked via chat, msgs=%d", len(b.LastMessages))
	}
	if got := provider.ProviderNameFrom(b.LastCtx); got != "flow-b" {
		t.Errorf("provider via ctx = %q, want flow-b", got)
	}
	// Last message is the user's "hello" (earlier entries: system prompt
	// added by BuildInput).
	last := b.LastMessages[len(b.LastMessages)-1]
	if last.Content != "hello" {
		t.Errorf("last message content = %q, want hello", last.Content)
	}
}

// ---------------------------------------------------------------------------
// v0.15.1: Builder injects the tool-calling surface into AgentRuntime
// ---------------------------------------------------------------------------

func TestFlowToolServiceInjected(t *testing.T) {
	registerFlowMocks(t)

	app, runtime := buildFlowApp(t, `
name: flow-tools
provider:
  exclude: ["openai", "deepseek"]
components:
  - name: "agent-main"
    type: "agentcore"
    driver: "native"
    config:
      type: "react"
`)
	_ = app

	// After a full Build, the AgentRuntime must expose a tool.Service.
	if rt := runtime.ToolService(); rt == nil {
		t.Fatal("AgentRuntime.ToolService() = nil after Build, want injected ToolManager")
	}
}
