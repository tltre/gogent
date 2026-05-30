package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon"
	"gopkg.in/yaml.v3"
)

var (
	regDriver      string
	regCommand     string
	regEndpoint    string
	regLevel       int
	regDescription string
	regEnv         []string
)

var toolRegisterCmd = &cobra.Command{
	Use:   "register [name]",
	Short: "Register a tool (opens editor if no args, CLI flags otherwise)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Editor mode: only when completely bare (no args, no flags)
		if len(args) == 0 && cmd.Flags().NFlag() == 0 {
			return registerViaEditor(cmd)
		}

		// CLI mode
		if len(args) == 0 {
			return fmt.Errorf("tool name is required when using flags")
		}
		env := parseKeyValuePairs(regEnv)
		req := &api.RegisterToolRequest{
			Name:         args[0],
			Driver:       regDriver,
			Command:      regCommand,
			Endpoint:     regEndpoint,
			DefaultLevel: regLevel,
			Description:  regDescription,
			Env:          env,
		}
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.ToolRegister(req); err != nil {
			return fmt.Errorf("register tool: %w", err)
		}
		fmt.Printf("tool %q registered (driver=%s, level=%d)\n", args[0], regDriver, regLevel)
		return nil
	},
}

func init() {
	toolRegisterCmd.Flags().StringVar(&regDriver, "driver", "builtin", "driver type (builtin/process/http)")
	toolRegisterCmd.Flags().StringVar(&regCommand, "command", "", "command for process driver")
	toolRegisterCmd.Flags().StringVar(&regEndpoint, "endpoint", "", "endpoint for http driver")
	toolRegisterCmd.Flags().IntVar(&regLevel, "level", 0, "default security level (0-2)")
	toolRegisterCmd.Flags().StringVar(&regDescription, "description", "", "human-readable description")
	toolRegisterCmd.Flags().StringArrayVar(&regEnv, "env", nil, "environment variables (KEY=VALUE)")
	toolCmd.AddCommand(toolRegisterCmd)
}

// registerTemplate is the YAML structure for editor mode.
type registerTemplate struct {
	Tools []registerEntry `yaml:"tools"`
}

type registerEntry struct {
	Name        string            `yaml:"name"`
	Driver      string            `yaml:"driver"`
	Command     string            `yaml:"command,omitempty"`
	Endpoint    string            `yaml:"endpoint,omitempty"`
	Level       int               `yaml:"level"`
	Description string            `yaml:"description,omitempty"`
	Env         map[string]string `yaml:"env,omitempty"`
}

func registerViaEditor(cmd *cobra.Command) error {
	// Detect editor
	editor := detectEditor()

	// Create temp file with template
	tmpFile, err := os.CreateTemp("", "gogent-tool-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	// Write template
	tmpl := `# Gogent Tool Registration
# Lines starting with '#' are comments and will be ignored.
# Save and close to register. One failure won't block others.
---
tools:
  - name: my-tool              # required: lowercase, dot-separated
    driver: builtin            # builtin | process | http
    level: 0                   # 0 (safe) | 1 | 2 (elevated)
    description: ""            # optional

  # Uncomment and edit for more tools:
  # - name: another-tool
  #   driver: process
  #   command: "./server.sh"
  #   level: 2
  #   env:
  #     API_KEY: "${MY_KEY}"
  #   description: "Custom MCP server"
`
	if _, err := tmpFile.WriteString(tmpl); err != nil {
		tmpFile.Close()
		return fmt.Errorf("write template: %w", err)
	}
	tmpFile.Close()

	// Launch editor
	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr
	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("editor %q exited with error: %w", editor, err)
	}

	// Read back
	data, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("read edited file: %w", err)
	}

	var tmplData registerTemplate
	if err := yaml.Unmarshal(data, &tmplData); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	if len(tmplData.Tools) == 0 {
		return fmt.Errorf("no tools defined in the file")
	}

	client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
	var anyErr bool
	for _, entry := range tmplData.Tools {
		req := &api.RegisterToolRequest{
			Name:         entry.Name,
			Driver:       entry.Driver,
			Command:      entry.Command,
			Endpoint:     entry.Endpoint,
			DefaultLevel: entry.Level,
			Description:  entry.Description,
			Env:          entry.Env,
		}
		if err := client.ToolRegister(req); err != nil {
			fmt.Fprintf(os.Stderr, "%s: failed — %v\n", entry.Name, err)
			anyErr = true
		} else {
			fmt.Printf("%s: registered\n", entry.Name)
		}
	}
	if anyErr {
		return fmt.Errorf("some tools failed to register")
	}
	return nil
}

func detectEditor() string {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(env); e != "" {
			return e
		}
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

func parseKeyValuePairs(pairs []string) map[string]string {
	m := make(map[string]string)
	for _, p := range pairs {
		for i := 0; i < len(p); i++ {
			if p[i] == '=' {
				m[p[:i]] = p[i+1:]
				break
			}
		}
	}
	return m
}
