# 工具生态架构

> **文档状态**：本文档记录 Gogent 在 v0.14-v0.15 的架构演进与决策过程，包括方案对比、取舍理由与后续演进方向。代码与本文档如有出入，以代码为准。

本文档描述 Gogent 工具生态的整体架构：从配置管理到运行时调用的全链路（ToolRegistry、MCP server、执行管线、凭证管理、App 侧集成）。

## 一、架构总览

```
┌──────────────────────────────────────────────────────────────────┐
│                        Daemon Process                             │
│                                                                   │
│  ┌───────────────┐  ┌──────────────┐  ┌──────────────────────┐  │
│  │  ServerStore   │  │ ToolRegistry  │  │    McpRunner         │  │
│  │  (MCP 元信息)  │  │ (可调用工具)  │  │  (process+http)     │  │
│  │                │  │              │  │                      │  │
│  │  github-mcp    │  │ calculator   │  │  StdioMCPClient      │  │
│  │  {command,env} │  │ github-mcp.  │  │  StreamableHttpClient│  │
│  │                │  │   pull       │  │                      │  │
│  │  calculator    │  │ github-mcp.  │  │  LifecycleManager    │  │
│  │  {builtin}     │  │   push       │  │  (MCP Ping → 30s)   │  │
│  └───────────────┘  └──────────────┘  └──────────────────────┘  │
│           │                  │                    │              │
│           ▼                  ▼                    ▼              │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  gRPC ToolService (ExecuteTool / RegisterManifest)       │   │
│  │  HTTP Daemon API (list/status/register/unregister/restart)│   │
│  └──────────────────────────────────────────────────────────┘   │
└──────────────────────────────────────────────────────────────────┘
           │ gRPC
           ▼
┌──────────────────────────────────────────────────────────────────┐
│                        App Process                                │
│                                                                   │
│  ToolManager                                                     │
│    ├── RegisterManifest(name) → 展开 server 为子 tool              │
│    ├── cache → {github-mcp.pull, github-mcp.push, ...}            │
│    ├── ListByServer(name) → 按 server 查询                        │
│    ├── ListServers() → 列出所有 server                             │
│    ├── Execute("github-mcp.pull", params) → CallTool               │
│    └── HookManager → 处理 Daemon 的 HookInvocation                 │
└──────────────────────────────────────────────────────────────────┘
```

## 二、核心概念

### 2.1 MCP Server 粒度

tools.yaml 中配置的是 **MCP Server**（而不是单个工具）。一个 MCP Server（如 github-mcp）可能包含几十个工具（pull、push、list-issues……）。

```yaml
# ~/.gogent/tools.yaml
tools:
  my-custom-mcp:            # ← MCP Server
    driver: process
    command: "node mcp-server.js"
    level: 2
    env:
      API_KEY: "${MY_MCP_KEY}"
```

### 2.2 Tool 命名约定

| 类型 | 示例 | 说明 |
|------|------|------|
| builtin 单工具 | `calculator` | 全局唯一，无 Server 前缀 |
| MCP Server 子工具 | `my-custom-mcp.pull` | `<server-name>.<tool-name>` |
| 远程 HTTP MCP | `github-mcp.list-issues` | 同上 |

### 2.3 三层存储

| 存储 | 存什么 | 示例 |
|------|--------|------|
| **ServerStore** | MCP Server 元信息（command、env、status、mcp client） | `github-mcp: {command, status:ACTIVE, client}` |
| **ToolRegistry** | 可调用的工具条目 | `calculator`、`github-mcp.pull`、`github-mcp.push` |
| **ManifestStore** | App 注册的工具声明 | `{"my-agent": ["github-mcp"]}` |

## 三、初始化流程

```
Daemon.NewDaemon():
  ① NewServerStore()
  ② NewToolRegistry()
  ③ toolReg.LoadDefault(serverStore)
       ├── ServerStore: calculator, think, todo, filesystem.read, shell  (builtin server)
       └── ToolRegistry: calculator, think, todo, filesystem.read, shell  (可调用工具)
  ④ toolReg.LoadFromFile(resolver)
       └── 只处理 driver=process|http 的条目
           ├── ServerStore: my-custom-mcp, github-mcp  (MCP server 元信息)
           └── ToolRegistry: (不变——空缺，等 ListTools 填充)
  ⑤ NewHandler(registry, manifestStore, serverStore)
       └── 创建 McpRunner(serverStore, registry)
           ├── Runner["builtin"] → BuiltinRunner
           └── Runner["process"] + Runner["http"] → McpRunner (同一实例)
  ⑥ handler.StartAllServers()
       └── for each process|http server in ServerStore:
           ├── McpRunner.StartServer(name)
           │   ├── mcp-go client (Stdio / StreamableHTTP)
           │   ├── Initialize()
           │   ├── ListTools() → {pull, push, ...}
           │   └── ToolRegistry ← github-mcp.pull, github-mcp.push, ...
           └── ServerStore.status = ACTIVE
  ⑦ handler.StartLifecycle()
       └── goroutine: 每 30s MCP Ping → 健康/失败
```

