package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var toolStatusCmd = &cobra.Command{
	Use:   "status <name>",
	Short: "Show detailed status for a registered tool",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		info, err := client.ToolsStatus(args[0])
		if err != nil {
			return fmt.Errorf("tool status: %w", err)
		}

		if info["name"] == nil {
			return fmt.Errorf("tool %q not found", args[0])
		}

		fmt.Printf("Name:       %v\n", info["name"])
		fmt.Printf("Driver:     %v\n", info["driver"])
		if d, ok := info["description"].(string); ok && d != "" {
			fmt.Printf("Descr:      %s\n", d)
		}
		fmt.Printf("Level:      %v\n", info["defaultLevel"])
		if s, ok := info["source"].(string); ok && s != "" {
			fmt.Printf("Source:     %s\n", s)
		}
		fmt.Printf("Status:     %v\n", info["status"])
		fmt.Printf("Calls:      %v (failures: %v)\n", info["invocations"], info["failures"])
		return nil
	},
}

func init() {
	toolCmd.AddCommand(toolStatusCmd)
}
