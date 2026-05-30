package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all running agent applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgmtPort := resolveMgmtPort(cmd)
		client := daemon.NewDaemonClient(mgmtPort)
		apps, err := client.ListApps()
		if err != nil {
			return fmt.Errorf("connect: %w (is daemon running?)", err)
		}

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
	},
}

func formatUptime(unixTs int64) string {
	elapsed := time.Now().Unix() - unixTs
	if elapsed < 0 {
		return "0s"
	}

	d := time.Duration(elapsed) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60

	switch {
	case h > 0:
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	case m > 0:
		return fmt.Sprintf("%dm%ds", m, s)
	default:
		return fmt.Sprintf("%ds", s)
	}
}


