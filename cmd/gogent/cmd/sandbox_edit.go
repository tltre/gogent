package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var sandboxEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Edit sandbox.yaml in $EDITOR",
	Long: `Opens ~/.gogent/sandbox.yaml in your configured editor ($EDITOR, $VISUAL).
After saving and exiting, the daemon reloads the configuration automatically.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("home dir: %w", err)
		}
		path := home + "/.gogent/sandbox.yaml"

		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = os.Getenv("VISUAL")
		}
		if editor == "" {
			if runtime.GOOS == "windows" {
				editor = "notepad"
			} else {
				editor = "vi"
			}
		}

		editCmd := exec.Command(editor, path)
		editCmd.Stdin = os.Stdin
		editCmd.Stdout = os.Stdout
		editCmd.Stderr = os.Stderr
		if err := editCmd.Run(); err != nil {
			return fmt.Errorf("editor: %w", err)
		}

		// Reload daemon after edit
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.SandboxReload(); err != nil {
			return fmt.Errorf("reload sandbox config: %w", err)
		}
		fmt.Println("sandbox.yaml reloaded")
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(sandboxEditCmd)
}
