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
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)

		if len(args) == 0 {
			// Try daemon mode: show health for all registered apps
			apps, err := client.ListApps()
			if err == nil && len(apps) > 0 {
				for i, app := range apps {
					if i > 0 {
						fmt.Println()
					}
					fmt.Printf("=== %s ===\n", app.Name)
					appClient := mgmt.NewClient(app.Port)
					results, hErr := appClient.Health()
					if hErr != nil {
						fmt.Printf("error: %v\n", hErr)
						continue
					}
					printHealthTable(results)
				}
				return nil
			}
			// Fallback: single-agent mode
		} else {
			name := args[0]
			app, err := client.AppStatus(name)
			if err == nil && app != nil {
				appClient := mgmt.NewClient(app.Port)
				results, hErr := appClient.Health()
				if hErr != nil {
					return fmt.Errorf("connect to app %q on port %s: %w", name, app.Port, hErr)
				}
				fmt.Printf("=== %s ===\n", name)
				printHealthTable(results)
				return nil
			}
			return fmt.Errorf("app %q not found via daemon: %w", name, err)
		}

		// Fallback: single agent (current behavior)
		results, err := client.Health()
		if err != nil {
			return fmt.Errorf("connect: %w (is agent running on %s?)", err, mgmtPort)
		}
		if len(results) == 0 {
			fmt.Println("no components to check")
			return nil
		}
		printHealthTable(results)
		return nil
	},
}

func printHealthTable(results []mgmt.HealthResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tSTATUS\tLATENCY")
	for _, r := range results {
		latency := fmt.Sprintf("%dms", r.LatencyMs)
		if r.Error != "" {
			latency = r.Error
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.Component, r.Status, latency)
	}
	w.Flush()
}
