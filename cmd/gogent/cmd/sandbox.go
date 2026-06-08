package cmd

import "github.com/spf13/cobra"

var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "Manage sandbox providers, profiles, and defaults",
	Long: `Manage the daemon's sandbox configuration (sandbox.yaml).

Subcommands: provider, profile, defaults, edit, status.`,
}

func init() {
	rootCmd.AddCommand(sandboxCmd)
}
