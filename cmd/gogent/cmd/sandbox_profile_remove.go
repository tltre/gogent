package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var sandboxProfileRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a sandbox profile",
	Long:  `Remove a profile. Apps using this profile will fail on new sandbox creation.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.SandboxProfileRemove(args[0]); err != nil {
			return fmt.Errorf("remove profile: %w", err)
		}
		fmt.Printf("profile %q removed\n", args[0])
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(sandboxProfileRemoveCmd)
}
