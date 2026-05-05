package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gagent/pkg/agentcore"
	"github.com/tltre/gagent/pkg/component"
)

func buildRun(cli *DefaultCLI) *cobra.Command {
	var prompt string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single prompt non-interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" {
				return cmd.Help()
			}
			return runPrompt(cmd.Context(), cli, prompt)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "prompt to run")

	return cmd
}

func runPrompt(ctx context.Context, cli *DefaultCLI, prompt string) error {
	reg := cli.Registry()
	agentComp := reg.GetDefault(component.ComponentAgentCore)
	if agentComp == nil {
		return nil
	}
	agent, ok := agentComp.(*agentcore.AgentRuntime)
	if !ok {
		return nil
	}

	output, err := agent.Run(ctx, agentcore.Input{
		Messages: []agentcore.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return err
	}

	fmt.Println(output.Response.Content)
	return nil
}
