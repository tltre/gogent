# 沙箱生态架构

> **文档状态**：本文档记录 Gogent 在 v0.14-v0.15 的架构演进与决策过程，包括方案对比、取舍理由与后续演进方向。代码与本文档如有出入，以代码为准。

本文档描述 Gogent 沙箱系统的整体架构：配置体系、与工具生态的联动关系、Provider 体系与 CLI 管理命令。

## 一、架构总览

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Daemon Process                                │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────────┐ │
│  │  ToolRegistry │  │  SandboxMgr  │  │  internal/credentials/     │ │
│  │  (工具注册表) │  │  (沙箱管理)   │  │  (统一凭证解析)            │ │
│  └──────┬───────┘  └──────┬───────┘  └────────────────────────────┘ │
│         │                │                                           │
│         ▼                ▼                                           │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │  HTTP Daemon API + gRPC ToolService                           │   │
│  │  ExecuteTool 管线: Auth → Hook:pre → Sandbox → Hook:post → Rst │   │
│  └──────────────────────────────────────────────────────────────┘   │
│         │                │                                           │
│         ▼                ▼                                           │
│  ┌──────────────┐  ┌───────────────────────────────────────────┐    │
│  │  BuiltinRunner│  │  Provider 体系                             │    │
│  │  (内置工具)   │  │  ┌──────────┐ ┌──────────┐ ┌──────────┐  │    │
│  │  calculator  │  │  │ E2B      │ │ Builtin  │ │ Agent-   │  │    │
│  │  think       │  │  │Provider  │ │ Provider │ │ Sandbox  │  │    │
│  │  todo        │  │  │ (MicroVM) │ │ (bwrap)  │ │ (K8s)    │  │    │
│  └──────────────┘  │  └──────────┘ └──────────┘ └──────────┘  │    │
│                     └───────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────┘
            │ gRPC / HTTP
            ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        App Process                                   │
│                                                                      │
│  ToolManager (通过 gRPC metadata 传递 app-name + sandbox 配置)       │
│  SandboxManager Client (gRPC 管理沙箱 CRUD + 执行)                   │
└─────────────────────────────────────────────────────────────────────┘
```

**核心设计决策**：沙箱在 **Daemon 侧**执行——安全边界不由"囚犯"（App 进程）自己管理，App 侧沙箱方案被否决。

## 二、分层配置体系

沙箱配置分为三层，存放于两个文件：

### 2.1 Daemon 侧：sandbox.yaml

```yaml
# ~/.gogent/sandbox.yaml

# 沙箱后端连接信息
sandbox-providers:
  my-e2b:
    type: e2b                          # "e2b" | "builtin" | "agent-sandbox"
    endpoint: "https://api.e2b.app"
    apiKey: "${E2B_API_KEY}"           # 从 credentials.yaml 或环境变量解析

# 安全模板（定义什么能做、什么不能做）
sandbox-profiles:
  restricted-shell:
    provider: my-e2b                   # 引用已声明的 provider
    template: "code-interpreter-v1"    # E2B template ID
    network: false                     # 无网络
    maxMemoryMB: 256
    allowCommands: [ls, cat, grep, echo, pwd]

  workspace:
    provider: my-e2b
    template: "code-interpreter-v1"
    network: true
    lifecycle:
      mode: persistent                 # 持久化工作区

# 工具默认 fallback（App 不指定 sandbox 时使用）
defaults:
  builtin:
    profile: restricted-shell           # 内置工具默认
  process:
    profile: restricted-shell           # process/http MCP 默认
```

### 2.2 App 侧：app.yaml

```yaml
name: my-agent
version: "0.1.0"

# 沙箱实例声明
sandboxes:
  workspace:
    profile: restricted-shell
    workDir: "/workspace/my-agent"
    allowedCommands: [ls, cat, grep, echo, pwd]

# App 级别默认（优先级高于 daemon defaults）
default:
  sandbox: workspace

# 工具声明 + 可选的 sandbox 路由
tools:
  - name: shell
    sandbox: workspace                  # 显式指定沙箱
    securityLevel: 2
  - name: net-curl
    sandbox: net-access
    securityLevel: 2
  - name: calculator
    securityLevel: 0                    # 不指定 → 查 App.default → Daemon.defaults
```

### 2.3 Daemon 侧：tools.yaml（不涉及沙箱）

`tools.yaml` 只负责 MCP Server 管理，不包含任何沙箱配置。

## 三、沙箱路由三层 Fallback

ExecuteTool 在决定是否使用沙箱时，按以下优先级查找：

```
① tools[].sandbox 显式指定  ──→ 直接使用
② App.default.sandbox       ──→ App 侧默认
③ Daemon defaults           ──→ 按工具 driver 分类 fallback
   ├─ builtin → defaults.builtin.profile
   └─ process → defaults.process.profile
