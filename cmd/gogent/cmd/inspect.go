package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/app"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect <config.yaml>",
	Short: "Show resolved configuration",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}

		cfg, err := app.ParseConfig(data)
		if err != nil {
			return fmt.Errorf("parse config: %w", err)
		}

		out, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Println(string(out))
		return nil
	},
}
