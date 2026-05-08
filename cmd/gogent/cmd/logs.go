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
	Use:   "logs",
	Short: "View agent logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := mgmt.NewClient(mgmtPort)

		ctx := context.Background()
		if follow {
			ctx = cmd.Context()
		}

		body, err := client.Logs(ctx)
		if err != nil {
			return fmt.Errorf("connect: %w (is agent running on %s?)", err, mgmtPort)
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
