# Daemon — App & Component 生命周期管理

> 适用版本：v0.11.x+
> 最后更新：2026-05-28

---

## 一、架构总览

Daemon 是一个后台进程，负责管理 agent application 及其组件的完整生命周期。

```
┌─ 用户 CLI ──────────────────────────────────────┐
│  gogent run config.yaml   ← 前台，终端交互         │
│  gogent serve config.yaml ← 后台，daemon 管理      │
│  gogent stop <name>       ← 停止 application      │
│  gogent list              ← 列出所有 application   │
│  gogent doctor            ← 逐组件健康检查          │
│  gogent restart <name>    ← 重启 application       │
└────────────────────────────────────────────────────┘
        │
        ▼ HTTP API (:9090)
┌─ Daemon 进程 ───────────────────────────────────┐
│  AppStore        map[string]*AppInfo             │
│  ComponentStore  map[string]*ComponentInfo       │
│  PortAllocator   端口分配器                        │
│  StartHealthCheck    App/组件级健康检查 goroutine  │
└────────────────────────────────────────────────────┘
        │
        ├─ os.StartProcess → 组件子进程（stubcomponent / gogent component）
        │     每个组件一个进程（provider、memory、agentcore 等）
        │     通过 gRPC 通信，daemon 通过 gRPC health 探测存活
        │
        └─ os.StartProcess → agent 子进程（仅 gogent serve）
              Application 进程：Builder → Initialize → Start → mgmt HTTP → 阻塞等信号
```

---

## 二、两套启动路径

### 2.1 gogent run（in-process）

```
用户终端 → gogent run config.yaml
  │
  ├─ EnsureDaemon() → 启动/检测 daemon（detachDaemon 隔离信号）
  │
  ├─ LoadApp(configPath, needForkApplication=false)
  │     └─ 解析 YAML → fork 组件（process-driver）→ 分配 agent 端口
  │        → 注册 AppInfo(PID=0, status="running")
  │        → 返回 { name, port, env: [GOGENT_PROVIDER_TARGET=...] }
  │
  ├─ 设 env vars → forkAgent(configPath, port, foreground=true)
  │     └─ Builder.Build → Initialize → Start
  │        → mgmt.Listen → WriteAppPortFile（写入真实 PID）
  │        → iface.Run() → CLI REPL（终端 stdin/stdout）
  │
  ├─ 退出：daemonClient.StopApp(name)
  │     └─ 读 port file 拿真实 PID → kill（可能失败）→ 清理组件 → Unregister
  │
  └─ 异常退出（crash / taskkill）：
        runHealthCheck → isProcessAlive(false) → UpdateStatus("stopped")
        → cleanupAppComponents → 不再重复打印
```

**特征**：agent 跑在 gogent run 当前进程内，CLI 直接接管终端 stdin。
daemon 知道该 app 但 PID=0（首次注册时），真实 PID 由 port file 回写。

### 2.2 gogent serve（daemon-forked）

```
用户终端 → gogent serve config.yaml
  │
  ├─ EnsureDaemon()
  └─ LoadApp(configPath, needForkApplication=true)
        └─ 解析 YAML → fork 组件 → 分配端口 → fork agent 子进程
           → 等 agent mgmt HTTP 就绪
           → 注册 AppInfo(PID=真实, status="running")
           → 返回 { name, port, pid }
```

**特征**：agent 是 daemon fork 的独立子进程，没有终端交互。
前端 `gogent serve` 调用后即退出，agent 在后台运行。

### 2.3 核心差异

| 维度 | gogent run | gogent serve |
|------|-----------|-------------|
| agent 位置 | gogent run 进程内 | daemon fork 子进程 |
| PID 来源 | LoadApp 时 0，port file 回写 | LoadApp 时 real PID |
| stdin | 终端键盘 | NUL（无终端） |
| iface | forAgent(true)→有 CLI | forAgent(false)→SetInterface(nil) |
| Ctrl+C 影响 | daemon 隔离不动（detachDaemon） | 同左 |
| 退出方式 | StopApp（正常）/ 健康检查（异常） | 同左 |

