package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func buildVersion(cli *DefaultCLI) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print agent version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Gogent agent")
			return nil
		},
	}
}
