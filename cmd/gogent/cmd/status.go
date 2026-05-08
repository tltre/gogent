package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show component status table",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)
		comps, err := client.Registry()
		if err != nil {
			return fmt.Errorf("connect: %w (is agent running on %s?)", err, mgmtPort)
		}

		if len(comps) == 0 {
			fmt.Println("no components registered")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tTYPE\tSTATUS")
		for _, c := range comps {
			fmt.Fprintf(w, "%s\t%s\t%s\n", c.Name, c.Type, c.Status)
		}
		return w.Flush()
	},
}

func resolveMgmtPort(cmd *cobra.Command) string {
	if !cmd.Flags().Changed("port") {
		if p, err := mgmt.PortFromFile(); err == nil {
			return p
		}
	}
	return port
}
