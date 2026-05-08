package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show framework version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("gogent version %s\n", mgmt.Version)
	},
}
