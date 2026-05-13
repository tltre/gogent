package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var port string

var rootCmd = &cobra.Command{
	Use:   "gogent",
	Short: "Gogent framework CLI — manage and observe agent applications",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// FIXME: only handles bare port numbers (9090), not host:port (localhost:9090)
		if port != "" && !strings.HasPrefix(port, ":") && !strings.Contains(port, ":") {
			port = ":" + port
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&port, "port", "9090", "management port (default 9090)")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(listCmd)
}
