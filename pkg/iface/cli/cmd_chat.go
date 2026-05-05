package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tltre/gagent/pkg/agentcore"
	"github.com/tltre/gagent/pkg/component"
)

func buildChat(cli *DefaultCLI) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start interactive chat",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runChat(cmd.Context(), cli)
		},
	}
}

func runChat(ctx context.Context, cli *DefaultCLI) error {
	reg := cli.Registry()
	agentComp := reg.GetDefault(component.ComponentAgentCore)
	if agentComp == nil {
		return fmt.Errorf("no AgentCore registered")
	}
	agent, ok := agentComp.(*agentcore.AgentRuntime)
	if !ok {
		return fmt.Errorf("default component is not an AgentRuntime")
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(os.Stdout, cli.Prompt)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			fmt.Fprint(os.Stdout, cli.Prompt)
			continue
		}
		if input == "quit" || input == "exit" {
			return nil
		}

		output, err := agent.Run(ctx, agentcore.Input{
			Messages: []agentcore.Message{
				{Role: "user", Content: input},
			},
		})
		if err != nil {
			fmt.Fprintf(os.Stdout, "Error: %v\n%s", err, cli.Prompt)
			continue
		}

		fmt.Fprintf(os.Stdout, "%s\n%s", output.Response.Content, cli.Prompt)
	}

	return scanner.Err()
}
