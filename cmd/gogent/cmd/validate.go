package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/app"
)

var validateCmd = &cobra.Command{
	Use:   "validate <config.yaml>",
	Short: "Validate configuration YAML without starting",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}

		_, err = app.ParseConfig(data)
		if err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		fmt.Println("config is valid")
		return nil
	},
}
