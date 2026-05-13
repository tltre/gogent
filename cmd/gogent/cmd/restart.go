package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var restartCmd = &cobra.Command{
	Use:   "restart <name>",
	Short: "Restart a running agent application (stop + reload)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)

		// Look up current app info to read its config_path
		appInfo, err := client.AppStatus(name)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				fmt.Printf("app %s not found\n", name)
				return nil
			}
			return fmt.Errorf("lookup %s: %w", name, err)
		}

		// Stop the running app
		if err := client.StopApp(name); err != nil {
			return fmt.Errorf("stop %s: %w", name, err)
		}

		// Re-load using the same config path
		newInfo, err := client.LoadApp(appInfo.ConfigPath)
		if err != nil {
			return fmt.Errorf("restart %s: %w", name, err)
		}

		fmt.Printf("app %s restarted | port=%s pid=%d\n", newInfo.Name, newInfo.Port, newInfo.PID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(restartCmd)
}
