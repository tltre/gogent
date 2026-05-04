package iface

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tltre/gagent/pkg/agentcore"
	"github.com/tltre/gagent/pkg/component"
)

type DefaultCLI struct {
	Banner string
	Prompt string
}

func NewDefaultCLI(banner, prompt string) *DefaultCLI {
	if prompt == "" {
		prompt = "> "
	}
	return &DefaultCLI{
		Banner: banner,
		Prompt: prompt,
	}
}

func (c *DefaultCLI) Run(ctx context.Context, reg *component.Registry) error {
	if c.Banner != "" {
		fmt.Fprintln(os.Stdout, c.Banner)
	}

	agentComp := reg.GetDefault(component.ComponentAgentCore)
	if agentComp == nil {
		return fmt.Errorf("no AgentCore registered")
	}
	agent, ok := agentComp.(*agentcore.AgentRuntime)
	if !ok {
		return fmt.Errorf("default component is not an AgentRuntime")
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Fprint(os.Stdout, c.Prompt)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			fmt.Fprint(os.Stdout, c.Prompt)
			continue
		}
		if line == "quit" || line == "exit" {
			return nil
		}

		input := agentcore.Input{
			Messages: []agentcore.Message{
				{Role: "user", Content: line},
			},
		}

		output, err := agent.Run(ctx, input)
		if err != nil {
			fmt.Fprintf(os.Stdout, "Error: %v\n%s", err, c.Prompt)
			continue
		}

		fmt.Fprintf(os.Stdout, "%s\n%s", output.Response.Content, c.Prompt)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("cli: %w", err)
	}
	return nil
}
