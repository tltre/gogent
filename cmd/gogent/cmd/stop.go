package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var stopCmd = &cobra.Command{
	Use:   "stop <name>",
	Short: "Stop a running agent application",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)

		err := client.StopApp(name)
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				fmt.Printf("app %s not found\n", name)
				return nil
			}
			return fmt.Errorf("stop %s: %w", name, err)
		}

		fmt.Printf("app %s stopped\n", name)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
