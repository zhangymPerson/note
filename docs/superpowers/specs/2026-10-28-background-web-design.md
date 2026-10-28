# 设计：`note web` 后台运行（start / stop / status）与日志文件

日期：2026-10-28 · 状态：待批准 · 覆盖 PRD v1.5 §8.9 / §8.10、TECH_DESIGN v1.5 §4.1 / §4.2 / §4.3 / §4.6 / §14.1 中「`note web` 只能前台运行、日志只进 stdout」的结论

## 1. 意图与为什么

现状：`note web` 是纯前台进程（`cmd/note/main.go:runWeb`：`net.Listen` → `Serve` → `signal.Notify` 等 Ctrl+C），`slog` 默认写 stderr。用户日常开着本地笔记服务时终端被占住，关掉终端服务就没了，日志也随终端滚走。

目标形态：`note web start` 后台拉起、`note web stop` 优雅停止、`note web status` 看运行情况；后台实例的日志落到**与 SQLite 同一个目录**（数据目录），路径在 `note info` 里可见。不带子命令的 `note web` 保持前台语义不变（开发时看着日志跑）。

## 2. 目标 / 非目标 / 反目标

**目标**

1. `note web`（无子命令）行为不变：前台、Ctrl+C 退出、日志进 stderr。
2. `note web start [原有参数]` 后台启动：等待就绪后打印地址、pid、日志路径、停止方法，然后父进程退出；子进程脱离终端与控制台继续服务。
3. `note web stop` 优雅停止后台实例：走 `POST /api/shutdown`（状态文件里的随机令牌），等进程退出；超时或请求失败时退化为强制结束并说明后果。
4. `note web status` 输出人类可读的运行情况：运行中 / 进程在但不响应 / 未运行，附 pid、地址、启动时间、已运行时长、日志路径与大小、数据库路径、版本。
5. 日志文件 `<数据目录>/note.log`（= `filepath.Dir(cfg.DBPath)`），追加写；`note info` 的数据段新增「日志」一行（路径 + 大小）。
6. 状态文件 `<数据目录>/note.json`（`0600`，原子写）：`pid` / `addr` / `urls` / `started_at` / `db` / `log` / `token` / `version`。
7. 交互菜单的「启动 Web 服务」改为后台启动，并在菜单退出前打印地址与日志路径。

**非目标**

- 日志轮转、压缩、按天分文件（单用户本地，`.log` 一直追加；需要时手动删）。
- 开机自启 / 注册为系统服务（systemd unit、launchd plist、计划任务）。二进制提供 start/stop/status，包装成服务是使用者的事。
- 同一数据目录的多实例管理（两个实例用不同端口开同一个库仍可发生，属既有行为）。
- 远程 stop、跨机器控制、把运行状态做进前端页面。
- 状态文件加密（`0600` + 同用户威胁模型足够）。

**反目标**（技术上跑通也算失败的形态）

- 令牌出现在日志、响应体、错误信息、命令行参数或仓库里的任何文件。
- `/api/shutdown` 绕过 `hostGuard`，或需要 CSRF cookie 才能用（那样浏览器页面也能触发）。
- 动了 `note web` 的前台语义（开发流程依赖它）。
- `start` 之后父进程挂住等子进程（`scripts/dev.ps1` 当初就是被继承的管道句柄坑过，用 WMI 绕的）。
- Windows 上 `stop` 只能 TerminateProcess，每次停止都跳过 WAL checkpoint。

## 3. 约束（来自现有代码）

- 配置解析 `config.Load` 用 `flag.FlagSet` 且**拒绝位置参数**（`unexpected argument`）——`start/stop/status` 必须在 `internal/cli.Parse` 层剥掉，不能传给 flag。
- `csrfProtect` 对全部非 GET 的 `/api/*` 强制双提交校验，关停请求没有 CSRF cookie → 必须显式豁免该路径。
- `hostGuard` 只允许 `127.0.0.1` / `localhost` / `::1`（`allow_remote` 时加监听 host）→ 控制请求按 `127.0.0.1:<端口>` 发起，端口取 `cfg.Addr` 的端口（监听通配地址时同样成立）。
- `slog` 的默认 handler 在进程启动时就绑定了当时的 `os.Stderr`，事后改 `os.Stderr` 变量**不生效** → 后台子进程的日志不能靠「运行期改 slog 输出」实现，必须由父进程把 `cmd.Stdout` / `cmd.Stderr` 指向日志文件（fd 级重定向，子进程零改动）。
- `store.Open` 会 `MkdirAll(dir, 0700)`，但父进程打开日志文件在 `store.Open` 之前 → 父进程自己 `MkdirAll`。
- `GET /api/info` 已返回 `addr` / `started_at` / `uptime_seconds` / `db_path` / `build_label` → `status` 直接复用，`/api/info` 的响应字段不变。
- `config.StartupURLs(请求地址, listener 实际地址)` 已能推导可点击地址 → `start` 打印复用同一套（父进程拿不到 listener，改用子进程写进状态文件的 `urls`，子进程用同一函数算）。
- Windows 无 fork：只能自我 re-exec；Unix 用 `Setsid`，Windows 用 `DETACHED_PROCESS|CREATE_NEW_PROCESS_GROUP` + `HideWindow`（平台分文件 + build tags）。可执行文件路径取 `os.Executable()`（`go run` 拉到临时二进制时也能工作，子进程在父进程退出前就已完成 exec）。

