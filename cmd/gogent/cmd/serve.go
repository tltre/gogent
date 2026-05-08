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

var serveCmd = &cobra.Command{
	Use:   "serve <config.yaml>",
	Short: "Start agent as a background service (no REPL)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return serveAgent(args[0], port)
	},
}

func serveAgent(configPath string, mgmtPort string) error {
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

	if err := mgmt.WritePortFile(mgmtPort, "agent"); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not write port file: %v\n", err)
	}
	// FIXME: agent name should come from config, not hardcoded "agent"
	defer mgmt.RemovePortFile()

	fmt.Fprintf(os.Stderr, "agent serving on %s  pid=%d\n", mgmtPort, os.Getpid())

	<-ctx.Done()
	return application.Stop(ctx)
}
