package cli_test

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/app"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/iface/cli"
)

func TestCLIRegister(t *testing.T) {
	c := cli.New("", "> ")

	cmd := &cobra.Command{Use: "test", RunE: func(_ *cobra.Command, _ []string) error { return nil }}
	c.Register("test", cmd)

	root := c.BuildRoot()
	found := false
	for _, child := range root.Commands() {
		if child.Use == "test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Register did not add command")
	}
}

func TestCLIRegisterOverrides(t *testing.T) {
	c := cli.New("", "> ")

	cmd1 := &cobra.Command{Use: "test"}
	c.Register("test", cmd1)

	cmd2 := &cobra.Command{Use: "test", Short: "replaced"}
	c.Register("test", cmd2)

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "test" && child.Short == "replaced" {
			return
		}
	}
	t.Error("Register did not override existing command")
}

func TestCLIUnregister(t *testing.T) {
	c := cli.New("", "> ")

	cmd := &cobra.Command{Use: "test"}
	c.Register("test", cmd)
	c.Unregister("test")

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "test" {
			t.Error("Unregister did not remove command")
		}
	}
}

func TestCLIUnregisterPrefixMatch(t *testing.T) {
	c := cli.New("", "> ")

	c.Register("config.validate", &cobra.Command{Use: "validate"})
	c.Register("config.show", &cobra.Command{Use: "show"})
	c.Register("tools", &cobra.Command{Use: "tools"})

	c.Unregister("config")

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "config" {
			t.Error("Unregister config should remove config children")
		}
	}
	hasTools := false
	for _, child := range root.Commands() {
		if child.Use == "tools" {
			hasTools = true
			break
		}
	}
	if !hasTools {
		t.Error("Unregister config should not remove tools")
	}
}

func TestCLIUnregisterSingleSubcommand(t *testing.T) {
	c := cli.New("", "> ")

	c.Register("config.validate", &cobra.Command{Use: "validate"})
	c.Register("config.show", &cobra.Command{Use: "show"})

	c.Unregister("config.validate")

	root := c.BuildRoot()
	var configCmd *cobra.Command
	for _, child := range root.Commands() {
		if child.Use == "config" {
			configCmd = child
			break
		}
	}
	if configCmd == nil {
		t.Fatal("config parent should still exist")
	}

	hasShow := false
	hasValidate := false
	for _, child := range configCmd.Commands() {
		if child.Use == "show" {
			hasShow = true
		}
		if child.Use == "validate" {
			hasValidate = true
		}
	}
	if !hasShow {
		t.Error("config.show should still exist")
	}
	if hasValidate {
		t.Error("config.validate should be removed")
	}
}

func TestCLIEnsureDefaults(t *testing.T) {
	c := cli.New("", "> ")

	c.EnsureDefaults()

	root := c.BuildRoot()
	names := make(map[string]bool)
	for _, child := range root.Commands() {
		names[child.Use] = true
	}
	for _, name := range []string{"chat", "run", "version"} {
		if !names[name] {
			t.Errorf("EnsureDefaults missing command: %s", name)
		}
	}
}

func TestCLIRegisterOverridesDefault(t *testing.T) {
	c := cli.New("", "> ")

	myChat := &cobra.Command{Use: "chat", Short: "custom-chat"}
	c.Register("chat", myChat)

	c.EnsureDefaults()

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "chat" && child.Short == "custom-chat" {
			return
		}
	}
	t.Error("EnsureDefaults overrode user-registered command")
}

func TestCLIUnregisterBlocksDefaults(t *testing.T) {
	c := cli.New("", "> ")

	c.Unregister("chat")
	c.EnsureDefaults()

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "chat" {
			t.Error("Unregister should block EnsureDefaults from re-adding chat")
		}
	}
}

func TestCLIBuildRootDefaultSubcommand(t *testing.T) {
	c := cli.New("", "> ")
	c.EnsureDefaults()

	root := c.BuildRoot()

	if root.RunE == nil {
		t.Error("root should have RunE set to chat when chat is registered")
	}
}

