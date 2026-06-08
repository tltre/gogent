package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var sandboxStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show sandbox system overview",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		status, err := client.SandboxStatus()
		if err != nil {
			return fmt.Errorf("get sandbox status: %w", err)
		}

		fmt.Printf("Providers (%d):\n", len(status.Providers))
		for _, p := range status.Providers {
			fmt.Printf("  %s  %s\n", p.Name, p.Type)
		}
		fmt.Println()
		fmt.Printf("Profiles (%d):\n", len(status.Profiles))
		for _, p := range status.Profiles {
			fmt.Printf("  %s  provider=%s  template=%s\n", p.Name, p.Provider, p.Template)
		}
		return nil
	},
}

func init() {
	sandboxCmd.AddCommand(sandboxStatusCmd)
}
