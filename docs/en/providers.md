# Providers

Gogent ships multi-engine LLM providers behind a unified `IProvider` interface: OpenAI, DeepSeek, and any OpenAI-compatible service (Groq, Mistral, Ollama, vLLM, and 10+ more). This page explains how to configure them, attach credentials, and switch at runtime.

## Built-in Engines

| Engine | Brand | API key env var | Notes |
|--------|-------|-----------------|-------|
| `openai` | OpenAI | `OPENAI_API_KEY` | full implementation: Generate/Stream/SSE/tool calling/model list |
| `deepseek` | DeepSeek | `DEEPSEEK_API_KEY` | thin OpenAI-compatible wrapper, built-in base URL |
| openai-compat | (base class) | per-vendor | not registered standalone; reused to add compatible vendors |

- **Model lists are fetched dynamically**: each engine pulls available models from `GET {baseURL}/models` (10-minute cache), **model names are never hardcoded**
- **Default model**: resolution chain is config/env → first `/models` entry → engine resolves dynamically

## Ways to Connect

### 1. YAML Declaration (recommended)

Built-in engines are **always registered** (no declaration needed); use the `exclude` blacklist in the top-level `provider:` section to remove what you don't want:

```yaml
provider:
  exclude: []            # empty = all enabled; e.g. exclude: ["deepseek"]
```

With the `provider` section absent, all built-in engines are available. The ProviderManager auto-becomes the default provider and the REPL `/provider` command lists/switches engines.

### 2. External Suppliers (gRPC Endpoint Direct Connection)

Any externally running service exposing `gogent.v1.ProviderService` can be registered via `servers`:

```yaml
provider:
  servers:
    - name: "my-gateway"          # registration name (shown in /provider)
      endpoint: "localhost:9092"  # external ProviderService gRPC address
```

- The app connects to the endpoint directly over gRPC — **no daemon involvement**
- `servers[].name` **must not collide with built-in engine names** — openai/deepseek are reserved to keep `/provider openai` from silently routing to a remote instead of the native engine (build fails with a clear error)

### 3. Code Injection (App Developers)

```go
builder, _ := app.NewBuilder("config.yaml")

application, err := builder.Build(
    app.WithProvider("openai", provider.NewOpenAI(provider.OpenAIConfig{
        Model: "gpt-4o",            // optional; empty = resolved dynamically
        Timeout: 60 * time.Second,  // optional; default 60s
    })),
    app.WithProvider("deepseek", provider.NewDeepSeek(provider.DeepSeekConfig{})),
)
```

### 4. Adding a New OpenAI-Compatible Vendor

Create a thin engine with the `NewOpenAICompat(baseURL)` base class (registered into the engine registry):

```go
// e.g. groq (custom file groq.go)
func init() {
    _ = provider.RegisterEngine("groq", func() (provider.IProvider, error) {
        return provider.NewOpenAICompat("https://api.groq.com/openai/v1"), nil
    })
}
```

Once registered it automatically shows up in `/provider` and can be toggled with the `exclude` blacklist. The engine name is also the CredentialStore key.

## Credentials

### Where Credentials Live

| Purpose | Path | Notes |
|---------|------|-------|
| Provider API keys | `~/.gogent/apps/<app-name>/credentials.yaml` | **app-scoped**, isolated from daemon-level credentials |
| Tool/sandbox secrets | `~/.gogent/credentials.yaml` | daemon-level, for MCP servers, E2B, etc. |

Provider credential file format (exact keys, matching engine names):

```yaml
# ~/.gogent/apps/example-agent/credentials.yaml
openai: "sk-..."
deepseek: "sk-..."
```

### Interactive Configuration (/key command)

In the chat REPL:

```
> /key                        # list credential status of all engines (keys masked)
> /key openai sk-xxx          # set openai's key (written to the app-scoped file, effective immediately)
> /key openai                 # check a single engine's status
> /key deepseek --delete      # delete a credential
```

The **next message uses the new key** — no restart required.

### Resolution Priority

1. CredentialStore (the app-scoped file written by `/key`)
2. Construction-time environment variables (`OPENAI_API_KEY` / `DEEPSEEK_API_KEY`)

### Replacing the Credential Implementation

The default `FileCredentialStore` can be swapped for any implementation (Vault, keyring, ...):

```go
builder.Build(app.WithCredentialStore(myStore))
```

## Runtime Switching

### chat REPL (session-level)

```
> /provider                   # list all engines + current selection
> /provider deepseek          # switch engine (auto-selects its default model)
> /model deepseek-reasoner    # switch the model of the current engine
> /model                      # show the current model
```

### run command (single call)

The agent's CLI provides a `run` subcommand for single non-interactive calls:

```
agent run -p "hello" --provider openai --model gpt-4o-mini
```

When provider/model are not given, the session's current selection is used; a model name is validated against the engine's available list. The framework CLI's `gogent run <config>` starts the interactive chat REPL (session-level switching) — a different granularity from the single call above.

## FAQ

| Symptom | Cause | Fix |
|---------|-------|-----|
| `/provider` list is empty | no engines registered or all excluded | check `provider.exclude` in the config |
| call reports "no api key for X" | engine has no credential configured | `/key X sk-...` or write credentials.yaml |
| model list is empty | no key configured, `/models` fetch failed | configure the key first; auto-retries within 10 min |
| want a model not in the list | model validation blocks it | confirm the model appears in the vendor's `/models` response |
