package cmd

import (
	"bufio"
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var follow bool

var logsCmd = &cobra.Command{
	Use:   "logs [name]",
	Short: "View agent logs",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)

		// Determine the target app port
		targetPort := mgmtPort

		if len(args) == 1 {
			// Named app: look up via daemon
			app, err := client.AppStatus(args[0])
			if err != nil {
				return fmt.Errorf("lookup app %q: %w", args[0], err)
			}
			targetPort = app.Port
		} else {
			// No name given: try auto-select via daemon
			apps, err := client.ListApps()
			if err == nil {
				switch {
				case len(apps) == 0:
					fmt.Println("no apps running")
					return nil
				case len(apps) == 1:
					targetPort = apps[0].Port
				default:
					fmt.Printf("multiple apps running, specify name: gogent logs <name>\n")
					return nil
				}
			}
			// err != nil (no daemon) → fall through, use mgmtPort directly
		}

		target := mgmt.NewClient(targetPort)

		ctx := context.Background()
		// FIXME: non-follow logs should use a request-scoped context with timeout
		if follow {
			ctx = cmd.Context()
		}

		body, err := target.Logs(ctx)
		if err != nil {
			return fmt.Errorf("connect: %w (is agent running on %s?)", err, targetPort)
		}
		defer body.Close()

		scanner := bufio.NewScanner(body)
		for scanner.Scan() {
			fmt.Println(scanner.Text())
			if !follow {
				break
			}
		}
		return scanner.Err()
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
}
