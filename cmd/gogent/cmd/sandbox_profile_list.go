package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var sandboxProfileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sandbox profiles",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		status, err := client.SandboxStatus()
		if err != nil {
			return fmt.Errorf("get sandbox status: %w", err)
		}
		if len(status.Profiles) == 0 {
			fmt.Println("No profiles configured.")
			return nil
		}
		for _, p := range status.Profiles {
			fmt.Printf("  %s  provider=%s  template=%s\n", p.Name, p.Provider, p.Template)
		}
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(sandboxProfileListCmd)
}
