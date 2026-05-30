package cmd

import "github.com/spf13/cobra"

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage tools in the daemon tool registry",
	Long: `Manage tool definitions registered with the running daemon.

Subcommands: list, status, register, unregister.`,
}

func init() {
	rootCmd.AddCommand(toolCmd)
}