**关键区别：**

| 来源 | ServerStore 条目 | ToolRegistry 条目 |
|------|----------------|-------------------|
| `LoadDefault()` (builtin) | `{calculator, driver:builtin}` | `calculator` |
| `LoadFromFile()` (process) | `{my-custom-mcp, command:...}` | (空——等 ListTools) |
| `StartAllServers()` | status → ACTIVE | `my-custom-mcp.pull`, `my-custom-mcp.push` |

## 四、Runner 体系

### 4.1 Runner 接口

```go
type Runner interface {
    Execute(ctx context.Context, def *ToolDefinition, params map[string]any) (Result, error)
}
```

### 4.2 三种 Driver 的实现

| Driver | Runner | 底层 | 说明 |
|--------|--------|------|------|
| `builtin` | BuiltinRunner | Go 函数调用 | calculator、think、todo 等进程内执行 |
| `process` | McpRunner | mcp-go StdioMCPClient | fork 子进程 + stdin/stdout JSON-RPC |
| `http` | McpRunner | mcp-go StreamableHTTPClient | HTTP POST + JSON-RPC |

**McpRunner 是统一的**——process 和 http 共享同一套代码路径。差异只在传输层创建：

```go
switch storeInfo.Driver {
case "process":
    mcpCl, _ = client.NewStdioMCPClient(command, env)
case "http":
    mcpCl, _ = client.NewStreamableHttpClient(endpoint)
}
```

## 五、Run 执行管线

### 5.1 ExecuteTool 消息流

```
v0.12.9 完整管线:

App                              Daemon
 │── ExecuteTool(stream) ──────→
 │── ToolExecuteRequest ───────→
 │  ← AuthResult(seq=1) ───────  ── 鉴权
 │  ← HookInvocation:pre(seq=2)  ── Pre-execute 钩子
 │── HookVerdict{approved} ────→
 │  ← HookInvocation:post(seq=3) ── Post-execute 钩子
 │── HookVerdict{output} ──────→  ── 可修改输出
 │  ← ToolResult(seq=4) ───────  ── 最终结果
 │  ← EOF ─────────────────────
```

### 5.2 执行路由（McpRunner）

```
ExecuteTool("github-mcp.pull", {owner:"foo"})
  → parseServerTool → server="github-mcp", tool="pull"
  → McpRunner.servers["github-mcp"] → client
  → mcpClient.CallTool("pull", {owner:"foo"})
  → mcpResultToResult → text content
```

## 六、生命周期管理

### 6.1 状态状态机

```
REGISTERED ──(StartServer)──→ ACTIVE
ACTIVE ──(Ping 失败)──→ UNHEALTHY ──(重启成功)──→ ACTIVE
UNHEALTHY ──(超 3 次)──→ STOPPED ──(restart)──→ ACTIVE
STOPPED ──(unregister)──→ REMOVED
```

### 6.2 健康检查

- 使用 mcp-go `MCPClient.Ping()`（协议级探活）
- process 和 http 共用同一段逻辑
- 每 30 秒循环
- 连续 3 次失败后标记 STOPPED
- 自动重启尝试（RestartServer = Close + StartServer）

### 6.3 手动恢复

```bash
gogent tool restart my-custom-mcp
  → 关闭旧 client
  → 重新 fork / 连接
  → 重新 ListTools → 注册子工具
  → 状态恢复 ACTIVE
```

## 七、CLI 命令体系

### 7.1 命令树

```
gogent
  └── tool
        ├── list                         列出所有 MCP Server（含 builtin）
        │     NAME              DRIVER   LEVEL   TOOLS
        │     calculator        builtin  0       1
        │     shell             builtin  2       1
        │     my-custom-mcp     process  2       12
        │
        ├── status <server>              查看 Server 详情（含 source/tools 数）
        ├── register [name]              编辑器模式（无参数）或 CLI flags 模式
        │     --driver, --command, --level, --env, --description
        ├── unregister <name>            注销（有 App 使用时拒绝，--force 覆盖）
        └── restart <server-name>        重启 STOPPED 的 MCP Server
```

### 7.2 编辑器注册模式

```bash
gogent tool register
  → 拉起 $EDITOR / code / notepad
  → 编辑 YAML 模板（tools: 列表）
  → 保存退出 → 逐个注册
  → 一个失败不影响其他
```

## 八、凭证管理

### 8.1 credentials.yaml

```yaml
# ~/.gogent/credentials.yaml (0600) — 不提交 git
my-mcp:
  API_KEY: "sk-abc..."
  TOKEN: "eyJ..."
```

### 8.2 双源解析

```go
// 优先级：credentials.yaml → os.Getenv
func (r *EnvResolver) Resolve(input string) string {
    return os.Expand(input, func(name string) string {
        if v, ok := r.creds[name]; ok { return v }
        return os.Getenv(name)
    })
}
```

