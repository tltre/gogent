package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var forceUnregister bool

var toolUnregisterCmd = &cobra.Command{
	Use:   "unregister <name>",
	Short: "Unregister a tool from the daemon registry",
	Long: `Unregister a tool. If any running app is using this tool,
the command will refuse and list the affected apps.
Use --force to override. Restarting the daemon re-registers built-in tools.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))

		// Check if this is a built-in tool and warn
		info, err := client.ToolsStatus(args[0])
		if err == nil {
			if source, _ := info["source"].(string); source == "builtin" {
				fmt.Fprintf(os.Stderr, "Warning: %q is a built-in tool and will be re-registered on daemon restart\n", args[0])
			}
		}

		if err := client.ToolUnregister(args[0], forceUnregister); err != nil {
			return fmt.Errorf("unregister tool: %w", err)
		}
		fmt.Printf("tool %q unregistered\n", args[0])
		return nil
	},
}

func init() {
	toolUnregisterCmd.Flags().BoolVar(&forceUnregister, "force", false, "force unregister even if apps are using the tool")
	toolCmd.AddCommand(toolUnregisterCmd)
}
