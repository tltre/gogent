package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Health check all components",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)
		results, err := client.Health()
		if err != nil {
			return fmt.Errorf("connect: %w (is agent running on %s?)", err, mgmtPort)
		}

		if len(results) == 0 {
			fmt.Println("no components to check")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "COMPONENT\tSTATUS\tLATENCY")
		for _, r := range results {
			latency := fmt.Sprintf("%dms", r.LatencyMs)
			if r.Error != "" {
				latency = r.Error
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", r.Component, r.Status, latency)
		}
		return w.Flush()
	},
}