### 8.3 凭证隔离

- HTTP GET `/api/v1/daemon/tools` 和 `/api/v1/daemon/tools/{name}` 返回 `Sanitized()` 结果（不含 env）
- gRPC `GetToolStatus` proto 不含 env 字段
- Registry 存储原始 `${VAR}` 引用，解析发生在 Runner 执行时

## 九、App 侧集成

### 9.1 config.yaml

```yaml
# app-a.yaml
tools:
  - name: github-mcp          # 声明 MCP server 名 → 展开子工具
    securityLevel: 1
  - name: calculator          # 声明 builtin 工具名（同样需要声明！）
    securityLevel: 0
```

**builtin 工具不会自动加载到 App**：daemon 持有的内置工具（calculator/think/todo 等）与 MCP server 一样，**必须由 App 显式声明才可用**。这是 v0.12.2 的声明式授权设计——`ManifestStore.IsAuthorized` 在执行时校验 App 是否声明了该工具，未声明即无权执行。App 只获得它声明的工具（最小权限）。

### 9.2 Manifest 展开

```
App RegisterManifest("github-mcp")
  → Daemon 查 ToolRegistry → 直接工具（builtin/子工具）直接接受
  → 查 ServerStore → MCP Server 名 → 展开子工具
  → 逐个校验并返回 ManifestResponse

App 侧 cache:
  [{name:"calculator"}, {name:"github-mcp.pull"}, {name:"github-mcp.push"}, ...]
```

**声明解析顺序**（daemon 侧 `RegisterManifest`）：

```
1. entry.Name 命中 ToolRegistry（builtin 或已注册子工具）→ 直接接受为 builtin
2. entry.Name 命中 ServerStore（MCP server）→ 展开为 <server>.<tool> 子工具
3. 都不命中 → 拒绝（tool not found）
```

**builtin 名优先于 MCP server 名**：若某个 MCP server 名与 builtin 工具同名，声明该名时 daemon 会当作 builtin 接受（第 1 分支先命中）。因此应避免 MCP server 使用 builtin 名。

### 9.3 命名冲突保护

**builtin 工具名是保留名**，MCP server（process/http）注册时**不能覆盖**：

| 注册路径 | 冲突行为 | 说明 |
|---------|---------|------|
| CLI/gRPC（`gogent tool register`） | 拒绝：`name "X" conflicts with builtin tool` | `ToolRegistry.Register` 重名拒绝 |
| tools.yaml 加载 | 拒绝：`cannot override builtin tool "X" with driver "Y"` | `RegisterOrUpdate` 保护 SourceBuiltin |
| ServerStore.Add | 拒绝：`server "X" already exists` | builtin 已注册进 ServerStore |

**原因**：若允许同名覆盖，`calculator` 会从 builtin 静默变成 MCP server——ToolRegistry 与 ServerStore 状态不一致，工具语义混乱，授权/路由不确定。builtin 名空间保留，杜绝冲突产生。

### 9.4 ToolManager 扩展方法

```go
tm.ListByServer("github-mcp")  → 属于该 server 的所有 tool
tm.ListServers()               → 所有 server 名（含 builtin）
tm.Execute("github-mcp.pull", {owner:"foo"})  → 调用
```

## 十、Hook 事件流

### 10.1 Pre/Post Execute 钩子

```
Auth → Hook:pre(blocking) → Run → Hook:post(blocking) → Result
              │                         │
              ▼                         ▼
       HookVerdict{approved}      HookVerdict{output}
```

| 阶段 | 可修改 | 阻断语义 |
|------|--------|---------|
| pre_execute | 调用参数（modified_params） | approved=false → 不执行 |
| post_execute | 输出（output 字段） | approved 不检查 |

### 10.2 HookVerdict Proto

```protobuf
message HookVerdict {
  string hook_id = 1;
  bool approved = 2;                                    // pre: 是否允许执行
  string reason = 3;                                     // pre: 阻断原因
  map<string, google.protobuf.Value> modified_params = 4; // pre: 修改调用参数
  google.protobuf.Value output = 5;                       // post: 修改后的输出
}
```

## 十一、后续演进方向

| 功能 | 说明 |
|------|------|
| 沙箱体系 | Daemon 统一管理的沙箱系统（容器 / MicroVM），替代 App 侧方案（已在 v0.13 落地，见 sandbox.md） |
| Shell + Filesystem 工具 | 依赖 sandbox 就绪后实现 |
| 工具日志 | 统一 OTel 可观测性方案（已在 v0.15 落地，见 observability.md） |
| 工具分组 | YAML `groups:` 语法糖，方便 App 快速引用一组工具 |

## 参考

- 沙箱生态架构：[sandbox.md](./sandbox.md)
- 可观测性设计：[observability.md](./observability.md)
- 实现：`internal/daemon/tool/`（registry、runner、lifecycle、grpc、yaml）