---

## 三、App 生命周期

### 3.1 状态机

```
           LoadApp(false) / LoadApp(true)
                  │
                  ▼
            ┌──────────┐
            │  running  │
            └────┬─────┘
                 │
       ┌─────────┼──────────┐
       │         │          │
       ▼         ▼          ▼
    StopApp   taskkill    SIGTERM
       │         │          │
       ▼         ▼          ▼
  cleanupApp   runHealthCheck  signal handler
  +Unregister  → UpdateStatus  → srv.GracefulStop
               ("stopped")      → unregister / cleanup
```

### 3.2 LoadApp（入口）

```go
func (d *Daemon) LoadApp(configPath string, needForkApplication bool) (*AppInfo, error)
```

`needForkApplication` 控制是否 fork agent。

| 值 | 行为 | 调用者 |
|----|------|--------|
| true（默认） | fork 组件 + fork agent + 健康检查 | gogent serve / gogent restart |
| false | fork 组件 + 分配端口，不 fork agent | gogent run |

返回的 `AppInfo` 在 `needForkApplication=false` 时额外包含 `Env []string`，
即组件 target 的环境变量（`GOGENT_PROVIDER_TARGET=localhost:xxxx`），
调用方需 `os.Setenv` 后自己 Build agent。

### 3.3 StopApp（正常退出）

```
StopApp(name)
  ├─ store.Get(name)
  ├─ ReadAppPortFile(name) → 拿真实 PID（port file 中有 agent 写入的 PID）
  │
  ├─ if PID > 0:
  │     killProcess(PID) → 可能失败（进程已自退出）
  │     wait 5s → 也可能失败 → 不阻塞，继续清理
  │
  ├─ cleanupAppComponents → 释放组件端口、杀组件进程、删 port file
  ├─ store.Unregister(name)
  └─ RemoveAppPortFile(name)
```

**关键特性**：kill 失败不阻塞后续清理。PID=0（in-process agent）或进程已死时，
`killProcess` 由 `pid <= 0` 守卫放行，直接进组件清理。

### 3.4 异常退出（crash / taskkill）

异常退出时 `StopApp` 不会被调用。由 `runHealthCheck` 兜底：

```
runHealthCheck（每 10s）
  ├─ 遍历 AppStore
  ├─ PID ≤ 0 → ReadAppPortFile → UpdatePID（port file 回写）
  ├─ isProcessAlive(PID) == false:
  │     if status != "stopped"（首次检测到）:
  │       打印一次 "[daemon] app ... stopped"
  │       UpdateStatus("stopped")
  │       RemoveAppPortFile
  │       cleanupAppComponents
  │     if status == "stopped"（后续轮次）:
  │       跳过（不重复打印，不重复清理）
  └─ isProcessAlive(PID) == true:
        tryHealthEndpoint(Port) ← best-effort ping
```

**防止日志风暴**：首次检测到死亡后标记 `status=stopped`，后续轮次跳过。
不再重复 `"[daemon] app ... stopped (pid ... no longer alive)"`。

`gogent list` 仍显示 `stopped` 记录，供管理参考。
可用 `gogent stop <name>` 彻底清除（Unregister）。

---

## 四、组件生命周期

### 4.1 组件分类

| 类型 | 所属 | daemon 管理 | 说明 |
|------|------|------------|------|
| singleton | provider, memory, contextmanager, agentcore | 是 | 全局唯一，跨 app 共享 |
| tool | tool | 是 | 特殊：重命名为 tool-service |
| appLocal | hook, channel, eventbus, logger, sandbox | 否 | 进程内 native 默认实现 |

### 4.2 组件 Fork

组件 fork 在 `LoadApp` 内的 `forkComp` 闭包中完成：

