package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/api"
	"github.com/tltre/gogent/internal/daemon"
)

var (
	provType     string
	provEndpoint string
	provAPIKey   string
)

var sandboxProviderAddCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a sandbox provider",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := api.SandboxProviderRequest{
			Name: args[0], Type: provType,
			Endpoint: provEndpoint, APIKey: provAPIKey,
		}
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		if err := client.SandboxProviderAdd(req); err != nil {
			return fmt.Errorf("add provider: %w", err)
		}
		fmt.Printf("provider %q added (type=%s)\n", args[0], provType)
		return nil
	},
}

func init() {
	sandboxProviderAddCmd.Flags().StringVar(&provType, "type", "", "provider type (e2b, builtin)")
	sandboxProviderAddCmd.Flags().StringVar(&provEndpoint, "endpoint", "", "API endpoint (required for e2b)")
	sandboxProviderAddCmd.Flags().StringVar(&provAPIKey, "api-key", "", "API key (required for e2b)")
	sandboxProviderAddCmd.MarkFlagRequired("type")
	sandboxCmd.AddCommand(sandboxProviderAddCmd)
}
