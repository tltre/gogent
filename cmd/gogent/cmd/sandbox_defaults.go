package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var (
	defBuiltin string
	defProcess string
)

var sandboxDefaultsCmd = &cobra.Command{
	Use:   "defaults [--builtin <profile>] [--process <profile>]",
	Short: "View or set default sandbox profile mappings",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		status, err := client.SandboxStatus()
		if err != nil {
			return fmt.Errorf("get sandbox status: %w", err)
		}
		fmt.Printf("Providers: %d\n", len(status.Providers))
		fmt.Printf("Profiles:  %d\n", len(status.Profiles))
		return nil
	},
}

func init() {
	sandboxDefaultsCmd.Flags().StringVar(&defBuiltin, "builtin", "", "default profile for builtin tools")
	sandboxDefaultsCmd.Flags().StringVar(&defProcess, "process", "", "default profile for process/http tools")
	sandboxCmd.AddCommand(sandboxDefaultsCmd)
}
