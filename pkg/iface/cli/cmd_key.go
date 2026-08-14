package cli

import (
	"fmt"
	"strings"

	"github.com/tltre/gogent/pkg/provider"
)

// keyCommand is the parsed form of a /key REPL command.
type keyCommand struct {
	kind   string // "list" | "get" | "set" | "delete"
	name   string // provider name
	apiKey string // for "set"
}

// parseKeyCommand parses a REPL line starting with "/key".
//
//	/key                        → list configured providers (masked keys)
//	/key <name>                 → show configured status for one provider
//	/key <name> <apiKey>        → set the API key for a provider
//	/key <name> --delete        → remove the API key for a provider
func parseKeyCommand(line string) (*keyCommand, bool) {
	line = strings.TrimSpace(line)
	if line == "/key" {
		return &keyCommand{kind: "list"}, true
	}
	if !strings.HasPrefix(line, "/key ") {
		return nil, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "/key "))
	if rest == "" {
		return &keyCommand{kind: "list"}, true
	}

	parts := strings.Fields(rest)
	name := parts[0]
	switch {
	case len(parts) >= 2 && parts[1] == "--delete":
		return &keyCommand{kind: "delete", name: name}, true
	case len(parts) >= 2:
		// Join the remainder in case a key contains no spaces (keys usually
		// don't); single field after name means key is empty → show.
		key := strings.Join(parts[1:], "")
		if key == "" {
			return &keyCommand{kind: "get", name: name}, true
		}
		return &keyCommand{kind: "set", name: name, apiKey: key}, true
	default:
		return &keyCommand{kind: "get", name: name}, true
	}
}

// maskKey renders a key with only the last 4 characters visible.
func maskKey(key string) string {
	if key == "" {
		return "(not configured)"
	}
	if len(key) <= 8 {
		return "sk-****"
	}
	return "****" + key[len(key)-4:]
}

// renderKeyList formats configured credentials for all providers present in
// the manager plus any additional configured entries.
func renderKeyList(mgr *provider.ProviderManager, store provider.CredentialStore) string {
	if store == nil {
		return "no credential store available (provider keys cannot be configured)"
	}

	var b strings.Builder
	b.WriteString("Provider credentials:\n")

	// Providers known to the manager first, then any extra configured keys.
	seen := map[string]bool{}
	if mgr != nil {
		for _, info := range mgr.List() {
			seen[info.Name] = true
			key, _ := store.Get(info.Name)
			fmt.Fprintf(&b, "  %-10s %s\n", info.Name, maskKey(key))
		}
	}
	names, _ := store.List()
	for _, name := range names {
		if seen[name] {
			continue
		}
		key, _ := store.Get(name)
		fmt.Fprintf(&b, "  %-10s %s\n", name, maskKey(key))
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// handleKeyCommand executes a /key command against the credential store.
// Returns (handled, continueChat).
func handleKeyCommand(mgr *provider.ProviderManager, store provider.CredentialStore, line string) (bool, bool) {
	kc, ok := parseKeyCommand(line)
	if !ok {
		return false, true
	}

	switch kc.kind {
	case "list":
		fmt.Println(renderKeyList(mgr, store))

	case "get":
		if store == nil {
			fmt.Println("no credential store available")
			break
		}
		key, _ := store.Get(kc.name)
		fmt.Printf("%s: %s\n", kc.name, maskKey(key))

	case "set":
		if store == nil {
			fmt.Println("no credential store available")
			break
		}
		// Optionally validate the provider exists.
		if mgr != nil && !providerExists(mgr, kc.name) {
			fmt.Printf("provider %q not found (use /provider to list)\n", kc.name)
			break
		}
		if err := store.Set(kc.name, kc.apiKey); err != nil {
			fmt.Printf("failed to save key for %s: %v\n", kc.name, err)
			break
		}
		fmt.Printf("Saved API key for %s\n", kc.name)

	case "delete":
		if store == nil {
			fmt.Println("no credential store available")
			break
		}
		if err := store.Delete(kc.name); err != nil {
			fmt.Printf("failed to delete key for %s: %v\n", kc.name, err)
			break
		}
		fmt.Printf("Removed API key for %s\n", kc.name)
	}

	return true, true
}

func providerExists(mgr *provider.ProviderManager, name string) bool {
	for _, info := range mgr.List() {
		if info.Name == name {
			return true
		}
	}
	return false
}