func TestCLIRunWithContext(t *testing.T) {
	c := cli.New("", "> ")

	reg := component.NewRegistry()
	reg.Register(agentcore.NewComponent("agent-test", &echoAgent{}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = c.Run(ctx, reg)
}

func TestCLICommandEntry(t *testing.T) {
	c := cli.New("", "> ")

	entries := []cli.CommandEntry{
		{
			Path: "mycmd",
			Build: func(cli *cli.DefaultCLI) *cobra.Command {
				return &cobra.Command{Use: "mycmd", Short: "custom"}
			},
		},
		{
			Path: "nested.sub",
			Build: func(cli *cli.DefaultCLI) *cobra.Command {
				return &cobra.Command{Use: "sub", Short: "nested"}
			},
		},
	}
	cli.RegisterByPath(c, entries)

	root := c.BuildRoot()
	hasMyCmd := false
	hasNested := false
	for _, child := range root.Commands() {
		if child.Use == "mycmd" && child.Short == "custom" {
			hasMyCmd = true
		}
		if child.Use == "nested" {
			for _, sub := range child.Commands() {
				if sub.Use == "sub" && sub.Short == "nested" {
					hasNested = true
				}
			}
		}
	}
	if !hasMyCmd {
		t.Error("RegisterByPath failed for flat command")
	}
	if !hasNested {
		t.Error("RegisterByPath failed for nested command")
	}
}

func TestCLICommandEntryNilBuild(t *testing.T) {
	c := cli.New("", "> ")

	c.Register("remove-me", &cobra.Command{Use: "remove-me"})

	entries := []cli.CommandEntry{
		{Path: "remove-me", Build: nil},
	}
	cli.RegisterByPath(c, entries)

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "remove-me" {
			t.Error("nil Build should unregister the command")
		}
	}
}

func TestCLIHierarchicalBuildRoot(t *testing.T) {
	c := cli.New("", "> ")

	c.Register("config", &cobra.Command{Use: "config", Short: "config-root"})
	c.Register("config.show", &cobra.Command{Use: "show", Short: "show-cfg"})
	c.Register("config.validate", &cobra.Command{Use: "validate", Short: "check-cfg"})
	c.Register("tools", &cobra.Command{Use: "tools", Short: "tool-list"})
	c.Register("tools.call", &cobra.Command{Use: "call", Short: "tool-call"})

	root := c.BuildRoot()

	var configCmd, toolsCmd *cobra.Command
	for _, child := range root.Commands() {
		if child.Use == "config" {
			configCmd = child
		}
		if child.Use == "tools" {
			toolsCmd = child
		}
	}
	if configCmd == nil {
		t.Fatal("config command not found")
	}
	if configCmd.Short != "config-root" {
		t.Errorf("config Short = %q, want %q", configCmd.Short, "config-root")
	}
	if toolsCmd == nil {
		t.Fatal("tools command not found")
	}

	configChildren := make(map[string]string)
	for _, child := range configCmd.Commands() {
		configChildren[child.Use] = child.Short
	}
	if configChildren["show"] != "show-cfg" {
		t.Error("config.show not found or wrong Short")
	}
	if configChildren["validate"] != "check-cfg" {
		t.Error("config.validate not found or wrong Short")
	}

	toolsChildren := make(map[string]string)
	for _, child := range toolsCmd.Commands() {
		toolsChildren[child.Use] = child.Short
	}
	if toolsChildren["call"] != "tool-call" {
		t.Error("tools.call not found or wrong Short")
	}
}

func TestCLIWithBuilder(t *testing.T) {
	builder := app.NewBuilderFromConfig(&app.Config{
		Name: "test-agent",
		Interface: app.InterfaceConfig{
			Type: "cli",
			CLI:  app.CLIConfig{Prompt: "> "},
		},
	})

	appInst, err := builder.Build(
		app.WithAgentCore("agent-test", &echoAgent{}),
		app.WithCLICommand(cli.CommandEntry{
			Path: "custom",
			Build: func(c *cli.DefaultCLI) *cobra.Command {
				return &cobra.Command{
					Use: "custom",
					RunE: func(cmd *cobra.Command, args []string) error {
						cmd.Println("custom-ok")
						return nil
					},
				}
			},
		}),
	)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if iface := appInst.Interface(); iface == nil {
		t.Fatal("Interface() returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := appInst.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := appInst.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { appInst.Stop(context.Background()) })
}

func TestCLIVersionCommand(t *testing.T) {
	c := cli.New("", "> ")
	c.EnsureDefaults()

	root := c.BuildRoot()
	var versionCmd *cobra.Command
	for _, child := range root.Commands() {
		if child.Use == "version" {
			versionCmd = child
			break
		}
	}
	if versionCmd == nil {
		t.Fatal("version command not found")
	}
	if versionCmd.Short == "" {
		t.Error("version command has no Short description")
	}
}

func TestCLIRunCommandFlag(t *testing.T) {
	c := cli.New("", "> ")
	c.EnsureDefaults()

	root := c.BuildRoot()
	var runCmd *cobra.Command
	for _, child := range root.Commands() {
		if child.Use == "run" {
			runCmd = child
			break
		}
	}
	if runCmd == nil {
		t.Fatal("run command not found")
	}

	promptFlag := runCmd.Flags().Lookup("prompt")
	if promptFlag == nil {
		t.Fatal("run command missing -p/--prompt flag")
	}
	if promptFlag.Shorthand != "p" {
		t.Errorf("prompt flag shorthand = %q, want %q", promptFlag.Shorthand, "p")
	}
}

func TestCLIConfigCommands(t *testing.T) {
	c := cli.New("", "> ")

	cli.RegisterByPath(c, cli.ConfigCommands())

	root := c.BuildRoot()
	var configCmd *cobra.Command
	for _, child := range root.Commands() {
		if child.Use == "config" {
			configCmd = child
			break
		}
	}
	if configCmd == nil {
		t.Fatal("config command not found")
	}

	hasValidate := false
	for _, child := range configCmd.Commands() {
		if child.Use == "validate" {
			hasValidate = true
			break
		}
	}
	if !hasValidate {
		t.Error("config.validate not found")
	}
}

func TestCLIPrefixMatchEdgeCases(t *testing.T) {
	c := cli.New("", "> ")

	c.Register("abc", &cobra.Command{Use: "abc"})
	c.Register("abcd", &cobra.Command{Use: "abcd"})
	c.Register("abc.def", &cobra.Command{Use: "def"})

	c.Unregister("abc")

	root := c.BuildRoot()
	for _, child := range root.Commands() {
		if child.Use == "abc" {
			t.Error("abc should be removed")
		}
	}
	hasAbcd := false
	for _, child := range root.Commands() {
		if child.Use == "abcd" {
			hasAbcd = true
		}
	}
	if !hasAbcd {
		t.Error("abcd should be kept (not a prefix match)")
	}
}

type echoAgent struct {
	runtime *agentcore.AgentRuntime
}

func (a *echoAgent) SetAgentRuntime(r *agentcore.AgentRuntime) { a.runtime = r }

func (a *echoAgent) Run(ctx context.Context, input agentcore.Input) (agentcore.Output, error) {
	return agentcore.Output{
		Response: agentcore.Message{Role: "assistant", Content: "echo-ok"},
	}, nil
}

func (a *echoAgent) Stream(ctx context.Context, input agentcore.Input) (<-chan agentcore.Event, error) {
	ch := make(chan agentcore.Event, 1)
	go func() {
		defer close(ch)
		out, _ := a.Run(ctx, input)
		ch <- agentcore.Event{Type: agentcore.EventAfterRun, Payload: out}
	}()
	return ch, nil
}
