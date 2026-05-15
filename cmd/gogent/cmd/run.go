package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/internal/mgmt"
)

var tuiMode bool
var keepAlive bool

var runCmd = &cobra.Command{
	Use:   "run <config.yaml>",
	Short: "Start agent with interactive CLI REPL",
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
	runCmd.Flags().BoolVar(&keepAlive, "keep-alive", false, "keep app running after REPL exits")
}

func runAgent(configPath string, mgmtPort string) error {
	// 1. Ensure daemon is running and get a client.
	daemonClient, err := mgmt.EnsureDaemon(mgmtPort)
	if err != nil {
		return fmt.Errorf("daemon: %w", err)
	}

	// 2. Load the app through the daemon.
	info, err := daemonClient.LoadApp(configPath)
	if err != nil {
		return fmt.Errorf("load app via daemon: %w", err)
	}

	fmt.Fprintf(os.Stderr, "app %s loaded | port=%s pid=%d\n", info.Name, info.Port, info.PID)

	// 3. Connect to the app's management endpoint for REPL.
	appClient := mgmt.NewClient(info.Port)

	// 4. Run interactive REPL.
	runErr := runREPL(appClient, info.Name, daemonClient)

	return runErr
}

// runREPL runs an interactive read-eval-print loop using the app's
// management API. On exit it stops the app unless --keep-alive is set.
func runREPL(client *mgmt.Client, appName string, daemonClient *mgmt.Client) error {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(os.Stdout, "> ")

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Fprint(os.Stdout, "> ")
			continue
		}
		if input == "quit" || input == "exit" {
			break
		}

		resp, err := client.AgentRun([]mgmt.AgentMessage{
			{Role: "user", Content: input},
		}, nil)

		if err != nil {
			fmt.Fprintf(os.Stdout, "Error: %v\n> ", err)
			continue
		}

		if resp.Error != "" {
			fmt.Fprintf(os.Stdout, "Error: %s\n> ", resp.Error)
		} else {
			fmt.Fprintf(os.Stdout, "%s\n", resp.Response.Content)
		}
		fmt.Fprint(os.Stdout, "> ")
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Stop the app unless --keep-alive is set.
	if !keepAlive {
		if err := daemonClient.StopApp(appName); err != nil {
			return fmt.Errorf("stop app: %w", err)
		}
		fmt.Fprintln(os.Stderr, "app stopped")
	}

	return nil
}
