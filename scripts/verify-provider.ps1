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
# 步骤 2：写 app 作用域凭证文件
#   mkdir -p ~/.gogent/apps/verify-agent
#   编辑 ~/.gogent/apps/verify-agent/credentials.yaml：
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
#   > /provider openai
#     期望：切回 openai，model 重置为默认
#   > quit
#
# 验证通过标准：
#   1. run 命令返回所选 provider/model 的真实回复
#   2. chat 中 /provider /model 切换生效，消息使用所选引擎
#   3. 无 key 的供应商在 /provider 列表中出现但调用报错（凭证缺失）
# =============================================================================