## 4. 方案（已在对话中拍板）

### 4.1 后台化：自我 re-exec（唯一可行路径）

`note web start` = 父进程 spawn 一个自己的副本 + 环境变量 `NOTE_DAEMON_STATE=<状态文件路径>`（同时充当「我是后台子进程」的标记），然后轮询就绪、打印、退出。子进程 argv 由「全局 `-c` + `web` + 原样透传的参数」组成，父进程不给子进程任何令牌或实现细节。

- Unix（`spawn_unix.go`）：`SysProcAttr{Setsid: true}`。
- Windows（`spawn_windows.go`）：`SysProcAttr{HideWindow: true, CreationFlags: DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP}`。
- 两侧都把 `cmd.Stdout`、`cmd.Stderr` 指向 `<数据目录>/note.log`（`O_APPEND|O_CREATE|O_WRONLY`, `0600`），因此访问日志、启动横幅、`fmt.Println` 的 URL、panic 栈全部落在同一个文件里，`cmd/note` 无需改任何日志相关代码。
- 没有采用的其他路子：OS 服务管理器（与 start/stop/status 需求重复，且要求使用者懂三种平台的服务机制）；强行在进程内 `fork`（Go 运行时不允许）。

### 4.2 停止通道：业务同端口 + 状态文件令牌（A1）

`POST /api/shutdown`：

- 仅后台模式启用：`web.NewWithControl(cfg, st, dist, web.Control{Token, Shutdown})`，`New` 保持原签名并转发空 `Control`。令牌为空时端点返回 `403 shutdown_disabled`。
- 请求头 `X-Shutdown-Token`，与令牌做 `subtle.ConstantTimeCompare`；缺失/不匹配 → `403 shutdown_denied`（文案「关停令牌无效」），不消耗任何次数、不做区分。
- 命中后先写 `202 {"status":"shutting_down"}`，再 `go ctl.Shutdown()`；`Shutdown` 在 `cmd/note` 里是 `sync.Once` 包裹的 `close(stopCh)`，与信号处理共用同一个 select。
- `csrfProtect` 对 `/api/shutdown` 显式放行（新增常量 `shutdownPath`）；`hostGuard` 照旧生效；日志只记 `slog.Info("shutdown requested", "remote", r.RemoteAddr)`。
- 令牌 32 字节 `crypto/rand` → base64url，**由子进程自己生成**并写进状态文件。父进程不生成、不传递令牌（命令行/env 里都没有），`stop`/`status` 从状态文件读。

### 4.3 运行状态判定（B1）

**控制地址推导**（我的判断）：监听地址可能是通配符，`0.0.0.0` / 空 host 连不上、`::` 要连 `::1`，所以新增纯函数 `daemon.ControlAddr(addr)`：通配 IPv4 → `127.0.0.1:<port>`，通配 IPv6 → `[::1]:<port>`，其余原样。`stop` 与 `status` 一律经它发请求（`status` 优先用状态文件里的 `addr`，无状态文件时用 `cfg.Addr`），单测覆盖五种形态。`stop`/`status` 的 flag 只影响「没有状态文件时用哪个地址探测」，不改变已有实例。

`note web status` 先做 HTTP 探测（`GET /api/info`，不需要令牌），再与状态文件合并判定：

| 探测 | 状态文件      | 结论                                       | 退出码 |
| ---- | ------------- | ------------------------------------------ | ------ |
| 200  | pid 存活      | 运行中（后台）                             | 0      |
| 200  | 无 / pid 已死 | 运行中（前台或状态文件缺失），顺手清理残留 | 0      |
| 失败 | pid 存活      | 进程存在但不响应（附 pid 与日志路径）      | 1      |
| 失败 | 无 / pid 已死 | 未运行（顺手清理残留）                     | 3      |

