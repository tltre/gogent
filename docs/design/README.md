# 设计文档（Architecture Decision Records）

本文档目录记录 Gogent 的架构演进与决策过程，供**贡献者与深入了解实现细节的读者**阅读。

> 使用者（怎么配、怎么用、能做什么）请见 [docs/ 使用者指南](../quickstart.md)。

## 定位

设计文档回答"**为什么这么设计**"：方案对比、取舍理由、约束条件与后续演进方向。它们不是使用教程——使用层面的内容（YAML 怎么写、命令怎么敲）在 docs/ 顶层的使用者指南中。

## 文档列表

| 文档 | 主题 | 关联版本 |
|------|------|---------|
| [provider.md](provider.md) | Provider 模块：多供应商引擎、ProviderManager 统一管理、凭证体系 | v0.14.x |
| [agent.md](agent.md) | Agent Core：ReactAgent 的 ReAct 循环、上下文/记忆架构 | v0.15.x |
| [observability.md](observability.md) | 可观测性：OTel 插桩、span 结构、多进程导出方案 | v0.15.x |
| [daemon.md](daemon.md) | Daemon：多应用生命周期管理、进程隔离、崩溃恢复 | v0.11.x+ |
| [tool.md](tool.md) | 工具生态：ToolRegistry、MCP server、执行管线 | v0.12.x |
| [sandbox.md](sandbox.md) | 沙箱生态：E2B provider、分层配置、工具路由 | v0.13.x |

## 阅读建议

- **想理解整体架构**：先读 [daemon.md](daemon.md)（进程模型）→ [tool.md](tool.md)（工具链路）→ [agent.md](agent.md)（agent 行为）
- **想接入新 LLM 供应商**：读 [provider.md](provider.md) 的引擎注册与凭证部分
- **想部署到生产环境**：读 [observability.md](observability.md)（观测）与 [sandbox.md](sandbox.md)（隔离）

## 文档状态约定

设计文档记录的是**当时的设计决策**。代码演进过程中可能出现文档与实现不一致的情况——**以代码为准**。每篇文档顶部均有状态说明，标注其覆盖的版本区间与决策要点。
