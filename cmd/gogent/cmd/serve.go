package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var serveAgentPort string

var serveCmd = &cobra.Command{
	Use:   "serve <config.yaml>",
	Short: "Start agent as a background service (no REPL)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return serveAgent(args[0], port)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAgentPort, "agent-port", "", "port override for the agent app (future-proof)")
}

// serveAgent attempts to load the app through the daemon. If no daemon is
// running it auto-starts one.
func serveAgent(configPath string, mgmtPort string) error {
	client, err := mgmt.EnsureDaemon(mgmtPort)
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}

	info, err := client.LoadApp(configPath)
	if err != nil {
		return fmt.Errorf("load app via daemon: %w", err)
	}

	fmt.Printf("app %s loaded | port=%s pid=%d\n", info.Name, info.Port, info.PID)
	return nil
}
