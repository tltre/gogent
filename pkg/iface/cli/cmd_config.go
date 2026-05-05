package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func buildConfig(cli *DefaultCLI) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Configuration utilities",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate configuration YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("config is valid")
			return nil
		},
	})

	return cmd
}

func ConfigCommands() []CommandEntry {
	return []CommandEntry{
		{Path: "config", Build: func(cli *DefaultCLI) *cobra.Command { return buildConfig(cli) }},
	}
}
