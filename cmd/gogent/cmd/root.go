package cmd

import (
	"github.com/spf13/cobra"
)

var port string

var rootCmd = &cobra.Command{
	Use:   "gogent",
	Short: "Gogent framework CLI — manage and observe agent applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&port, "port", ":9090", "management port address")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(versionCmd)
}