```

**设计选择**：LLM 选工具 = 选沙箱，不引入参数级复杂度；不同工具名映射不同沙箱，LLM 无需感知沙箱概念。

## 四、Provider 体系

### 4.1 接口设计

```go
// SandboxProvider 是对接外部沙箱后端的统一接口。
type SandboxProvider interface {
    Name() string
    Type() SandboxType
    Create(ctx context.Context, profile *SandboxProfile, cfg *SandboxConfig) (ISandbox, error)
}

// ISandbox 是沙箱实例的执行接口。
type ISandbox interface {
    Execute(ctx context.Context, req ExecRequest) (ExecResult, error)
    Close(ctx context.Context) error
}

// 可选能力接口
type Refreshable interface {           // E2B 沙箱 TTL 续期
    Refresh(ctx context.Context) error
}
type SnapshotProvider interface {      // MicroVM 快照
    Snapshot(ctx context.Context, sb ISandbox) (string, error)
    CreateFromSnapshot(ctx context.Context, snapshotID string, cfg *SandboxConfig) (ISandbox, error)
}
```

**设计选择**：Provider 可选接口（Refreshable/SnapshotProvider）由后端自行实现，框架只要求核心 Create/Execute/Close。

### 4.2 E2BProvider（当前功能实现）

通过社区 Go SDK（`github.com/matiasinsaurralde/go-e2b`）对接 E2B-compatible REST API。

```go
type E2BProvider struct {
    client *e2b.Client  // Go SDK 客户端
}
```

| 能力 | 实现方式 |
|------|---------|
| 创建沙箱 | `client.NewSandbox(ctx, config)` → `POST /sandboxes` |
| 执行命令 | `sandbox.Commands.RunWithContext(ctx, "sh", ["-c", cmd])` → envd gRPC |
| TTL 续期 | `sandbox.SetTimeoutWithContext(ctx, ttl)` → `POST /sandboxes/{id}/refreshes` |
| 快照 | `sandbox.CreateSnapshot(ctx)` → `POST /sandboxes/{id}/snapshots` |
| 销毁 | `sandbox.CloseWithContext(ctx)` → `DELETE /sandboxes/{id}` |

### 4.3 BuiltinProvider（本地隔离）

| 平台 | 后端 | 依赖 |
|------|------|------|
| Linux | bubblewrap (bwrap) | `apt install bubblewrap` |
| macOS | sandbox-exec (Seatbelt) | 内置 |
| Windows | Docker fallback / stub | Docker Desktop |

## 五、生命周期管理

```
App 连接 Daemon (gRPC):
   ① RegisterManifest → 存储 tools + sandboxes + default 声明
   ② 不立即创建沙箱（懒初始化）

App 执行工具 (ExecuteTool):
   ① 查 sandboxName（三层 fallback）
   ② SandboxManager.GetOrCreateForApp()
   ③ Provider.Create() → 返回 ISandbox
   ④ ISandbox.Execute() → 返回 ExecResult

App 存活期间:
   Daemon 定时调用 SandboxManager.RefreshAll()
   → 遍历所有 Refreshable 沙箱 → 续期 TTL

App 正常断开:
   Daemon.StopApp() → 通知后清理
   → ManifestStore.Unregister(appName)
   → SandboxManager.DestroyAppSandboxes()

App crash / Daemon crash:
   → TTL 到期 → E2B 侧自动销毁（不留孤儿）
   → 下次 start 用快照恢复（如果有）
```

**设计选择**：懒初始化（首次工具执行才创建沙箱）；crash 兜底依赖 E2B TTL 自动销毁，不留孤儿实例。

## 六、与 Tool 生态的联动

### 6.1 路由关系

```
Tool 执行请求                沙箱路由
─────────────────           ─────────
shell(cmd="ls")      ──→   workspace 沙箱（E2B MicroVM）
filesystem.read      ──→   workspace 沙箱
net-curl             ──→   net-access 沙箱（有网络）
calculator, think    ──→   不走沙箱（Level 0）
```

### 6.2 sandbox-tool-map Metadata

App 侧 `tools[].sandbox` 的 per-tool 映射通过 gRPC metadata 传递：

```
Header: sandbox-tool-map
Value:  {"shell":"workspace","net-curl":"isolated-shell"}
```

Daemon 侧 `AppSandboxInfo.ToolSandboxMap` 存储此映射，ExecuteTool 时查表路由。

### 6.3 App 身份传递

App 侧 ToolManager 在 gRPC metadata 中注入：

```
app-name: my-agent
sandbox-configs: {"workspace":"restricted-shell"}
sandbox-default: workspace
sandbox-tool-map: {"shell":"workspace","net-curl":"isolated-shell"}
```

## 七、敏感配置管理

### 7.1 统一凭证解析

所有 `${VAR}` 引用通过 `internal/credentials/` 包统一解析：

```go
import "github.com/tltre/gogent/internal/credentials"

