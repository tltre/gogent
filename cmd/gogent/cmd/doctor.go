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
			// No args: get all component health from daemon, group by App.
			compResults, compErr := client.ComponentsHealth("")
			if compErr == nil {
				// Try to group by app using the App→Component mapping.
				apps, appErr := client.ListApps()
				if appErr == nil && len(apps) > 0 {
					printComponentsHealthGrouped(compResults, apps)
				} else {
					printComponentsHealthTable(compResults)
				}
				return nil
			}
			// Fallback: try daemon mode via per-app health.
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
			// Fallback: single-agent mode.
		} else {
			// With arg: filtered component health for the named app.
			name := args[0]
			compResults, compErr := client.ComponentsHealth(name)
			if compErr == nil {
				fmt.Printf("=== %s ===\n", name)
				printComponentsHealthTable(compResults)
				return nil
			}
			// Fallback: try per-app health via AppStatus.
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

		// Fallback: single agent (current behavior).
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

func printComponentsHealthTable(results []mgmt.ComponentHealthResult) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tDRIVER\tSTATUS\tLATENCY")
	for _, r := range results {
		latency := fmt.Sprintf("%dms", r.LatencyMs)
		if r.Error != "" {
			latency = r.Error
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Name, r.Type, r.Driver, r.Status, latency)
	}
	w.Flush()
}

// printComponentsHealthGrouped displays component health results grouped by
// App using the bidirectional App→Component mapping from AppInfo.
func printComponentsHealthGrouped(results []mgmt.ComponentHealthResult, apps []mgmt.AppInfo) {
	// Build component→app lookup map from AppInfo.Components.
	compApp := make(map[string]string) // component name → app name
	for _, app := range apps {
		for _, compName := range app.Components {
			compApp[compName] = app.Name
		}
	}

	// Group results by app.
	appResults := make(map[string][]mgmt.ComponentHealthResult)
	var uncategorized []mgmt.ComponentHealthResult
	for _, r := range results {
		if app, ok := compApp[r.Name]; ok {
			appResults[app] = append(appResults[app], r)
		} else {
			uncategorized = append(uncategorized, r)
		}
	}

	// Print per-app sections in app registration order.
	printed := 0
	for _, app := range apps {
		compList, ok := appResults[app.Name]
		if !ok {
			continue
		}
		if printed > 0 {
			fmt.Println()
		}
		fmt.Printf("=== %s ===\n", app.Name)
		printComponentsHealthTable(compList)
		printed++
	}

	// Print standalone components (not owned by any app).
	if len(uncategorized) > 0 {
		if printed > 0 {
			fmt.Println()
		}
		fmt.Println("=== (standalone) ===")
		printComponentsHealthTable(uncategorized)
	}
}