进程存活判定按平台分文件：Unix `syscall.Kill(pid, 0)`（`EPERM` 也算活）；Windows `os.FindProcess(pid)` 出错即不存在（不引入 `golang.org/x/sys` 直接依赖）。

**`stop` 打到前台实例**（我的判断）：没有有效状态文件却探测到有 `note` 在响应 → 打印「检测到运行中的实例（前台启动或状态文件缺失），`note web stop` 只能关停后台实例，请回到它的终端按 Ctrl+C」，退出码 1。不做「猜 pid 然后杀」——那是拿用户的编辑器进程玩俄罗斯轮盘。

**强制结束**（我的判断）：优雅请求之后 10 秒仍未退出（或请求本身失败且 pid 仍存活）→ Unix `SIGKILL`、Windows `TerminateProcess`，打印「已强制结束，WAL 未完成 checkpoint（数据安全，但下次启动会重放日志）」，随后清理状态文件。

## 5. 落地结构

```text
internal/daemon/          新增包：状态文件、日志路径、探测、spawn、pid 存活
├── state.go              State{...} / Load / Save 原子写 / StatePath / LogPath / NewToken
├── probe.go              Probe(addr) → ServerInfo；RequestStop(state) → error
├── pid_unix.go           processAlive(pid)
├── pid_windows.go        processAlive(pid)
├── spawn_unix.go         spawn(exe, args, logPath) → *exec.Cmd
└── spawn_windows.go
internal/web/            Control 注入 + /api/shutdown handler + CSRF 豁免
internal/cli/            WebAction 解析、usage、FormatStatus
cmd/note/                runWebStart / runWebStop / runWebStatus，菜单项接后台启动
```

### 5.1 CLI 面

```text
note web                       前台（不变）
note web start [原有参数]      后台启动；已在运行则打印地址后退出 0
note web stop                  优雅停止；未运行则提示退出 0
note web status                运行情况；未运行退出 3
```

`internal/cli` 增加 `WebAction`（`WebForeground` / `WebStart` / `WebStop` / `WebStatus`）与 `Options.WebAction`。`note web` 后的第一个非 flag token 若为 `start|stop|status` 则该值生效，其余参数原样留在 `Options.Args` 交给 `config.Load`；若首 token 以 `-` 开头照旧；若既不是 flag 也不是已知动作 → 报 `无法识别的 web 子命令 <x>（可用: start, stop, status）` 并以退出码 2 结束。`note web status -h` 走 `help web`。

### 5.2 子进程侧（后台模式）

`runWeb` 在 `loadConfig` 之后检查 `NOTE_DAEMON_STATE`：

1. 生成令牌与 `State{Pid: os.Getpid(), Addr: cfg.Addr, DB: cfg.DBPath, Log: LogPath(cfg.DBPath), StartedAt: now UTC, Version: build.Describe().Label()}`；
2. `net.Listen` 成功后补上**实际**端口（`ln.Addr()`）与 `config.StartupURLs(cfg.Addr, ln.Addr().String())` 算出的 `urls`，原子写状态文件；
3. 把 `Control{Token, Shutdown: requestStop}` 传给 `web.NewWithControl`；
4. 退出路径（信号、`/api/shutdown`、服务错误）统一走原有的优雅关停：`server.Shutdown(10s)` → `store.Close()`（含 WAL checkpoint）→ 删除状态文件（仅当文件里的 pid 仍等于自己，避免删掉新实例的文件）。

Linux/Windows 之外的平台不做特殊处理，`SETSID` 不可用时（如部分受限环境）spawn 失败会原样报错。

### 5.3 日志与 `note info`

- 路径纯函数 `daemon.LogPath(dbPath) = filepath.Join(filepath.Dir(dbPath), "note.log")`；状态文件同目录 `note.json`。
- 前台模式不建日志文件（保持「日志进终端」不变）；只有 `start` 的父进程创建/追加它。
- `cli.InfoData` 增加 `LogPath` / `LogExists` / `LogSizeBytes`，`FormatInfo` 数据段新增「日志」行：
  - 存在：`…/note.log（1.2 MB）`
  - 不存在：`…/note.log（未创建）`

### 5.4 `status` 输出样式

