package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var toolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := daemon.NewDaemonClient(resolveMgmtPort(cmd))
		tools, err := client.ToolsList()
		if err != nil {
			return fmt.Errorf("list tools: %w", err)
		}

		if len(tools) == 0 {
			fmt.Println("no tools registered")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tDRIVER\tLEVEL")
		for _, t := range tools {
			fmt.Fprintf(w, "%s\t%s\t%d\n", t.Name, t.Driver, t.DefaultLvl)
		}
		return w.Flush()
	},
}

func init() {
	toolCmd.AddCommand(toolListCmd)
}
