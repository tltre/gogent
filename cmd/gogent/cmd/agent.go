package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
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

	application, cancel, err := buildAndInitAgent(configPath)
	if err != nil {
		return err
	}
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		<-sigChan
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		cancel()
		close(done)
	}()

	srv := mgmt.Listen(listenPort, application.Registry())
	defer srv.Shutdown(context.Background())

	if err := mgmt.WriteAppPortFile(application.Name(), listenPort); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not write port file: %v\n", err)
	}
	defer mgmt.RemoveAppPortFile(application.Name())

	fmt.Fprintf(os.Stderr, "agent %q serving on %s  pid=%d\n", application.Name(), listenPort, os.Getpid())

	<-done
	return application.Stop(context.Background())
}