```text
note web · 运行中
  pid       12345
  地址      http://127.0.0.1:8080
  启动时间  2026-10-28T09:12:33Z（已运行 3 小时 12 分）
  日志      …/note.log（1.2 MB）
  数据库    …/notes.db
  版本      1.5.0 (418917b)
```

```text
note web · 未运行
  状态文件  …/note.json（不存在）
  日志      …/note.log（1.2 MB）
  数据库    …/notes.db
```

对齐用现有 `uniseg` 宽度算法（复用 `internal/cli/info.go` 里的 `displayWidth` / `padRight`）。

## 6. 安全不变量（新增）

- 令牌只在状态文件（`0600`）里，不进日志、不进响应、不进命令行、不进环境变量、不进任何浏览器存储。
- `/api/shutdown` 只在后台模式存在，需 32 字节令牌，仍受 `hostGuard` 约束；无 CSRF 依赖是**有意**的（凭证即令牌），实现时不得把它扩成「所有写请求免 CSRF」。
- 日志新增内容为零敏感字段：不记令牌、不记 `RemoteAddr` 之外的东西。
- 状态文件记录的路径（db/log）不是秘密，与 `note info` 输出一致。

## 7. 测试计划（TDD：先 RED 后 GREEN）

| 层                                                 | 用例                                                                                                                                                                                                                                        |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/daemon/state_test.go`                    | Save/Load round-trip；`0600` 权限；临时文件 + rename 原子替换（旧文件被覆盖）；损坏 JSON 报错；`StatePath`/`LogPath` 推导                                                                                                                   |
| `internal/daemon/probe_test.go`                    | `httptest` 起假服务：`Probe` 解析 `addr`/`started_at`/`build_label`；非 200 返回错误；`RequestStop` 的路径、方法、`X-Shutdown-Token` 头与错误状态映射                                                                                       |
| `internal/daemon/pid_test.go`                      | 自身 pid 存活；不存在的 pid 判死                                                                                                                                                                                                            |
| `internal/web/shutdown_api_test.go`                | 正确令牌 → 202 且回调被调用一次；错误/缺失令牌 → 403；未启用 → 403 `shutdown_disabled`；GET → 405；不带 CSRF cookie 也能过；Host 非法仍被 `hostGuard` 拦下                                                                                  |
| `internal/cli/parse_test.go`                       | `web start                                                                                                                                                                                                                                  | stop | status` 识别；动作后的 flag 透传；`web status -h` → help；未知动作报错；`web --addr` 不受影响 |
| `internal/cli/status_test.go`                      | `FormatStatus` 三种形态的渲染、uptime 文案、大小文案、CJK 对齐                                                                                                                                                                              |
| 真实二进制冒烟（记录进 CHANGELOG，不进 `go test`） | `just build` → `note web start --addr 127.0.0.1:8099`（确认返回、状态文件与日志生成）→ `status`（0）→ `info`（有日志行）→ `stop`（进程消失、状态文件删除、日志有 `shutting down`）→ `status`（3）→ 前台 `note web` 仍正常、关停端点返回 403 |

改动合入前必须 `just test` 与 `just check` 全绿（仓库硬性约定）。

## 8. 文档同步清单

- PRD：§8.9（命令与运行形态）、§8.10（日志文件位置与内容）、§8.11（备份需含 `note.log` / `note.json` 说明，状态文件可删）、AC 新增三到四条、版本号与修订说明。
- TECH_DESIGN：§4.1 命令表、§4.2（新增「日志与状态文件位置」）、§4.3 路由表 + 关停端点安全说明、§4.4/中间件（CSRF 豁免一行）、§4.6 日志、§14.1 运行与迁移指南、新增 ADR（自我 re-exec 后台化；同端口令牌关停；状态文件单一 JSON）、里程碑。
- AGENTS.md：部署形态补后台形态；硬性约定补「关停令牌不落日志、`/api/shutdown` 仅后台模式存在」。
- CHANGELOG `[Unreleased]`：按 Added / Changed / Security 记录实现过程与关键决策。

## 9. 留给实现者的判断（本设计不钉死）

- 就绪轮询的间隔与总超时（建议 100ms / 10s）、失败时打印日志尾部行数（建议 20 行）；`start` 失败时的退出码（建议 1）。
- `status` 里 uptime 的中文文案细节（`X 小时 Y 分` / `X 分 Y 秒`）。
- `internal/daemon` 内部函数是否导出为更细的探针接口以便测试（只要 `httptest` 能覆盖即可）。
- 是否额外打印「日志尾部」到终端之外的辅助命令（如 `note web logs`）——本设计不做，若实现时觉得必要需重新过设计。