creds, _ := credentials.LoadCredentials(path)  // 读取 credentials.yaml
resolver := credentials.NewResolver(creds)
resolver.Resolve("${E2B_API_KEY}")             // → credentials.yaml → os.Getenv
```

解析顺序：`credentials.yaml` → `os.Getenv`。支持 fsnotify 热加载。

### 7.2 credentials.yaml

```yaml
# ~/.gogent/credentials.yaml (0600 权限)
E2B_API_KEY: "sk-..."
```

## 八、CLI 管理命令

### 8.1 命令树

```
gogent sandbox
  ├── provider
  │   ├── list                         列出所有 provider
  │   ├── add <name>                   新增 provider
  │   │   --type string                 类型（e2b, builtin）
  │   │   --endpoint string             API 地址
  │   │   --api-key string              API Key
  │   └── remove <name>                删除 provider（运行时生效）
  │
  ├── profile
  │   ├── list                         列出所有 profile
  │   ├── add <name>                   新增 profile
  │   │   --provider string             引用 provider 名
  │   │   --template string             沙箱模板 ID
  │   │   --network                     启用网络
  │   ├── edit <name>                  编辑 profile 字段
  │   │   --provider string             修改 provider
  │   │   --template string             修改模板
  │   │   --network                     修改网络
  │   └── remove <name>                删除 profile（运行时生效）
  │
  ├── defaults                         查看/设置默认 fallback
  │   --builtin profile                 内置工具默认 profile
  │   --process profile                 MCP 工具默认 profile
  │
  ├── edit                             打开编辑器编辑完整 sandbox.yaml
  │                                     保存退出后自动 reload
  │
  └── status                           查看沙箱系统概览
```

### 8.2 即时生效

所有 `add/edit/remove` 操作：

1. 调用 Daemon HTTP API，更新内存中的 ProviderRegistry / ProfileRegistry
2. 写 sandbox.yaml 文件持久化
3. 不需要 restart daemon

`edit` 命令保存退出后自动触发 `POST /api/v1/daemon/sandbox/reload`，重新解析 sandbox.yaml。

### 8.3 延迟校验

- `provider remove` 不检查被哪些 profile 引用（错误在运行时暴露）
- `profile remove` 不检查被哪些 App 引用
- 如果配置文件有错误，daemon 拒绝 reload，保持旧配置

**设计选择**：配置错误运行时暴露，CLI 不校验引用关系（延迟校验），保持 CLI 轻量。

## 九、测试体系

| 层级 | 位置 | 依赖 | 说明 |
|------|------|------|------|
| 单元测试 | `tests/native/sandbox/` | 无（mock Provider） | Registry、Manager、配置加载 |
| 集成测试 | `tests/native/sandbox/` | 无（mock gRPC Server） | ExecuteTool 路由、App 身份、断连清理 |
| 端到端测试 | `tests/e2e/sandbox_test.go` | E2B API Key | 真实 E2B 沙箱执行（需外部服务） |

## 十、设计决策记录

| # | 决策 | 理由 |
|---|------|------|
| 1 | 沙箱在 Daemon 侧执行 | 安全边界不由囚犯管理，App 侧沙箱被否决 |
| 2 | 声明式多沙箱 | LLM 选工具 = 选沙箱，不引入参数级复杂度 |
| 3 | 工具名路由 | 不同工具名映射不同沙箱，LLM 无需感知沙箱概念 |
| 4 | 分层配置 | Daemon 定安全边界（profiles），App 定工作区配置 |
| 5 | E2B 协议兼容 | 利用已有生态，通过 Adapter 接入不同后端 |
| 6 | Provider 可选接口 | Refreshable/SnapshotProvider 由后端自行实现 |
| 7 | 延迟校验 | 配置错误运行时暴露，CLI 不校验引用关系 |
| 8 | 统一凭证解析 | `internal/credentials/` 包，tool 和 sandbox 共享 |

## 参考

- 工具生态架构：[tool.md](./tool.md)
- 实现：`internal/daemon/sandbox/`（manager、provider、yaml）
