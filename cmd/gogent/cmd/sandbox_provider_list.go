package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var sandboxProviderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all sandbox providers",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		status, err := client.SandboxStatus()
		if err != nil {
			return fmt.Errorf("get sandbox status: %w", err)
		}
		if len(status.Providers) == 0 {
			fmt.Println("No providers configured.")
			return nil
		}
		for _, p := range status.Providers {
			fmt.Printf("  %s  %s\n", p.Name, p.Type)
		}
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(sandboxProviderListCmd)
}
