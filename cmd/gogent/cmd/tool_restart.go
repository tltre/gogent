package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var toolRestartCmd = &cobra.Command{
	Use:   "restart <server-name>",
	Short: "Restart a registered MCP server",
	Long: `Restart a registered MCP server. This closes the existing connection
and re-forks (for process drivers) or re-connects (for http drivers).
Discovered tools are re-registered. Useful for recovering STOPPED servers.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.ToolRestart(args[0]); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
		fmt.Printf("server %q restarted\n", args[0])
		return nil
	},
}

func init() {
	toolCmd.AddCommand(toolRestartCmd)
}