```go
forkComp(compName, compType) → (target string, error)
  ├─ compByName[compName] → 找到 YAML 配置
  ├─ 非 process-driver → 注册到 ComponentStore（不 fork）
  ├─ process-driver:
  │     ├─ allocatePort()
  │     ├─ config.command 为空 → os.Executable() + "component" 子命令
  │     └─ config.command 非空 → os.StartProcess(command, args)
  │
  ├─ waitForComponentHealth(target, 10s) → gRPC health check
  ├─ Register ComponentInfo(Name, PID, Target, Status="running")
  └─ WriteComponentPortFile(appName, compName, port, pid)
```

### 4.3 组件清理

两种路径触发组件清理：

| 路径 | 触发条件 | 清理内容 |
|------|---------|---------|
| StopApp | 正常退出 / gogent stop | 遍历 info.Components → kill 进程 → releasePort → 删 port file → Unregister |
| cleanupAppComponents | 健康检查发现 app 死亡 | 同上，由 runHealthCheck 调用 |

```go
cleanupAppComponents(appName)
  ├─ store.Get(appName)
  ├─ 遍历 info.Components:
  │     compStore.RemoveApp(compName, appName)
  │     if remaining == 0:
  │         if DriverProcess && PID>0: killProcess → releasePort → RemoveComponentPortFile
  │         compStore.Unregister(compName)
  └─ fallback: compStore.ListByApp(appName)（无 bidirectional map 时）
```

---

## 五、进程管理

### 5.1 killProcess — 平台差异

| 平台 | 实现 | PID≤0 行为 |
|------|------|-----------|
| Windows | `taskkill /PID <pid> /F` | `pid <= 0` 时 return nil（不执行） |
| Unix | `syscall.Kill(pid, SIGTERM)` | 同上 |

`pid <= 0` 守卫是必要的——PID 0 在 Windows 上是 System Idle Process，
在 Unix 上是当前进程组，kill 0 的行为不可预测。

### 5.2 isProcessAlive

| 平台 | 实现 | PID≤0 返回 |
|------|------|-----------|
| Windows | `tasklist /FI "PID eq <pid>"` | `pid <= 0` 时 return false |
| Unix | `syscall.Kill(pid, 0) == nil` | 同上 |

### 5.3 Port File 回写 PID

in-process agent（gogent run）注册时 PID=0，但 `App.Run()` 中
`mgmt.Listen` 会调用 `WriteAppPortFile` 写入真实 PID。

`runHealthCheck` 通过 `ReadAppPortFile(name)` 读取，再调用
`store.UpdatePID(name, realPid)` 更新 AppStore。

`StopApp` 也通过 `ReadAppPortFile` 获取真实 PID 用于 kill。

### 5.4 detachDaemon — 信号隔离

`startDaemonBackground` 中调用 `detachDaemon(cmd)`，将 daemon 子进程放入新进程组，
防止 Ctrl+C 从父进程传播到 daemon：

```
Ctrl+C → Console → gogent run（收到 → 退出）
                   daemon（隔离 → 不受影响，继续运行）
```

| 平台 | 实现 |
|------|------|
| Windows | `CreationFlags \|= CREATE_NEW_PROCESS_GROUP` |
| Unix | `Setpgid = true` |

---

## 六、已知限制与后续工作

| 限制 | 说明 |
|------|------|
| `gogent run` 异常退出后 `list` 仍显示 stopped | 需要用 `stop` 清除，未来可增加超时自动 Unregister |
| `gogent restart` 总是 fork agent | 不保留原 in-process 模式，重启用 daemon-forked |
| `LoadApp` 端口分配后被抢 | 分配端口后调用方不及时 listen 导致冲突，未来由 daemon 先 listen 占住再返回 |
| 组件清理 taskkill 128 | 进程已死时 taskkill 报 128，当前静默忽略 |
| tool 组件名必须为工具-service | 与用户自定义名冲突，待 daemon 内部分离注册名和查找名 |
