package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon"
)

var (
	profProvider string
	profTemplate string
	profNetwork  bool
)

var sandboxProfileAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a sandbox profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n := profNetwork
		req := api.SandboxProfileRequest{
			Name: args[0], Provider: profProvider,
			Template: profTemplate, Network: &n,
		}
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.SandboxProfileAdd(req); err != nil {
			return fmt.Errorf("add profile: %w", err)
		}
		fmt.Printf("profile %q added\n", args[0])
		return nil
	},
}

func init() {
	sandboxProfileAddCmd.Flags().StringVar(&profProvider, "provider", "", "referenced provider name")
	sandboxProfileAddCmd.Flags().StringVar(&profTemplate, "template", "", "sandbox template ID")
	sandboxProfileAddCmd.Flags().BoolVar(&profNetwork, "network", false, "enable network access")
	sandboxProfileAddCmd.MarkFlagRequired("provider")
	sandboxProfileAddCmd.MarkFlagRequired("template")
	sandboxCmd.AddCommand(sandboxProfileAddCmd)
}
