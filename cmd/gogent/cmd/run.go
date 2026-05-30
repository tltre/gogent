package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/daemon"
)

var tuiMode bool

var runCmd = &cobra.Command{
	Use:   "run <config.yaml>",
	Short: "Start agent with interactive CLI REPL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if tuiMode {
			fmt.Fprintln(os.Stderr, "TUI mode not yet implemented (v0.5.0+)")
			return nil
		}
		return runAgent(args[0])
	},
}

func init() {
	runCmd.Flags().BoolVarP(&tuiMode, "tui", "i", false, "start in TUI mode (placeholder)")
}

func runAgent(configPath string) error {
	// 1. Ensure daemon is running.
	daemonClient, err := daemon.EnsureDaemon(port)
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}

	// 2. Prepare app: fork components, allocate port, but don't fork agent.
	info, err := daemonClient.LoadApp(configPath, false)
	if err != nil {
		return fmt.Errorf("prepare app: %w", err)
	}

	// 3. Set env vars so the in-process agent can find daemon-forked components.
	for _, env := range info.Env {
		// Split "KEY=VALUE" and set.
		for i := 0; i < len(env); i++ {
			if env[i] == '=' {
				os.Setenv(env[:i], env[i+1:])
				break
			}
		}
	}

	// 4. Build and run the agent in-process (foreground=true → CLI REPL on terminal).
	fmt.Fprintf(os.Stderr, "app %s starting | port=%s\n", info.Name, info.Port)
	if err := forkAgent(configPath, info.Port, true); err != nil {
		return err
	}

	// 5. Cleanup: stop the app via daemon (frees ports, cleans up components).
	if err := daemonClient.StopApp(info.Name); err != nil {
		return fmt.Errorf("stop app: %w", err)
	}
	fmt.Fprintln(os.Stderr, "app stopped")
	return nil
}
