# Quickstart

This guide takes you from zero to your first Gogent agent: clone the project → build the CLI → start an agent with the bundled example config (OpenAI + DeepSeek dual engines, ReAct tool calling, and OTel observability) → experience engine switching in the interactive chat.

## Prerequisites

- Go 1.25+
- At least one LLM API key (OpenAI or DeepSeek, for real conversations; the agent starts fine without one, you just get a credential-missing error on calls)
- (Optional) Docker + Jaeger, for viewing OTel traces

## 1. Get the Code and Build

```bash
git clone https://github.com/tltre/gogent
cd gogent

# Build the framework CLI
go build -o gogent.exe ./cmd/gogent
```

`gogent` is the framework's management CLI with subcommands such as `run` / `serve` / `stop` / `list` / `status` / `doctor` / `logs` / `tool` / `sandbox`.

## 2. Inspect the Example Config

```bash
gogent validate config/example.yaml   # validate the YAML offline
gogent inspect config/example.yaml    # show the parsed configuration
```

`config/example.yaml` is a fully runnable example containing:

- **interface**: a CLI interactive interface (chat REPL)
- **observability**: OTel export to a local Jaeger (optional)
- **tools**: declares the three built-in tools calculator / think / todo
- **components**: a react agentcore (built-in engines are always registered via the `provider` section)

## 3. Configure Your API Key

Configure the key before starting, either way works:

**Option A — write the file directly** (script/CI friendly):

```bash
# Windows
$env:USERPROFILE\.gogent\apps\example-agent\credentials.yaml
# Contents:
# openai: "sk-YOUR_OPENAI_KEY"
# deepseek: "sk-YOUR_DEEPSEEK_KEY"
```

**Option B — configure interactively in the REPL** (recommended; see the `/key` command in step 5), no file editing needed.

## 4. Start the Agent

```bash
./gogent.exe run config/example.yaml
```

What happens:

1. `run` auto-detects and starts the daemon (a background guardian process managing agent lifecycle and port allocation)
2. The daemon parses the config, allocates ports, and prepares components
3. The agent starts in the current process (foreground mode) and enters the interactive chat REPL

## 5. Your First Conversation

After startup you'll see the banner and the `> ` prompt:

```
> introduce yourself in one sentence
(the agent replies using the default engine)

> what is 1234 * 5678
(the model calls the calculator tool → the result is fed back → final answer;
this is the ReAct loop)
```

**Switching engines and models**:

```
> /provider
Available providers:
  1. openai    (OpenAI)     models: ...
  2. deepseek  (DeepSeek)   models: ...

> /provider deepseek
Switched to deepseek (model: deepseek-chat)

> /model deepseek-reasoner
Switched model to deepseek-reasoner
```

**Configuring credentials (when you didn't pre-configure a key)**:

```
> /key openai sk-YOUR_OPENAI_KEY
Saved API key for openai

> /key
Provider credentials:
  openai     ****xxxx
  deepseek   (not configured)
```

A `/key` setting takes effect on the **very next message** — no restart needed.

## 6. Non-interactive Single Call

The framework CLI's `gogent run <config>` starts the interactive chat REPL. For non-interactive single calls, use the **agent's own `run` subcommand** (`-p/--prompt` for the prompt, `--provider`/`--model` to select the engine), aimed at apps embedding Gogent or scripted calls:

```
agent run -p "introduce yourself in one sentence" --provider openai --model gpt-4o-mini
```

> Note: after building an app, its CLI enters the chat REPL by default; the `run` subcommand is for launching the agent in subcommand mode (see [Providers](providers.md) → "Runtime switching").

## 7. (Optional) View OTel Traces

```bash
docker run --rm -d --name jaeger \
  -e COLLECTOR_OTLP_ENABLED=true \
  -p 16686:16686 -p 4317:4317 \
  jaegertracing/all-in-one:latest
```

Open http://localhost:16686 → Search → search by `service: example-agent` to see:

```
agent.run                 ← the ReAct run as a whole
├── agent.llm.generate    ← each LLM call
├── tool.exec             ← each tool call (with auth/hook/result events)
└── agent.llm.generate    ← final reasoning based on the tool result
```

## Managing a Running Agent

In another terminal:

```bash
./gogent.exe list              # list all agents (NAME/PORT/PID/STATUS)
./gogent.exe status            # component status table
./gogent.exe doctor            # per-component health checks
./gogent.exe logs              # real-time log stream
./gogent.exe stop example-agent # stop the agent
```

## Next Steps

- [Configuration](configuration.md) — what every YAML field means
- [Providers](providers.md) — adding LLM providers, credentials, runtime switching
- [Agents](agents.md) — react agent behavior, context/memory, tool calling
