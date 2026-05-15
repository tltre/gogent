package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var basePort string

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the daemon process for multi-agent management",
	Long: `Starts the Gogent daemon process. The daemon manages the lifecycle
of agent instances, allocates ports, and provides a REST API for
CLI management commands (serve, stop, list, status, doctor, logs).

The daemon listens on localhost only (loopback). Use --port to
set the daemon's own listen port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon()
	},
}

func init() {
	daemonCmd.Flags().StringVar(&basePort, "base-port", "9090", "base port for agent port allocation (default 9090)")
}

func runDaemon() error {
	// Parse base port
	bp, err := strconv.Atoi(basePort)
	if err != nil {
		return fmt.Errorf("invalid --base-port value %q: %w", basePort, err)
	}

	// Create daemon
	d := mgmt.NewDaemon(bp)

	// Start health check to detect crashed apps (checks every 5s)
	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	d.StartHealthCheck(healthCtx, 5*time.Second)

	// Start component health check (separate goroutine, checks every 15s)
	compHealthCtx, compHealthCancel := context.WithCancel(context.Background())
	defer compHealthCancel()
	d.StartComponentHealthCheck(compHealthCtx, 15*time.Second)

	// Start HTTP server (reg is nil since daemon is not a Component)
	srv := mgmt.NewDaemonServer(port, nil, d)
	defer srv.Shutdown(context.Background())

	// Write daemon port file
	if err := mgmt.WriteDaemonPortFile(port); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not write daemon port file: %v\n", err)
	}
	defer mgmt.RemoveDaemonPortFile()

	fmt.Fprintf(os.Stderr, "daemon listening on %s  pid=%d\n", port, os.Getpid())

	// Block on interrupt signal (os.Interrupt for cross-platform: SIGINT on Unix, CTRL events on Windows)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)

	<-sigChan
	fmt.Fprintln(os.Stderr, "\nShutting down daemon...")

	// Graceful shutdown: stop all managed apps and clean up port files.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := d.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "warn: daemon shutdown errors: %v\n", err)
	}

	return nil
}
