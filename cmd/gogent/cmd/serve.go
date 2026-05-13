package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var serveAgentPort string

var serveCmd = &cobra.Command{
	Use:   "serve <config.yaml>",
	Short: "Start agent as a background service (no REPL)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return serveAgent(args[0], port)
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveAgentPort, "agent-port", "", "port override for the agent app (future-proof)")
}

// serveAgent attempts to load the app through the daemon. If no daemon is
// running it auto-starts one; if that fails, it falls back to a direct
// in-process start.
func serveAgent(configPath string, mgmtPort string) error {
	client, err := ensureDaemon(mgmtPort)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warn: %v, falling back to direct start\n", err)
		return serveAgentDirect(configPath, mgmtPort)
	}

	info, err := client.LoadApp(configPath)
	if err != nil {
		return fmt.Errorf("load app via daemon: %w", err)
	}

	fmt.Printf("app %s loaded | port=%s pid=%d\n", info.Name, info.Port, info.PID)
	return nil
}

// ---------------------------------------------------------------------------
// Fallback: direct in-process start (pre-v0.6.0 behavior)
// ---------------------------------------------------------------------------

// serveAgentDirect builds, initializes, and starts the agent directly in the
// current process. This is the fallback path when the daemon is unavailable.
func serveAgentDirect(configPath string, mgmtPort string) error {
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

	srv := mgmt.Listen(mgmtPort, application.Registry())
	defer srv.Shutdown(context.Background())

	if err := mgmt.WriteAppPortFile(application.Name(), mgmtPort); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not write port file: %v\n", err)
	}
	defer mgmt.RemoveAppPortFile(application.Name())

	fmt.Fprintf(os.Stderr, "agent serving on %s  pid=%d\n", mgmtPort, os.Getpid())

	<-done
	return application.Stop(context.Background())
}
