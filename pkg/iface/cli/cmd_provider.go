package cli

import (
	"fmt"
	"strings"

	"github.com/tltre/gogent/pkg/provider"
)

// providerCommand is the parsed form of a /provider or /model REPL command.
type providerCommand struct {
	kind string // "list" | "switch-provider" | "show-model" | "switch-model"
	name string // provider or model name (for switch commands)
}

// parseProviderCommand parses a REPL line starting with "/provider" or
// "/model". Returns (nil, false) for lines that are not provider commands.
//
//	/provider             → list providers + current selection
//	/provider <name>      → switch provider
//	/model                → show current model
//	/model <name>         → switch model of current provider
func parseProviderCommand(line string) (*providerCommand, bool) {
	line = strings.TrimSpace(line)
	switch {
	case line == "/provider":
		return &providerCommand{kind: "list"}, true
	case strings.HasPrefix(line, "/provider "):
		name := strings.TrimSpace(strings.TrimPrefix(line, "/provider "))
		if name == "" || strings.ContainsAny(name, " \t") {
			return &providerCommand{kind: "list"}, true
		}
		return &providerCommand{kind: "switch-provider", name: name}, true
	case line == "/model":
		return &providerCommand{kind: "show-model"}, true
	case strings.HasPrefix(line, "/model "):
		name := strings.TrimSpace(strings.TrimPrefix(line, "/model "))
		if name == "" || strings.ContainsAny(name, " \t") {
			return &providerCommand{kind: "show-model"}, true
		}
		return &providerCommand{kind: "switch-model", name: name}, true
	default:
		return nil, false
	}
}

// renderProviderList formats the available providers with the current
// selection highlighted.
func renderProviderList(infos []provider.ProviderInfo, currentProvider, currentModel string) string {
	if len(infos) == 0 {
		return "no providers configured"
	}
	var b strings.Builder
	b.WriteString("Available providers:\n")
	for i, info := range infos {
		marker := " "
		models := info.Models
		if len(models) == 0 {
			models = []string{"(no model info)"}
		}
		if info.Name == currentProvider {
			marker = ">"
			if currentModel == "" {
				currentModel = info.DefaultModel
			}
		}
		fmt.Fprintf(&b, " %s %d. %-10s %-14s models: %s\n",
			marker, i+1, info.Name, "("+info.DisplayName+")", strings.Join(models, ", "))
	}
	fmt.Fprintf(&b, "Current: %s (model: %s)\n", currentProvider, currentModel)
	return strings.TrimSuffix(b.String(), "\n")
}

// resolveProvider validates a provider name against the manager and returns
// its ProviderInfo. An error is returned for unknown providers.
func resolveProvider(mgr *provider.ProviderManager, name string) (*provider.ProviderInfo, error) {
	for _, info := range mgr.List() {
		if info.Name == name {
			return &info, nil
		}
	}
	return nil, fmt.Errorf("provider %q not found", name)
}

// resolveModel validates a model name against the provider's supported list.
// Empty model returns the provider's default model; if none is resolvable
// (e.g. the /models list has not been fetched because no key is configured
// yet), it returns "" without error so provider switching is never blocked —
// the engine resolves the model dynamically at Generate time.
func resolveModel(info *provider.ProviderInfo, model string) (string, error) {
	if model == "" {
		if info.DefaultModel != "" {
			return info.DefaultModel, nil
		}
		if len(info.Models) > 0 {
			return info.Models[0], nil
		}
		return "", nil
	}
	for _, m := range info.Models {
		if m == model {
			return model, nil
		}
	}
	return "", fmt.Errorf("model %q not available for provider %q (available: %s)",
		model, info.Name, strings.Join(info.Models, ", "))
}
