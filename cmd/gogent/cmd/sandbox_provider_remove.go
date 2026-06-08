package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var sandboxProviderRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a sandbox provider",
	Long:  `Remove a provider. Profiles referencing this provider will fail on new sandbox creation.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.SandboxProviderRemove(args[0]); err != nil {
			return fmt.Errorf("remove provider: %w", err)
		}
		fmt.Printf("provider %q removed\n", args[0])
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(sandboxProviderRemoveCmd)
}
