package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
	"github.com/tltre/gogent/pkg/app"
)

var tuiMode bool

var runCmd = &cobra.Command{
	Use:   "run <config.yaml>",
	Short: "Run an agent application with management server",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if tuiMode {
			fmt.Fprintln(os.Stderr, "TUI mode not yet implemented (v0.5.0+)")
			return nil
		}
		return runAgent(args[0], port)
	},
}

func init() {
	runCmd.Flags().BoolVarP(&tuiMode, "tui", "i", false, "start in TUI mode (placeholder)")
}

func runAgent(configPath string, mgmtPort string) error {
	builder, err := app.NewBuilder(configPath)
	if err != nil {
		return fmt.Errorf("create builder: %w", err)
	}

	application, err := builder.Build()
	if err != nil {
		return fmt.Errorf("build application: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		cancel()
	}()

	if err := application.Initialize(ctx); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}

	if err := application.Start(ctx); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	srv := mgmt.Listen(mgmtPort, application.Registry())
	defer srv.Shutdown(context.Background())

	if iface := application.Interface(); iface != nil {
		err := iface.Run(ctx, application.Registry())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Application error: %v\n", err)
		}
	} else {
		<-ctx.Done()
	}

	return application.Stop(ctx)
}
