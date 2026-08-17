# 快速开始

本文带你从零跑起第一个 Gogent agent：clone 项目 → 构建 CLI → 用自带的示例配置启动一个带 OpenAI + DeepSeek 双引擎、ReAct 工具调用与 OTel 观测的 agent，并在交互式 chat 里体验引擎切换。

## 前置要求

- Go 1.25+
- 至少一个 LLM API key（OpenAI 或 DeepSeek，用于实际对话；不配置也能启动，只是调用时报凭证缺失）
- （可选）Docker + Jaeger，用于查看 OTel trace

## 1. 获取代码并构建

```bash
git clone https://github.com/tltre/gogent
cd gogent

# 构建框架 CLI
go build -o gogent.exe ./cmd/gogent
```

`gogent` 是框架的管理 CLI，包含 `run` / `serve` / `stop` / `list` / `status` / `doctor` / `logs` / `tool` / `sandbox` 等子命令。

## 2. 查看示例配置

```bash
gogent validate config/example.yaml   # 离线校验 YAML
gogent inspect config/example.yaml    # 展示解析后的配置
```

`config/example.yaml` 是一个完整可运行的示例，包含：

- **interface**：CLI 交互界面（chat REPL）
- **observability**：OTel 导出到本地 Jaeger（可选）
- **tools**：声明 calculator / think / todo 三个内置工具
- **components**：native provider（默认启用全部引擎）+ react agentcore

## 3. 配置 API key

启动前先把 key 配好，两种方式任选：

**方式 A — 直接写文件**（脚本/CI 友好）：

```bash
# Windows
$env:USERPROFILE\.gogent\apps\example-agent\credentials.yaml
# 内容：
# openai: "sk-你的OPENAIkey"
# deepseek: "sk-你的DEEPSEEKkey"
```

**方式 B — REPL 内交互配置**（推荐，见第 5 步 `/key` 命令），无需手动编辑文件。

## 4. 启动 agent

```bash
./gogent.exe run config/example.yaml
```

执行流程：

1. `run` 自动检测并启动 daemon（后台守护进程，管理 agent 生命周期与端口分配）
2. daemon 解析配置、分配端口、准备组件
3. agent 在当前进程内启动（前台模式），进入交互式 chat REPL

## 5. 第一个对话

启动后你会看到 banner 和 `> ` 提示符：

```
> 用一句话介绍你自己
（agent 使用默认引擎回复）

> 帮我计算 1234 * 5678
（模型调用 calculator 工具 → 结果回填 → 给出最终答案，这就是 ReAct 循环）
```

**切换引擎与模型**：

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

**配置凭证（未提前配 key 时）**：

```
> /key openai sk-你的OPENAIkey
Saved API key for openai

> /key
Provider credentials:
  openai     ****xxxx
  deepseek   (not configured)
```

`/key` 设置后**下一条消息立即生效**，无需重启。

## 6. 非交互式单次调用

框架 CLI 的 `gogent run <config>` 启动的是交互式 chat REPL。非交互单次调用由 **agent 自身的 CLI `run` 子命令**提供（`-p/--prompt` 指定提示词，`--provider`/`--model` 指定引擎），面向嵌入 Gogent 的应用或脚本化调用：

```
agent run -p "用一句话介绍你自己" --provider openai --model gpt-4o-mini
```

> 注：应用构建后其 CLI 默认进入 chat REPL；`run` 子命令供以子命令方式启动 agent 的场景使用（详见 [供应商接入](providers.md) 的"运行时切换"）。

## 7. （可选）查看 OTel trace

```bash
docker run --rm -d --name jaeger \
  -e COLLECTOR_OTLP_ENABLED=true \
  -p 16686:16686 -p 4317:4317 \
  jaegertracing/all-in-one:latest
```

打开 http://localhost:16686 → Search → 按 `service: example-agent` 搜索，可以看到：

```
agent.run                 ← ReAct 总流程
├── agent.llm.generate    ← 每次 LLM 调用
├── tool.exec             ← 每次工具调用（含鉴权/钩子/结果事件）
└── agent.llm.generate    ← 基于工具结果的最终推理
```

## 管理运行中的 agent

另开一个终端：

```bash
./gogent.exe list              # 列出所有 agent（NAME/PORT/PID/STATUS）
./gogent.exe status            # 组件状态表
./gogent.exe doctor            # 逐一健康检查
./gogent.exe logs              # 实时日志流
./gogent.exe stop example-agent # 停止 agent
```

## 下一步

- [配置参考](configuration.md) — 了解 YAML 每个字段的含义
- [供应商接入](providers.md) — 接入新 LLM 供应商、凭证管理、运行时切换
- [Agent 类型](agents.md) — react agent 的 ReAct 行为、上下文/记忆、工具调用
