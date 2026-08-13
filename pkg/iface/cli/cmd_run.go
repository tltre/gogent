package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
)

func buildRun(cli *DefaultCLI) *cobra.Command {
	var prompt string
	var providerName string
	var modelName string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run a single prompt non-interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			if prompt == "" {
				return cmd.Help()
			}
			return runPrompt(cmd.Context(), cli, prompt, providerName, modelName)
		},
	}

	cmd.Flags().StringVarP(&prompt, "prompt", "p", "", "prompt to run")
	// v0.14.6: single-call provider/model selection.
	cmd.Flags().StringVar(&providerName, "provider", "", "provider engine to use (e.g. openai, deepseek)")
	cmd.Flags().StringVar(&modelName, "model", "", "model to use within the provider")

	return cmd
}

func runPrompt(ctx context.Context, cli *DefaultCLI, prompt, providerName, modelName string) error {
	reg := cli.Registry()
	agentComp := reg.GetDefault(component.ComponentAgentCore)
	if agentComp == nil {
		return nil
	}
	agent, ok := agentComp.(*agentcore.AgentRuntime)
	if !ok {
		return nil
	}

	// v0.14.6: validate provider/model when explicitly requested.
	if providerName != "" || modelName != "" {
		if mgr := providerManagerFrom(cli); mgr != nil {
			if providerName == "" {
				if infos := mgr.List(); len(infos) > 0 {
					providerName = infos[0].Name
				}
			}
			if info, err := resolveProvider(mgr, providerName); err == nil {
				resolved, rErr := resolveModel(info, modelName)
				if rErr != nil {
					return rErr
				}
				modelName = resolved
			}
		}
	}

	output, err := agent.Run(ctx, agentcore.Input{
		Messages: []agentcore.Message{
			{Role: "user", Content: prompt},
		},
		ProviderName: providerName,
		ModelName:    modelName,
	})
	if err != nil {
		return err
	}

	fmt.Println(output.Response.Content)
	return nil
}
