package cmd

import (
	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

// resolveMgmtPort resolves the management port from the --port flag or
// the runtime port file written by a running agent.
func resolveMgmtPort(cmd *cobra.Command) string {
	if !cmd.Flags().Changed("port") {
		if pf, err := mgmt.ReadDaemonPortFile(); err == nil {
			return pf.Port
		}
	}
	return port
}
