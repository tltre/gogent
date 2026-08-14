# =============================================================================
# Provider 全链路手动验证（层 3）
# 需要真实 API key + 交互验证，由用户执行。
# =============================================================================
#
# 前置：构建二进制
#   go build -o gogent.exe ./cmd/gogent
#
# 步骤 1：写示例配置
#   已提供 config/provider-verify.yaml（见下）
#
# 步骤 2：配置凭证（两种方式任选）
#   方式 A（推荐，交互式）：进入 chat 后用 /key 命令配置
#   方式 B（脚本/CI）：直接编辑 ~/.gogent/apps/verify-agent/credentials.yaml：
#     openai: "sk-你的 OPENAI key"
#     deepseek: "sk-你的 DEEPSEEK key"
#   （文件权限建议 0600；不配置的供应商会自动降级跳过）
#
# 步骤 3a：非交互 run（单次调用 + provider/model 指定）
#   ./gogent.exe run config/provider-verify.yaml -p "用一句话介绍你自己" `
#       --provider openai --model gpt-4o-mini
#   期望输出：真实 OpenAI 回复（gpt-4o-mini）
#
#   ./gogent.exe run config/provider-verify.yaml -p "你好" `
#       --provider deepseek --model deepseek-chat
#   期望输出：真实 DeepSeek 回复
#
# 步骤 3b：交互 chat（验证 /provider /model 切换）
#   ./gogent.exe run config/provider-verify.yaml -i
#   （若框架 CLI 无 -i，则进入 agent 后执行 chat 子命令）
#
#   在 REPL 中依次验证：
#   > /key openai sk-你的OPENAIkey
#     期望：Saved API key for openai（交互配置，无需编辑文件）
#   > /key deepseek sk-你的DEEPSEEKkey
#     期望：Saved API key for deepseek
#   > /key
#     期望：列出 openai + deepseek，密钥脱敏
#   > /provider
#     期望：列出 openai + deepseek，标注当前
#   > /provider deepseek
#     期望：Switched to deepseek (model: deepseek-chat)
#   > /model deepseek-reasoner
#     期望：Switched model to deepseek-reasoner
#   > /model
#     期望：Current model: deepseek-reasoner
#   > 你好
#     期望：使用 deepseek-reasoner 的真实回复
#   > 我叫小明，请记住
#     期望：模型记住上下文（v0.15.x 会话历史，ContextManager 持久化）
#   > 我叫什么名字？
#     期望：回答"小明"——证明跨轮次历史已注入（BuildInput 加载历史）
#   > /provider openai
#     期望：切回 openai，model 重置为默认
#   > 你好
#     期望：使用刚 /key 配置的 openai key（无需重启）
#   > quit
#
# ReAct 工具调用验证（可选，需 app 声明 tools）：
#   在 provider-verify.yaml 的 components 中加 tools 声明（daemon 注册），
#   然后 chat 中问需要工具的问题（如计算/查询）：
#   > 帮我计算 1234*5678
#     期望：模型调用工具（若 daemon 有 calculator 等）→ 回填结果 → 最终回答
#   （工具执行在 daemon 侧；观察 agent 日志确认 tool 调用与回填）
#
# 验证通过标准：
#   1. /key 交互配置后消息立即使用新 key（无需重启/编辑文件）
#   2. run 命令返回所选 provider/model 的真实回复
#   3. chat 中 /provider /model 切换生效，消息使用所选引擎
#   4. 跨轮次对话：模型记住上下文（历史经 ContextManager 持久化并加载）
#   5. ReAct 工具调用：模型声明工具 → 执行 → 结果回填 → 最终回答
#   6. 无 key 的供应商在 /provider 列表中出现但调用报错（凭证缺失）
# =============================================================================
