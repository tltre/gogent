package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var (
	basePort         string
	foregroundMode   bool
	credentialsPath  string
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Start the daemon process for multi-agent management",
	Long: `Starts the Gogent daemon process. The daemon manages the lifecycle
of agent instances, allocates ports, and provides a REST API for
CLI management commands (serve, stop, list, status, doctor, logs).

The daemon listens on localhost only (loopback). Use --port to
set the daemon's own listen port. By default the daemon forks to
background; use --foreground to keep it in the current process.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDaemon()
	},
}

func init() {
	daemonCmd.Flags().StringVar(&basePort, "base-port", "9090", "base port for agent port allocation (default 9090)")
	daemonCmd.Flags().BoolVarP(&foregroundMode, "foreground", "F", false, "run in foreground (don't daemonize)")
	daemonCmd.Flags().StringVar(&credentialsPath, "credentials", "", "path to credentials.yaml (default ~/.gogent/credentials.yaml)")
}

func runDaemon() error {
	// Default: daemonize to background. Fork a child process that runs
	// with --foreground so it knows not to fork again.
	if !foregroundMode {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		args := []string{"daemon", "--foreground", "--port", port, "--base-port", basePort}
		if credentialsPath != "" {
			args = append(args, "--credentials", credentialsPath)
		}
		cmd := exec.Command(exePath, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		daemon.DetachDaemon(cmd)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start background daemon: %w", err)
		}
		fmt.Fprintf(os.Stderr, "daemon started in background (pid=%d)\n", cmd.Process.Pid)
		return nil
	}

	// Foreground mode: this is the actual daemon process.
	bp, err := strconv.Atoi(basePort)
	if err != nil {
		return fmt.Errorf("invalid --base-port value %q: %w", basePort, err)
	}

	d := daemon.NewDaemon(bp, credentialsPath)

	healthCtx, healthCancel := context.WithCancel(context.Background())
	defer healthCancel()
	d.StartHealthCheck(healthCtx, 5*time.Second)

	compHealthCtx, compHealthCancel := context.WithCancel(context.Background())
	defer compHealthCancel()
	d.StartComponentHealthCheck(compHealthCtx, 15*time.Second)

	// Start credential file watcher (v0.12.6)
	credCtx, credCancel := context.WithCancel(context.Background())
	defer credCancel()
	d.StartCredentialWatch(credCtx)

	d.StartGrpc()

	// Start all MCP servers and discover tools (v0.12.8)
	d.StartAllServers(context.Background())

	// Start health check loop (v0.12.8)
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	defer lifecycleCancel()
	d.StartLifecycle(lifecycleCtx)

	srv := daemon.NewDaemonServer(port, d)
	defer srv.Shutdown(context.Background())

	if err := daemon.WriteDaemonPortFile(port); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not write daemon port file: %v\n", err)
	}
	defer daemon.RemoveDaemonPortFile()

	fmt.Fprintf(os.Stderr, "daemon listening on %s  pid=%d\n", port, os.Getpid())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan
	fmt.Fprintln(os.Stderr, "\nShutting down daemon...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := d.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "warn: daemon shutdown errors: %v\n", err)
	}

	return nil
}
