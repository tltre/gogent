# 供应商接入

Gogent 内置多引擎 LLM Provider，通过统一的 `IProvider` 接口提供 OpenAI、DeepSeek 及所有 OpenAI 兼容服务（Groq、Mistral、Ollama、vLLM 等 10+ 家）的连接能力。本文说明如何配置、接入凭证并在运行时切换。

## 内置引擎

| 引擎名 | 品牌 | API key 环境变量 | 说明 |
|--------|------|------------------|------|
| `openai` | OpenAI | `OPENAI_API_KEY` | 完整实现：Generate/Stream/SSE/工具调用/模型列表 |
| `deepseek` | DeepSeek | `DEEPSEEK_API_KEY` | OpenAI 兼容薄包装，内置 baseURL |
| openai-compat | （基类） | 各家自定义 | 不独立注册，供接入新兼容厂商复用 |

- **模型列表动态拉取**：每个引擎从 `GET {baseURL}/models` 拉取可用模型（10 分钟缓存），**永不硬编码模型名**
- **默认模型**：解析链为 配置/环境变量 → `/models` 第一个 → 引擎动态解析

## 接入方式

### 1. YAML 声明（推荐）

native provider 组件默认启用**全部**内置引擎，用 `exclude` 黑名单排除不需要的：

```yaml
components:
  - name: "provider-main"
    type: "provider"
    driver: "native"
    config:
      exclude: []            # 留空 = 全部启用；例如 exclude: ["deepseek"]
```

声明后 ProviderManager 注册为默认 provider，REPL 中即可 `/provider` 查看与切换。

### 2. 代码注入（App 开发者）

```go
builder, _ := app.NewBuilder("config.yaml")

application, err := builder.Build(
    app.WithProvider("openai", provider.NewOpenAI(provider.OpenAIConfig{
        Model: "gpt-4o",            // 可选；空 = 动态解析
        Timeout: 60 * time.Second,  // 可选；默认 60s
    })),
    app.WithProvider("deepseek", provider.NewDeepSeek(provider.DeepSeekConfig{})),
)
```

### 3. 接入新的 OpenAI 兼容厂商

用 `NewOpenAICompat(baseURL)` 基类创建一个薄引擎（注册进引擎注册表）：

```go
// 以 groq 为例（自定义文件 groq.go）
func init() {
    _ = provider.RegisterEngine("groq", func() (provider.IProvider, error) {
        return provider.NewOpenAICompat("https://api.groq.com/openai/v1"), nil
    })
}
```

注册后即自动出现在 `/provider` 列表中，可用 `exclude` 黑名单控制启停。引擎名同时作为 CredentialStore 的 key。

## 凭证管理

### 凭证存储位置

| 用途 | 路径 | 说明 |
|------|------|------|
| Provider API key | `~/.gogent/apps/<app-name>/credentials.yaml` | **app 作用域**，与 daemon 级凭证隔离 |
| 工具/沙箱密钥 | `~/.gogent/credentials.yaml` | daemon 级，供 MCP server、E2B 等使用 |

Provider 凭证文件格式（精确键，与引擎名一致）：

```yaml
# ~/.gogent/apps/example-agent/credentials.yaml
openai: "sk-..."
deepseek: "sk-..."
```

### 交互式配置（/key 命令）

在 chat REPL 中：

```
> /key                        # 列出全部引擎凭证状态（密钥脱敏）
> /key openai sk-xxx          # 设置 openai 的 key（写入 app 作用域文件，立即生效）
> /key openai                 # 查看单个引擎状态
> /key deepseek --delete      # 删除凭证
```

设置后**下一条消息即用新 key**，无需重启。

### 解析优先级

1. CredentialStore（`/key` 写入的 app 作用域文件）
2. 构造时环境变量（`OPENAI_API_KEY` / `DEEPSEEK_API_KEY`）

### 替换凭证实现

默认 `FileCredentialStore` 可替换为任何实现（Vault、keyring 等）：

```go
builder.Build(app.WithCredentialStore(myStore))
```

## 运行时切换

### chat REPL（会话级）

```
> /provider                   # 列出所有引擎 + 当前选择
> /provider deepseek          # 切换引擎（自动切到其默认模型）
> /model deepseek-reasoner    # 切换当前引擎的模型
> /model                      # 查看当前模型
```

### run 命令（单次调用）

agent 的 CLI 提供 `run` 子命令做单次非交互调用：

```
agent run -p "你好" --provider openai --model gpt-4o-mini
```

未指定 provider/model 时使用会话当前选择；模型名会校验是否在该引擎的可用列表中。框架 CLI 的 `gogent run <config>` 启动的是交互式 chat REPL（会话级切换），与这里的单次调用粒度不同。

## 常见问题

| 现象 | 原因 | 解决 |
|------|------|------|
| `/provider` 列表空 | 无 native provider 组件或全部被 exclude | 检查 YAML `components` 中的 provider 条目 |
| 调用报 "no api key for X" | 该引擎未配置凭证 | `/key X sk-...` 或写 credentials.yaml |
| 模型列表为空 | 未配置 key，`/models` 拉取失败 | 先配 key，10 分钟内自动重试 |
| 想用不在列表中的模型 | 模型校验拦截 | 确认该模型在厂商 `/models` 返回中 |
