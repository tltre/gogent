package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
	"github.com/tltre/gogent/internal/mgmt"
)

var statusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Show component status table",
	Long: `Show component status table for a running agent.

Without arguments, lists all running apps managed by the daemon.
With a name argument, shows the component table for the specified app.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := daemon.NewDaemonClient(mgmtPort)

		if len(args) == 0 {
			// Try daemon first
			apps, err := client.ListApps()
			if err == nil {
				// Daemon is running — show app overview
				if len(apps) == 0 {
					fmt.Println("no apps running")
					return nil
				}
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "NAME\tPORT\tPID\tSTATUS\tUPTIME")
				for _, a := range apps {
					fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", a.Name, a.Port, a.PID, a.Status, formatUptime(a.StartedAt))
				}
				return w.Flush()
			}

			// Fallback: direct agent query (no daemon)
			appClient := mgmt.NewAppClient(mgmtPort)
			comps, err := appClient.Registry()
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
		}

		// Named app: resolve via daemon, connect to its port, query registry
		info, err := client.AppStatus(args[0])
		if err != nil {
			return fmt.Errorf("app %q not found: %w", args[0], err)
		}

		appClient := mgmt.NewAppClient(info.Port)
		comps, err := appClient.Registry()
		if err != nil {
			return fmt.Errorf("connect to %s (%s): %w", info.Name, info.Port, err)
		}

		if len(comps) == 0 {
			fmt.Printf("no components registered for %s\n", info.Name)
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


