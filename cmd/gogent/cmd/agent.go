package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/app"
)

var (
	agentConfig string
	agentPort   string
)

var agentCmd = &cobra.Command{
	Use:    "agent",
	Short:  "Internal: forked by daemon to run an independent agent process",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return forkAgent(agentConfig, agentPort)
	},
}

func init() {
	agentCmd.Flags().StringVar(&agentConfig, "config", "", "Path to agent YAML config (required)")
	agentCmd.Flags().StringVar(&agentPort, "port", "", "Management port for this agent (required)")
	agentCmd.MarkFlagRequired("config")
	agentCmd.MarkFlagRequired("port")
}

func forkAgent(configPath, listenPort string) error {
	// Normalize port: ensure ":" prefix for http.ListenAndServe
	if listenPort != "" && !strings.HasPrefix(listenPort, ":") && !strings.Contains(listenPort, ":") {
		listenPort = ":" + listenPort
	}

	builder, err := app.NewBuilder(configPath)
	if err != nil {
		return fmt.Errorf("create builder: %w", err)
	}

	application, err := builder.Build(app.WithMgmtPort(listenPort))
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

	fmt.Fprintf(os.Stderr, "agent %q serving on %s  pid=%d\n", application.Name(), listenPort, os.Getpid())

	return application.Run(ctx)
}
