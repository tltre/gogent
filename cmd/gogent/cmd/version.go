package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show framework version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("gogent version 0.5.0-dev")
	},
}
