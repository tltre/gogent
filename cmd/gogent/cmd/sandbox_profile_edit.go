package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon"
)

var (
	editProfileProvider string
	editProfileTemplate string
	editProfileNetwork  bool
	editProfileHasNet   bool // whether --network was explicitly set
)

var sandboxProfileEditCmd = &cobra.Command{
	Use:   "edit <name>",
	Short: "Edit a sandbox profile's fields",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var req api.SandboxProfileEditRequest
		if cmd.Flags().Changed("provider") {
			req.Provider = &editProfileProvider
		}
		if cmd.Flags().Changed("template") {
			req.Template = &editProfileTemplate
		}
		if cmd.Flags().Changed("network") {
			req.Network = &editProfileNetwork
		}
		if req.Provider == nil && req.Template == nil && req.Network == nil {
			return fmt.Errorf("at least one field to change is required (--provider, --template, --network)")
		}

		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.SandboxProfileEdit(args[0], req); err != nil {
			return fmt.Errorf("edit profile: %w", err)
		}
		fmt.Printf("profile %q updated\n", args[0])
		return nil
	},
}

func init() {
	sandboxProfileEditCmd.Flags().StringVar(&editProfileProvider, "provider", "", "change provider name")
	sandboxProfileEditCmd.Flags().StringVar(&editProfileTemplate, "template", "", "change template ID")
	sandboxProfileEditCmd.Flags().BoolVar(&editProfileNetwork, "network", false, "change network access")
	sandboxCmd.AddCommand(sandboxProfileEditCmd)
}
