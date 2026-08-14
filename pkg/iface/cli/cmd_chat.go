package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/agentcore"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/contextmanager"
	"github.com/tltre/gogent/pkg/provider"
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

	// v0.14.6: session-level provider/model selection.
	mgr := providerManagerFrom(cli)
	currentProvider, currentModel := initialSelection(mgr)

	// v0.15.4: conversation session — ContextManager keeps cross-turn history.
	// A session is created at chat start and passed via Input.SessionID so the
	// ReactAgent persists each turn (user + assistant) and reloads history.
	cm := contextManagerFrom(cli)
	sessionID := ""
	if cm != nil {
		sessionID = cm.NewSession()
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

		// Handle /provider, /model and /key REPL commands (v0.14.x).
		if handled, cont := handleProviderCommand(mgr, input, &currentProvider, &currentModel); handled {
			if !cont {
				return nil
			}
			fmt.Fprint(os.Stdout, cli.Prompt)
			continue
		}
		if handled, cont := handleKeyCommand(mgr, cli.credStore, input); handled {
			if !cont {
				return nil
			}
			fmt.Fprint(os.Stdout, cli.Prompt)
			continue
		}

		output, err := agent.Run(ctx, agentcore.Input{
			Messages: []agentcore.Message{
				{Role: "user", Content: input},
			},
			ProviderName: currentProvider,
			ModelName:    currentModel,
			SessionID:    sessionID,
		})
		if err != nil {
			fmt.Fprintf(os.Stdout, "Error: %v\n%s", err, cli.Prompt)
			continue
		}

		fmt.Fprintf(os.Stdout, "%s\n%s", output.Response.Content, cli.Prompt)
	}

	return scanner.Err()
}

// contextManagerFrom returns the ContextManager component from the registry,
// or nil when none is registered.
func contextManagerFrom(cli *DefaultCLI) *contextmanager.ContextManagerComponent {
	reg := cli.Registry()
	if reg == nil {
		return nil
	}
	comp := reg.GetDefault(component.ComponentContextManager)
	cm, ok := comp.(*contextmanager.ContextManagerComponent)
	if !ok {
		return nil
	}
	return cm
}

// providerManagerFrom returns the ProviderManager from the registry, or nil
// when no provider component is registered.
func providerManagerFrom(cli *DefaultCLI) *provider.ProviderManager {
	reg := cli.Registry()
	if reg == nil {
		return nil
	}
	comp := reg.GetDefault(component.ComponentProvider)
	if comp == nil {
		return nil
	}
	mgr, ok := comp.(*provider.ProviderManager)
	if !ok {
		return nil
	}
	return mgr
}

// initialSelection picks the session's starting provider/model: the first
// registered provider with its default model.
func initialSelection(mgr *provider.ProviderManager) (providerName, modelName string) {
	if mgr == nil {
		return "", ""
	}
	infos := mgr.List()
	if len(infos) == 0 {
		return "", ""
	}
	first := infos[0]
	return first.Name, first.DefaultModel
}

// handleProviderCommand executes a /provider or /model command against the
// session state. Returns (handled, continueChat): handled reports whether
// the line was a provider command; continueChat is false when the chat
// session should exit.
func handleProviderCommand(mgr *provider.ProviderManager, line string, currentProvider, currentModel *string) (bool, bool) {
	pc, ok := parseProviderCommand(line)
	if !ok {
		return false, true
	}

	switch pc.kind {
	case "list":
		var infos []provider.ProviderInfo
		if mgr != nil {
			infos = mgr.List()
		}
		fmt.Println(renderProviderList(infos, *currentProvider, *currentModel))

	case "switch-provider":
		if mgr == nil {
			fmt.Println("no providers configured")
			break
		}
		info, err := resolveProvider(mgr, pc.name)
		if err != nil {
			fmt.Println(err)
			break
		}
		model, err := resolveModel(info, "")
		if err != nil {
			fmt.Println(err)
			break
		}
		*currentProvider = info.Name
		*currentModel = model
		fmt.Printf("Switched to %s (model: %s)\n", info.Name, model)

	case "show-model":
		fmt.Printf("Current model: %s\n", *currentModel)

	case "switch-model":
		if mgr == nil {
			fmt.Println("no providers configured")
			break
		}
		info, err := resolveProvider(mgr, *currentProvider)
		if err != nil {
			fmt.Println(err)
			break
		}
		model, err := resolveModel(info, pc.name)
		if err != nil {
			fmt.Println(err)
			break
		}
		*currentModel = model
		fmt.Printf("Switched model to %s\n", model)
	}

	return true, true
}
