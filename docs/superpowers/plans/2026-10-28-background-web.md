# `note web` 后台运行（start / stop / status）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `note web start / stop / status` 管理一个脱离终端的后台实例，日志与状态文件落在 SQLite 同目录，`note info` 输出日志路径，`note web`（无子命令）保持前台不变。

**Architecture:** 新增 `internal/daemon` 包承担状态文件、日志路径、控制地址推导、HTTP 探测与平台相关的 spawn/pid 存活；后台化靠「自我 re-exec + 平台 SysProcAttr」实现，父进程把子进程 stdout/stderr 指向日志文件；停止走业务同端口的 `POST /api/shutdown`，凭状态文件里的 32 字节令牌（`internal/web` 注入 `Control`，CSRF 对该路径显式放行）；`status` 先 HTTP 探测 `GET /api/info` 再与状态文件合并判定。

**Tech Stack:** Go 1.26（标准库 `net/http`、`os/exec`、`log/slog`、`crypto/rand`、`encoding/json`），Svelte 前端不涉及。无新增第三方依赖。

**Spec:** `docs/superpowers/specs/2026-10-28-background-web-design.md`

## Global Constraints

- 时间戳统一 UTC 秒级文本 `2006-01-02T15:04:05Z`；状态文件里的 `started_at` 用它。
- 日志不得包含正文、密码、密文、盐、Cookie、完整查询串；新增内容不得包含**关停令牌**。
- `/api/shutdown` 仅后台模式存在（`Control.Token` 为空时 `403 shutdown_disabled`），仍受 `hostGuard`；CSRF 豁免只针对这一条路径，不得扩成「所有写请求免 CSRF」。
- 状态文件 `note.json`、日志文件 `note.log` 都在 `filepath.Dir(cfg.DBPath)`；状态文件权限 `0600`，目录 `0700`，写入必须原子（同目录临时文件 + `rename`）。
- 配置文件写入保持原有原子 + YAML 严格解析行为，本计划不改 `config` 包。
- Windows 构建必须可用：平台相关代码放 `_unix.go` / `_windows.go`（build tag `!windows` / `windows`），不得引入 `golang.org/x/sys` 直接依赖。
- 前端零改动（不改 `web/src`）。
- 每个任务结束跑 `go test ./...` 与 `gofmt`；全部完成后必须 `just test` 与 `just check` 通过，且按仓库约定把实现过程写进 `CHANGELOG.md` 的 `[Unreleased]`。
- 中文注释、中文用户可见文案（与现有代码一致）；提交信息沿用 `feat:` / `fix:` / `docs:` 前缀。

## Review Focus

- **端口已被占用时 `note web start`**：用户看到的不该是一句 `listen tcp ... address already in use` 就没了，而应是失败原因 + 日志尾部，且不留下状态文件。
- **状态文件损坏或写了一半**（上次被强杀）：`status` / `stop` 必须明确报错并给出处理办法，不能 panic，也不能把它当成「未运行」静默删掉（那会掩盖仍在跑的实例）。
- **数据目录不存在或不可写**（首次 `start`）：要么建好，要么给出可读错误，绝不能留下半截状态文件或 0 字节日志。
- **通配监听（`0.0.0.0` / `::`）下的 `stop`/`status` 连通性**：连 `0.0.0.0` 在某些系统上会「成功」到随便哪个监听者，必须改连回环地址。
- **前台实例还在跑、状态文件却是残留**：`status` 要说「运行中（前台）」并清理残留，`stop` 要拒绝而不是去杀一个猜出来的 pid。

---

### Task 1: `internal/daemon` 状态文件与路径推导

**Files:**
- Create: `internal/daemon/state.go`
- Test: `internal/daemon/state_test.go`

**Interfaces:**
- Produces:
  - `type State struct { Pid int; Addr string; URLs []string; StartedAt string; DB string; Log string; Token string; Version string }`（JSON 键依次为 `pid` / `addr` / `urls` / `started_at` / `db` / `log` / `token` / `version`）
  - `func StatePath(dbPath string) string`、`func LogPath(dbPath string) string`（均为 `filepath.Join(filepath.Dir(dbPath), "note.json" | "note.log")`）
  - `func NewToken() (string, error)`（32 字节 `crypto/rand` → `base64.RawURLEncoding`）
  - `func Load(path string) (State, error)`（文件不存在时返回 `os.ErrNotExist`，`errors.Is` 可判）
  - `func Save(path string, st State) error`（`MkdirAll(dir, 0o700)` + 临时文件 `0o600` + `rename`）
  - `func RemoveIfPid(path string, pid int) error`（文件里的 `pid` 等于入参才删；文件不存在返回 nil）

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestStatePathsShareDatabaseDirectory(t *testing.T) {
	db := filepath.Join("C:", "users", "me", "note", "notes.db")
	if got, want := StatePath(db), filepath.Join("C:", "users", "me", "note", "note.json"); got != want {
		t.Fatalf("StatePath = %q, want %q", got, want)
	}
	if got, want := LogPath(db), filepath.Join("C:", "users", "me", "note", "note.log"); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}

func TestSaveLoadRoundTripWithTightPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "note.json")
	token, err := NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	want := State{
		Pid: 4242, Addr: "127.0.0.1:8080", URLs: []string{"http://127.0.0.1:8080"},
		StartedAt: "2026-10-28T09:12:33Z", DB: filepath.Join(dir, "notes.db"),
		Log: filepath.Join(dir, "note.log"), Token: token, Version: "1.5.0",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Pid != want.Pid || got.Addr != want.Addr || got.Token != want.Token ||
		got.StartedAt != want.StartedAt || got.Version != want.Version || len(got.URLs) != 1 {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("状态文件权限 = %o，令牌可被其他用户读取", perm)
	}
}

func TestSaveFailsWhenTargetIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if err := Save(filepath.Join(blocker, "note.json"), State{Pid: 1}); err == nil {
		t.Fatal("父路径是普通文件时应报错，而不是静默成功")
	}
}

func TestSaveReplacesExistingFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.json")
	if err := Save(path, State{Pid: 1, Token: "a"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := Save(path, State{Pid: 2, Token: "b"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil || got.Pid != 2 || got.Token != "b" {
		t.Fatalf("load = %+v, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("目录里残留临时文件: %v", entries)
	}
}

func TestLoadMissingFileIsNotExist(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "note.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestLoadCorruptFileReportsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.json")
	if err := os.WriteFile(path, []byte(`{"pid": 12`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want 损坏文件的解析错误", err)
	}
}

func TestRemoveIfPidOnlyRemovesMatchingPid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.json")
	if err := Save(path, State{Pid: 7}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := RemoveIfPid(path, 8); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("pid 不匹配时不应删除: %v", err)
	}
	if err := RemoveIfPid(path, 7); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid 匹配时应删除，err = %v", err)
	}
	if err := RemoveIfPid(path, 7); err != nil {
		t.Fatalf("文件不存在时应返回 nil: %v", err)
	}
}

func TestNewTokenIs32BytesBase64URL(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token = %q (len %d), want 32 字节 base64url", token, len(raw))
	}
	other, err := NewToken()
	if err != nil || other == token {
		t.Fatalf("两次生成的令牌相同或出错: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run 'TestState|TestSave|TestLoad|TestRemove|TestNewToken' -v`
Expected: FAIL，`undefined: State` / `undefined: Save` 等编译错误

- [ ] **Step 3: Implement `internal/daemon/state.go`**

按接口块写 `State`、`StatePath`、`LogPath`、`NewToken`、`Load`、`Save`、`RemoveIfPid`。`Save` 照抄 `internal/config/config.go:SaveUserName` 的「`CreateTemp` + `Chmod(0o600)` + `Rename`」骨架（`defer os.Remove(tmpName)` 兜底）；`Load` 用 `os.ReadFile` + `json.Unmarshal`，错误包一层 `fmt.Errorf("read daemon state %s: %w", path, err)`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/state.go internal/daemon/state_test.go
git commit -m "feat(daemon): 状态文件与日志路径推导"
```

---

### Task 2: 控制地址推导 `ControlAddr`

**Files:**
- Create: `internal/daemon/control.go`
- Test: `internal/daemon/control_test.go`

**Interfaces:**
- Produces: `func ControlAddr(addr string) string` —— `stop`/`status`/`Probe` 一律经它把监听地址转成可连接的请求地址。

- [ ] **Step 1: Write the failing test**

```go
package daemon

import "testing"

func TestControlAddrMapsWildcardsToLoopback(t *testing.T) {
	cases := []struct{ in, want string }{
		{"127.0.0.1:8080", "127.0.0.1:8080"},
		{"localhost:8080", "localhost:8080"},
		{"[::1]:8080", "[::1]:8080"},
		{"0.0.0.0:8080", "127.0.0.1:8080"},
		{":8080", "127.0.0.1:8080"},
		{"[::]:8080", "[::1]:8080"},
		{"192.168.1.5:8080", "192.168.1.5:8080"},
		{"garbage", "garbage"},
	}
	for _, tc := range cases {
		if got := ControlAddr(tc.in); got != tc.want {
			t.Errorf("ControlAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestControlAddr -v`
Expected: FAIL，`undefined: ControlAddr`

- [ ] **Step 3: Implement `internal/daemon/control.go`**

`net.SplitHostPort`；host 为 `""` 或 `"0.0.0.0"` → `net.JoinHostPort("127.0.0.1", port)`；host 为 `"::"` → `net.JoinHostPort("::1", port)`；解析失败原样返回入参（调用方随后报错）。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestControlAddr -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/control.go internal/daemon/control_test.go
git commit -m "feat(daemon): 控制地址推导，通配监听到回环"
```

---

### Task 3: 进程存活判定（平台分文件）

**Files:**
- Create: `internal/daemon/pid_unix.go`（`//go:build !windows`）、`internal/daemon/pid_windows.go`（`//go:build windows`）
- Test: `internal/daemon/pid_test.go`

**Interfaces:**
- Produces: `func ProcessAlive(pid int) bool`（Unix `syscall.Kill(pid, 0)`，`EPERM` 视为存活；Windows `os.FindProcess` 出错即判死）。

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"os"
	"testing"
)

func TestProcessAliveForSelf(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Fatalf("自身 pid %d 被判为不存在", os.Getpid())
	}
}

func TestProcessAliveForImpossiblePid(t *testing.T) {
	if ProcessAlive(1 << 30) {
		t.Fatal("pid 1073741824 被判为存活")
	}
}

func TestProcessAliveForZeroOrNegative(t *testing.T) {
	if ProcessAlive(0) || ProcessAlive(-1) {
		t.Fatal("0 / 负数 pid 不应判为存活")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestProcessAlive -v`
Expected: FAIL，`undefined: ProcessAlive`

- [ ] **Step 3: Implement both platform files**

`pid_unix.go`：`if pid <= 0 { return false }`；`err := syscall.Kill(pid, 0)`；`err == nil` 或 `errors.Is(err, syscall.EPERM)` → true。`pid_windows.go`：`if pid <= 0 { return false }`；`_, err := os.FindProcess(pid); return err == nil`（文档注明 Windows 上 `FindProcess` 对不存在的进程返回错误）。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run TestProcessAlive -v` 且 `GOOS=windows go build ./...`
Expected: PASS（当前平台）+ Windows 构建无错误

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/pid_*.go internal/daemon/pid_test.go
git commit -m "feat(daemon): 平台相关的进程存活判定"
```

---

### Task 4: HTTP 探测与关停请求

**Files:**
- Create: `internal/daemon/probe.go`
- Test: `internal/daemon/probe_test.go`

**Interfaces:**
- Produces:
  - `type ServerInfo struct { Addr string; StartedAt string; UptimeSeconds int64; Version string; DBPath string }`
  - `func Probe(addr string) (ServerInfo, error)` —— `GET http://<ControlAddr(addr)>/api/info`，客户端超时 2s，非 200 返回错误。
  - `func RequestStop(st State) error` —— `POST http://<ControlAddr(st.Addr)>/api/shutdown`，带 `X-Shutdown-Token: st.Token`、`Content-Type: application/json`；403 返回错误「关停令牌无效」，其他非 202 也返回错误。
  - `const ShutdownTokenHeader = "X-Shutdown-Token"`
- Consumes: Task 1 的 `State`、Task 2 的 `ControlAddr`。

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newFakeServer 起一个只回 /api/info 与 /api/shutdown 的假服务，
// 记录收到的关停请求，返回其地址与记录通道。
func newFakeServer(t *testing.T, token string) (addr string, gotToken chan string, shutdownCalls chan struct{}) {
	t.Helper()
	gotToken = make(chan string, 1)
	shutdownCalls = make(chan struct{}, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"addr": "127.0.0.1:8080", "started_at": "2026-10-28T09:12:33Z",
			"uptime_seconds": 187, "build_label": "1.5.0 (418917b)", "db_path": "/data/notes.db",
		})
	})
	mux.HandleFunc("POST /api/shutdown", func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotToken <- r.Header.Get(ShutdownTokenHeader):
		default:
		}
		if r.Header.Get(ShutdownTokenHeader) != token {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		select {
		case shutdownCalls <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String(), gotToken, shutdownCalls
}

func TestProbeReturnsServerInfo(t *testing.T) {
	addr, _, _ := newFakeServer(t, "tok")
	info, err := Probe(addr)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if info.StartedAt != "2026-10-28T09:12:33Z" || info.UptimeSeconds != 187 ||
		info.Version != "1.5.0 (418917b)" || info.DBPath != "/data/notes.db" || info.Addr != "127.0.0.1:8080" {
		t.Fatalf("info = %+v", info)
	}
}

func TestProbeFailsWhenNothingListens(t *testing.T) {
	if _, err := Probe("127.0.0.1:1"); err == nil {
		t.Fatal("没有服务在监听时应返回错误")
	}
}

func TestRequestStopSendsTokenAndSucceeds(t *testing.T) {
	addr, gotToken, calls := newFakeServer(t, "secret-token")
	if err := RequestStop(State{Addr: addr, Token: "secret-token"}); err != nil {
		t.Fatalf("request stop: %v", err)
	}
	if got := <-gotToken; got != "secret-token" {
		t.Fatalf("服务端收到令牌 %q", got)
	}
	select {
	case <-calls:
	default:
		t.Fatal("关停回调未被触发")
	}
}

func TestRequestStopRejectsWrongToken(t *testing.T) {
	addr, _, _ := newFakeServer(t, "secret-token")
	err := RequestStop(State{Addr: addr, Token: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "关停令牌无效") {
		t.Fatalf("err = %v, want 关停令牌无效", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run 'TestProbe|TestRequestStop' -v`
Expected: FAIL，`undefined: Probe` / `undefined: RequestStop`

- [ ] **Step 3: Implement `internal/daemon/probe.go`**

`Probe`：`http.Client{Timeout: 2 * time.Second}`，`http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ControlAddr(addr)+"/api/info", nil)`，`defer resp.Body.Close()`，非 200 → `fmt.Errorf("probe %s: HTTP %d", addr, resp.StatusCode)`，JSON 解码到 `ServerInfo`（字段用 `json:"build_label"` → `Version`）。`RequestStop` 同上骨架，POST 空 body，按 `resp.StatusCode` 映射 202 成功 / 403「关停令牌无效」/ 其他 `HTTP %d`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/probe.go internal/daemon/probe_test.go
git commit -m "feat(daemon): 探测运行实例与请求关停"
```

---

### Task 5: 脱离终端的子进程启动 `RunDetached`

**Files:**
- Create: `internal/daemon/spawn_unix.go`、`internal/daemon/spawn_windows.go`
- Test: `internal/daemon/spawn_test.go`

**Interfaces:**
- Produces:
  - `const EnvStatePath = "NOTE_DAEMON_STATE"`
  - `func RunDetached(exe string, args []string, logPath, statePath string) (*exec.Cmd, error)` —— 打开日志（`O_APPEND|O_CREATE|O_WRONLY`, `0o600`）作为子进程 stdout/stderr，`Env = append(os.Environ(), EnvStatePath+"="+statePath)`，平台 `SysProcAttr`，`cmd.Start()` 成功后关闭父进程侧的日志句柄并返回 `cmd`。
- Consumes: 无。

- [ ] **Step 1: Write the failing test**

```go
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDetachedChild 是子进程侧：RunDetached 会注入 NOTE_DAEMON_STATE，
// 本用例只在被注入时输出标记，否则跳过，避免父测试进程误判。
func TestDetachedChild(t *testing.T) {
	state := os.Getenv(EnvStatePath)
	if state == "" {
		t.Skip("不是 RunDetached 拉起的子进程")
	}
	os.Stdout.WriteString("child-ready state=" + state + "\n")
}

func TestRunDetachedWritesChildOutputToLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "note.log")
	statePath := filepath.Join(dir, "note.json")

	cmd, err := RunDetached(os.Args[0], []string{"-test.run=TestDetachedChild"}, logPath, statePath)
	if err != nil {
		t.Fatalf("run detached: %v", err)
	}
	if cmd.Process == nil || cmd.Process.Pid <= 0 {
		t.Fatal("子进程没有 pid")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		data, err := os.ReadFile(logPath)
		if err == nil && strings.Contains(string(data), "child-ready state="+statePath) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("日志里没有子进程输出（err=%v, 内容=%q）", err, string(data))
		}
		time.Sleep(20 * time.Millisecond)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/ -run TestRunDetached -v`
Expected: FAIL，`undefined: RunDetached`

- [ ] **Step 3: Implement both platform files**

共同部分写在 `spawn_unix.go` 里会被 windows 文件重复——改为把公共体放 `spawn.go`、平台差异放两个 `sysProcAttr()` 函数：`spawn.go` 定义 `RunDetached` 与 `func sysProcAttr() *syscall.SysProcAttr`（未定义由平台文件提供）。`spawn_unix.go`：`&syscall.SysProcAttr{Setsid: true}`；`spawn_windows.go`：`&syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200}`（`DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`），并注释说明两者不可与 `CREATE_NEW_CONSOLE` 组合。`RunDetached` 里 `defer` 不关句柄的时机要注意：**必须在 `cmd.Start()` 之后关**，否则子进程拿不到有效的继承句柄。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/daemon/ -run 'TestRunDetached|TestDetachedChild' -v` 且 `GOOS=windows go build ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/spawn*.go internal/daemon/spawn_test.go
git commit -m "feat(daemon): 脱离终端的子进程启动"
```

---

### Task 6: `/api/shutdown` 与 `Control` 注入

**Files:**
- Create: `internal/web/control.go`
- Modify: `internal/web/server.go`（`New` → 转发 `NewWithControl`；mux 注册 `POST /api/shutdown`）
- Modify: `internal/web/middleware.go`（`csrfProtect` 放行 `shutdownPath`）
- Modify: `internal/web/server_test.go`（`newTestHandler` 拆出 `newTestHandlerWithControl`）
- Test: `internal/web/shutdown_api_test.go`

**Interfaces:**
- Produces:
  - `type Control struct { Token string; Shutdown func() }`、`func NewWithControl(cfg config.Config, st *store.Store, dist fs.FS, ctl Control) http.Handler`
  - `const shutdownPath = "/api/shutdown"`、`const shutdownTokenHeader = "X-Shutdown-Token"`
- Consumes: 现有 `newTestHandler` / `doRequest` 测试助手（`internal/web/server_test.go`）。

- [ ] **Step 1: Write the failing test**

先把 `internal/web/server_test.go` 的 `newTestHandler` 拆出带 `Control` 的版本（既有调用点零改动）：

```go
func newTestHandler(t *testing.T) (http.Handler, config.Config) {
	t.Helper()
	return newTestHandlerWithControl(t, Control{})
}

func newTestHandlerWithControl(t *testing.T, ctl Control) (http.Handler, config.Config) {
	t.Helper()
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>note</title>")},
		"assets/app.js": {Data: []byte("console.log('ok')")},
	}
	dir := t.TempDir()
	cfg := config.Config{
		Addr:       "127.0.0.1:8080",
		DBPath:     filepath.Join(dir, "notes.db"),
		ConfigPath: filepath.Join(dir, "note.yaml"),
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return NewWithControl(cfg, st, dist, ctl), cfg
}
```

然后 `internal/web/shutdown_api_test.go`：

```go
package web

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func newControlHandler(t *testing.T, token string) (http.Handler, *int32) {
	t.Helper()
	var calls int32
	handler, _ := newTestHandlerWithControl(t, Control{
		Token:    token,
		Shutdown: func() { atomic.AddInt32(&calls, 1) },
	})
	return handler, &calls
}

func TestShutdownAcceptsValidTokenWithoutCSRFCookie(t *testing.T) {
	handler, calls := newControlHandler(t, "secret")
	req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set(shutdownTokenHeader, "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("关停回调调用 %d 次，want 1", atomic.LoadInt32(calls))
	}
}

func TestShutdownRejectsWrongOrMissingToken(t *testing.T) {
	handler, calls := newControlHandler(t, "secret")
	for _, token := range []string{"", "wrong"} {
		req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
		req.Host = "127.0.0.1:8080"
		if token != "" {
			req.Header.Set(shutdownTokenHeader, token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("token %q: expected 403, got %d", token, rec.Code)
		}
	}
	if atomic.LoadInt32(calls) != 0 {
		t.Fatal("令牌无效时不应触发关停")
	}
}

func TestShutdownDisabledWithoutControl(t *testing.T) {
	handler, _ := newTestHandler(t) // 无 Control：等价于前台模式
	req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
	req.Host = "127.0.0.1:8080"
	req.Header.Set(shutdownTokenHeader, "anything")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	if body := errorBody(t, rec); body.Error.Code != "shutdown_disabled" {
		t.Fatalf("error code = %q, want shutdown_disabled", body.Error.Code)
	}
}

func TestShutdownRejectsNonPostAndForeignHost(t *testing.T) {
	handler, _ := newControlHandler(t, "secret")
	get := doRequest(handler, http.MethodGet, shutdownPath, "127.0.0.1:8080", "")
	if get.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: expected 405, got %d", get.Code)
	}
	req := httptest.NewRequest(http.MethodPost, shutdownPath, nil)
	req.Host = "evil.example:8080"
	req.Header.Set(shutdownTokenHeader, "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("外站 Host: expected 400 host_invalid, got %d", rec.Code)
	}
}
```


- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestShutdown -v`
Expected: FAIL，`undefined: NewWithControl` / `undefined: Control`

- [ ] **Step 3: Implement**

`internal/web/control.go`：`Control`、两个常量、`func (app *application) handleShutdown(w, r)`——`app.control.Token == ""` → `writeError(w, 403, "shutdown_disabled", "后台模式未启用关停接口")`；`subtle.ConstantTimeCompare([]byte(r.Header.Get(shutdownTokenHeader)), []byte(app.control.Token)) != 1` → `writeError(w, 403, "shutdown_denied", "关停令牌无效")`；否则 `slog.Info("shutdown requested", "remote", r.RemoteAddr)`、`writeJSON(w, 202, map[string]string{"status": "shutting_down"})`、`go app.control.Shutdown()`（`Shutdown == nil` 时只记 `slog.Warn`）。`application` 增加 `control Control` 字段（`handlers_settings.go:application`），`newApplication` 增加 `ctl Control` 参数。`server.go`：`New` 改一行转发 `NewWithControl(cfg, st, dist, Control{})`；`NewWithControl` 里 `newApplication(cfg, st, ctl)` 并注册 `mux.HandleFunc("POST "+shutdownPath, app.handleShutdown)`。`middleware.go:csrfProtect` 在非 GET 分支最前面加 `if r.URL.Path == shutdownPath { next.ServeHTTP(w, r); return }`，并在注释里写明「凭证即令牌，别把这条豁免扩大」。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/web/ -v`
Expected: PASS（含既有 100+ 项测试不回归）

- [ ] **Step 5: Commit**

```bash
git add internal/web/control.go internal/web/server.go internal/web/middleware.go internal/web/shutdown_api_test.go
git commit -m "feat(web): 后台模式关停端点与 Control 注入"
```

---

### Task 7: CLI 解析 `web start|stop|status`

**Files:**
- Modify: `internal/cli/parse.go`（`WebAction`、`Options.WebAction`、`web` 分支）
- Modify: `internal/cli/usage.go`（总帮助与 `webUsage`）
- Test: `internal/cli/parse_test.go`、`internal/cli/usage_test.go`

**Interfaces:**
- Produces: `type WebAction int` + 常量 `WebForeground`（零值）/ `WebStart` / `WebStop` / `WebStatus`；`Options.WebAction WebAction`。
- Consumes: 现有 `containsHelpFlag`。

- [ ] **Step 1: Write the failing tests**

```go
func TestParseWebActions(t *testing.T) {
	cases := []struct {
		args  []string
		want  WebAction
		rest  []string
	}{
		{[]string{"web"}, WebForeground, nil},
		{[]string{"web", "start", "-p", "9000"}, WebStart, []string{"-p", "9000"}},
		{[]string{"web", "stop"}, WebStop, nil},
		{[]string{"web", "status"}, WebStatus, nil},
		{[]string{"web", "--addr", "0.0.0.0:8080", "--remote"}, WebForeground, []string{"--addr", "0.0.0.0:8080", "--remote"}},
	}
	for _, tc := range cases {
		cmd, opts, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if cmd != CommandWeb || opts.WebAction != tc.want {
			t.Fatalf("%v: cmd=%v action=%v, want %v", tc.args, cmd, opts.WebAction, tc.want)
		}
		if !reflect.DeepEqual(opts.Args, tc.rest) {
			t.Fatalf("%v: args = %v, want %v", tc.args, opts.Args, tc.rest)
		}
	}
}

func TestParseWebRejectsUnknownAction(t *testing.T) {
	_, _, err := Parse([]string{"web", "serve"})
	if err == nil || !strings.Contains(err.Error(), "无法识别的 web 子命令 serve") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseWebStatusHelp(t *testing.T) {
	cmd, opts, err := Parse([]string{"web", "status", "-h"})
	if err != nil || cmd != CommandHelp || !reflect.DeepEqual(opts.Args, []string{"web"}) {
		t.Fatalf("cmd=%v opts=%+v err=%v", cmd, opts, err)
	}
}
```

`usage_test.go` 里给 `TestUsageForTopics` 的 `web` 断言列表追加：`"web start"`、`"web stop"`、`"web status"`、`"note.log"`。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestParseWeb|TestUsageForTopics' -v`
Expected: FAIL，`undefined: WebAction`

- [ ] **Step 3: Implement**

`parse.go`：新增类型与常量；`case arg == "web" || arg == "info":` 里，`web` 分支先看 `rest[0]`：等于 `start`/`stop`/`status` → 映射到 `WebAction` 且 `rest = rest[1:]`；以 `-` 开头 → `WebForeground` 保持原样；其他 → `return CommandMenu, Options{}, fmt.Errorf("无法识别的 web 子命令 %s（可用: start, stop, status）", rest[0])`。`containsHelpFlag` 仍作用于截断后的 `rest`。`usage.go`：总帮助 Web 行改成 `web      启动 Web 服务（start/stop/status 管理后台实例，不带动作则前台运行）`，`webUsage()` 增加动作段与三行示例（`note web start`、`note web status`、`note web stop`）和一句「后台实例日志写入数据库同目录的 note.log」。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/parse.go internal/cli/usage.go internal/cli/parse_test.go internal/cli/usage_test.go
git commit -m "feat(cli): note web start/stop/status 子动作"
```

---

### Task 8: `note info` 的日志行与 `status` 输出渲染

**Files:**
- Modify: `internal/cli/info.go`（`InfoData` 加 `LogPath` / `LogExists` / `LogSizeBytes`，数据段加「日志」）
- Create: `internal/cli/status.go`、`internal/cli/status_test.go`
- Modify: `internal/cli/info_test.go`（断言「日志」行）

**Interfaces:**
- Produces:
  - `type StatusData struct { Running bool; Unresponsive bool; Pid int; URLs []string; StartedAt string; UptimeSeconds int64; LogPath string; LogExists bool; LogSizeBytes int64; DBPath string; Version string; StatePath string; StateExists bool; StalePid int }`
  - `func FormatStatus(d StatusData) string`
- Consumes: 现有 `displayWidth` / `padRight` / `humanSize` / `orDash`（`internal/cli/info.go`）。

- [ ] **Step 1: Write the failing tests**

```go
package cli

import (
	"strings"
	"testing"
)

func TestFormatStatusRunning(t *testing.T) {
	out := FormatStatus(StatusData{
		Running: true, Pid: 12345, URLs: []string{"http://127.0.0.1:8080"},
		StartedAt: "2026-10-28T09:12:33Z", UptimeSeconds: 11520,
		LogPath: "/data/note.log", LogExists: true, LogSizeBytes: 1 << 20,
		DBPath: "/data/notes.db", Version: "1.5.0 (418917b)",
		StatePath: "/data/note.json", StateExists: true,
	})
	for _, want := range []string{
		"note web · 运行中", "12345", "http://127.0.0.1:8080",
		"2026-10-28T09:12:33Z（已运行 3 小时 12 分）", "/data/note.log（1.0 MB）",
		"/data/notes.db", "1.5.0 (418917b)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status 缺少 %q:\n%s", want, out)
		}
	}
}

func TestFormatStatusStopped(t *testing.T) {
	out := FormatStatus(StatusData{
		LogPath: "/data/note.log", DBPath: "/data/notes.db",
		StatePath: "/data/note.json", StalePid: 4242,
	})
	for _, want := range []string{"note web · 未运行", "note.log（未创建）", "note.json（残留 pid 4242，已清理）", "note web start"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status 缺少 %q:\n%s", want, out)
		}
	}
}

func TestFormatStatusUnresponsive(t *testing.T) {
	out := FormatStatus(StatusData{Unresponsive: true, Pid: 999, LogPath: "/data/note.log", LogExists: true})
	for _, want := range []string{"进程存在但不响应", "999", "note.log"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status 缺少 %q:\n%s", want, out)
		}
	}
}

func TestFormatUptimeText(t *testing.T) {
	cases := map[int64]string{5: "5 秒", 90: "1 分 30 秒", 11520: "3 小时 12 分", 0: "0 秒"}
	for in, want := range cases {
		if got := formatUptime(in); got != want {
			t.Errorf("formatUptime(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatInfoShowsLogPath(t *testing.T) {
	out := FormatInfo(InfoData{Label: "1.5.0", GoVersion: "go1.26", DBPath: "/data/notes.db", LogPath: "/data/note.log"})
	if !strings.Contains(out, "日志") || !strings.Contains(out, "/data/note.log（未创建）") {
		t.Fatalf("info 缺少日志行:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run 'TestFormatStatus|TestFormatUptime|TestFormatInfoShowsLogPath' -v`
Expected: FAIL，`undefined: FormatStatus`

- [ ] **Step 3: Implement**

`status.go`：`FormatStatus` 复用 `sections` + `padRight` 的对齐写法（可把 `info.go` 的「渲染 sections」抽成内部函数 `renderSections(sections) string` 供两处调用，不改 `FormatInfo` 的输出）。三种形态的标题固定：`note web · 运行中` / `note web · 进程存在但不响应` / `note web · 未运行`；`formatUptime` 按上面的用例实现；未运行时末行给提示 `启动：note web start`。`info.go`：`InfoData` 加三个字段，`FormatInfo` 的「数据」段在「数据库」后插入 `{"日志", logSummary(d)}`，`logSummary` 存在时 `path（size）`、不存在时 `path（未创建）`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/status.go internal/cli/status_test.go internal/cli/info.go internal/cli/info_test.go
git commit -m "feat(cli): note web status 渲染与 info 日志行"
```

---

### Task 9: 子进程侧——后台模式写状态文件并接关停

**Files:**
- Modify: `cmd/note/main.go`（`runWeb` → `serve`；后台模式分支；菜单项改后台启动）

**Interfaces:**
- Consumes: `daemon.EnvStatePath`、`daemon.NewToken`、`daemon.StatePath`、`daemon.LogPath`、`daemon.Save`、`daemon.RemoveIfPid`、`daemon.State`、`web.Control`、`web.NewWithControl`。
- Produces: `func serve(cfg config.Config) error`（前台与后台共用的服务主体）。

- [ ] **Step 1: 改造 `cmd/note/main.go`**

`runWeb(globalConfigPath, args)` 只做 `loadConfig` 然后 `return serve(cfg)`；把现有 `runWeb` 的主体整体搬进 `serve(cfg)`，并在其中加入：

```go
statePath := os.Getenv(daemon.EnvStatePath) // 非空 = 后台子进程
var token string
var state daemon.State
if statePath != "" {
	if token, err = daemon.NewToken(); err != nil {
		return fmt.Errorf("generate shutdown token: %w", err)
	}
	state = daemon.State{
		Pid: os.Getpid(), Addr: cfg.Addr, StartedAt: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		DB: cfg.DBPath, Log: daemon.LogPath(cfg.DBPath), Token: token,
		Version: build.Describe().Label(),
	}
}
stopCh := make(chan struct{})
var stopOnce sync.Once
requestStop := func() { stopOnce.Do(func() { close(stopCh) }) }
```

`handler` 改为 `web.NewWithControl(cfg, st, dist, web.Control{Token: token, Shutdown: requestStop})`；`net.Listen` 成功后（打印 URL 之前）写状态文件：`state.Addr = ln.Addr().String()` 不能直接覆盖——按设计要用 `cfg.Addr` 的 host + 实际端口，因此 `state.Addr` 保持 `cfg.Addr`，另加 `state.URLs = config.StartupURLs(cfg.Addr, ln.Addr().String())` 并 `daemon.Save(statePath, state)`；失败时 `return fmt.Errorf("write daemon state %s: %w", statePath, err)`（此时 listener 已建，`defer ln.Close()` 必须存在）。`select` 增加 `case <-stopCh: slog.Info("shutdown requested via control endpoint")`。退出前（`server.Shutdown` 与 `st.Close` 之后）`defer daemon.RemoveIfPid(statePath, os.Getpid())`，用一个 `func() { if statePath != "" && os.Getenv(daemon.EnvStatePath) == statePath { ... } }()` 收尾（简单起见：`if statePath != "" { defer func(){ if err := daemon.RemoveIfPid(statePath, os.Getpid()); err != nil { slog.Warn(...) } }() }`）。

菜单分支：`case cli.MenuWeb: return runWebStart(opts.ConfigPath, nil)`（`runWebStart` 由 Task 10 提供；本任务先写成 `return runWeb(opts.ConfigPath, nil)` 会导致编译通过但语义未达成——**因此本任务只改 `serve`，菜单改动放 Task 10**）。

- [ ] **Step 2: 验证编译与既有行为不变**

Run: `go build ./... && go vet ./...`
Expected: 无输出（成功）

- [ ] **Step 3: 前台冒烟（行为必须完全不变）**

Run: `go run ./cmd/note web --addr 127.0.0.1:8097`（另开一个终端）`curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8097/` 与 `curl -s -X POST -o /dev/null -w '%{http_code}' http://127.0.0.1:8097/api/shutdown`
Expected: `/` 返回 200；`/api/shutdown` 返回 **403**（前台模式未启用）；`Ctrl+C` 后进程正常退出、日志仍在终端上

- [ ] **Step 4: Commit**

```bash
git add cmd/note/main.go
git commit -m "feat(note): 后台子进程写状态文件并接受关停请求"
```

---

### Task 10: `note web start / stop / status` 与菜单

**Files:**
- Create: `cmd/note/web_service.go`（`runWebStart` / `runWebStop` / `runWebStatus` 与它们的纯逻辑辅助）
- Modify: `cmd/note/main.go`（`run()` 的 `CommandWeb` 分派、菜单动作、`runInfo` 补日志字段）
- Modify: `internal/cli/menu.go`（菜单项文案改「启动 Web 服务（后台）」）

**Interfaces:**
- Consumes: Task 1–9 的全部导出接口。
- Produces: 三个 `func runWebXxx(globalConfigPath string, args []string) error`。

- [ ] **Step 1: 实现 `runWebStart`**

流程（每步失败都要有可读文案）：`loadConfig` → `statePath := daemon.StatePath(cfg.DBPath)`；若 `Load` 成功且 `daemon.ProcessAlive(st.Pid)` 且 `daemon.Probe(st.Addr)` 成功 → 打印 `note 已在后台运行（pid %d）` + `st.URLs` + 日志路径，`return nil`；若 `Load` 成功但 pid 已死 → `daemon.RemoveIfPid` 清理后继续。随后 `os.MkdirAll(filepath.Dir(cfg.DBPath), 0o700)`；`exe, err := os.Executable()`；子进程参数 = 可选 `-c <globalConfigPath>` + `"web"` + `args`；`cmd, err := daemon.RunDetached(exe, childArgs, daemon.LogPath(cfg.DBPath), statePath)`；`go cmd.Wait()`；就绪轮询（100ms / 10s）：每轮 `daemon.Load(statePath)` 拿到 `st` 后 `daemon.Probe(st.Addr)` 成功即就绪；轮询期间若 `cmd.ProcessState != nil`（子进程已退出）立即失败；失败时打印 `启动失败：%v` + 日志路径 + 日志尾部（读最后 20 行，纯函数 `tailLines(path string, n int) []string` 放同文件），`daemon.RemoveIfPid(statePath, cmd.Process.Pid)` 后 `return fmt.Errorf("web 服务启动失败")`。成功时打印：

```text
note 已在后台启动，在浏览器打开：
http://127.0.0.1:8080
  pid   12345
  日志  /…/note.log
停止：note web stop    状态：note web status
```

- [ ] **Step 2: 实现 `runWebStop`**

`loadConfig` → `Load(statePath)`：`os.ErrNotExist` → 探测 `cfg.Addr`：若 `Probe` 成功 → 打印「检测到运行中的实例（前台启动或状态文件缺失），`note web stop` 只能关停后台实例，请回到它的终端按 Ctrl+C」，`return errExit(1)`；否则打印「note web 未在后台运行」，`return nil`。状态文件损坏 → 打印「状态文件 %s 无法解析（%v），请手动检查或删除它后重试」，`return errExit(1)`。有状态但 pid 已死 → `RemoveIfPid` + 打印「未在运行（已清理残留状态文件）」+ `return nil`。正常路径：`daemon.RequestStop(st)`（失败只 `slog.Warn`，继续等）；每 100ms 轮询 `ProcessAlive(st.Pid)` 至 10s；退出后 `daemon.RemoveIfPid(statePath, st.Pid)` 并打印「已停止（pid %d）」，`return nil`；超时 → Unix/Windows 都 `cmd := os.FindProcess(st.Pid); cmd.Kill()`（Windows 上即 `TerminateProcess`），打印「已强制结束（pid %d）：WAL 未完成 checkpoint，数据安全但下次启动会重放日志」，清理状态文件，`return nil`。

- [ ] **Step 3: 实现 `runWebStatus`**

`loadConfig` → `Load(statePath)` 分三种：成功（可能真、可能残留）、`os.ErrNotExist`、损坏（打印错误，`return errExit(1)`）。探测地址 = 状态文件里的 `st.Addr`，无状态文件时用 `cfg.Addr`；`info, probeErr := daemon.Probe(addr)`；按设计 §4.3 的表组装 `cli.StatusData`（`Running` / `Unresponsive` / 都否），`LogExists` / `LogSizeBytes` 用 `os.Stat(daemon.LogPath(cfg.DBPath))`；pid 已死但有状态文件 → `RemoveIfPid` 并把 `StalePid` 填进去。`fmt.Print(cli.FormatStatus(data))`，然后按 `Running` / `Unresponsive` / 其余分别 `return nil` / `return errExit(1)` / `return errExit(3)`。

`errExit` 在 `cmd/note/main.go` 定义：

```go
// exitError 让子命令指定退出码而不打印错误日志（status 未运行不是故障）。
type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func errExit(code int) error { return exitError(code) }
```

`run()` 里 `CommandWeb` 分派改为按 `opts.WebAction` 调四个函数；`main()` 里 `if err := run(...)` 前先 `var code exitError; if errors.As(err, &code) { os.Exit(int(code)); }`。

- [ ] **Step 4: 菜单与 `note info`**

`internal/cli/menu.go`：`{label: "启动 Web 服务", action: MenuWeb}` → `{label: "启动 Web 服务（后台）", action: MenuWeb}`（`menu_test.go` 里若有文案断言同步改）。`cmd/note/main.go` 菜单分支改 `runWebStart`。`buildInfoData` 补：

```go
data.LogPath = daemon.LogPath(cfg.DBPath)
if stat, err := os.Stat(data.LogPath); err == nil {
	data.LogExists = true
	data.LogSizeBytes = stat.Size()
}
```

- [ ] **Step 5: 编译、单测与真实二进制冒烟**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: 全部通过

Run（冒烟，换独立端口与临时库，避免碰到真实数据。注意：`--db`/`--addr` 必须写在 `web` 之后，全局层只认 `-c/--config`；显式指定 `--config` 时文件必须存在，所以先建它）：

```bash
just build
printf 'server:\n  addr: 127.0.0.1:8099\n' > .run/smoke.yaml

# 1) 正常链路
./note.exe web start --addr 127.0.0.1:8099 --db .run/smoke.db --config .run/smoke.yaml
    # 期望打印地址/pid/日志路径；.run/note.json 与 .run/note.log 生成
./note.exe web status --db .run/smoke.db --config .run/smoke.yaml    # 期望「运行中」，退出码 0
./note.exe info --db .run/smoke.db --config .run/smoke.yaml          # 期望含「日志 …/note.log」
./note.exe web start --addr 127.0.0.1:8099 --db .run/smoke.db --config .run/smoke.yaml
    # 期望「已在后台运行」
./note.exe web stop --db .run/smoke.db --config .run/smoke.yaml      # 期望「已停止」，进程消失，note.json 删除
./note.exe web status --db .run/smoke.db --config .run/smoke.yaml    # 期望「未运行」，退出码 3

# 2) 端口被占：另开终端跑前台实例占住 8098（用另一个库）后，再 start 同一端口
./note.exe web --addr 127.0.0.1:8098 --db .run/fg.db
./note.exe web start --addr 127.0.0.1:8098 --db .run/smoke.db --config .run/smoke.yaml
    # 期望「启动失败」+ 失败原因 + 日志尾部 + 退出码 1；.run/note.json 不应残留

# 3) 前台实例 + 无状态文件：status 说运行中，stop 必须拒绝（不杀前台进程）
./note.exe web status --addr 127.0.0.1:8098 --db .run/smoke.db --config .run/smoke.yaml   # 退出码 0
./note.exe web stop --addr 127.0.0.1:8098 --db .run/smoke.db --config .run/smoke.yaml
    # 期望「只能关停后台实例，请回到它的终端按 Ctrl+C」，退出码 1；前台实例仍在

# 4) 残留状态文件：手动写入 {"pid": 999999} 后
./note.exe web status --db .run/smoke.db --config .run/smoke.yaml
    # 期望「未运行」+「残留 pid 999999，已清理」，退出码 3
```

Expected：日志文件 `.run/note.log` 里能看到 `listening` 与 `shutting down`（后者证明走的是优雅关停而非强杀）；第 2 条不启后台进程、不留状态文件；第 3 条不杀前台实例。

- [ ] **Step 6: Commit**

```bash
git add cmd/note/main.go cmd/note/web_service.go internal/cli/menu.go internal/cli/menu_test.go
git commit -m "feat(note): note web start/stop/status 与后台菜单入口"
```

---

### Task 11: 文档同步与 CHANGELOG

**Files:**
- Modify: `docs/PRD.md`（§8.9 / §8.10 / §8.11、验收标准、修订说明与版本号）
- Modify: `docs/TECH_DESIGN.md`（§4.1 / §4.2 / §4.3 / §4.4 附注 / §4.6 / §14.1、新增 ADR、里程碑）
- Modify: `AGENTS.md`（部署形态 + 硬性约定）
- Modify: `CHANGELOG.md`（`[Unreleased]`）

- [ ] **Step 1: 按设计文档 §8 的清单逐条改写**

要点已在设计中固定，实现时只写事实：命令形态与退出码（0/1/3）、日志与状态文件位置及权限、`/api/shutdown` 的存在条件与令牌来源、`note info` 新增日志行、`stop` 对前台实例的拒绝行为、强制结束的 WAL 说明。PRD/TECH_DESIGN 里的版本号各升一位并在修订说明里指向本设计文档。

- [ ] **Step 2: `AGENTS.md` 硬性约定追加两条**

- 「`/api/shutdown` 只在后台模式（`Control.Token` 非空）存在，凭状态文件里的 32 字节令牌；该路径的 CSRF 豁免不得扩大，令牌不得进日志/响应/命令行。」
- 「后台实例的日志与状态文件固定在 `filepath.Dir(DBPath)`（`note.log` / `note.json`），`note info` 必须输出日志路径。」

- [ ] **Step 3: 跑完整的门禁**

Run: `just test && just check`
Expected: Go 与前端全部通过、`go vet` 与 `svelte-check` 0 error 0 warning（前端未改，应无变化）

- [ ] **Step 4: 写 CHANGELOG 条目（先测后写）**

在 `[Unreleased]` 的 `### Added` 顶部新增一条，覆盖：`internal/daemon` 新包与各文件职责、自我 re-exec 后台化与平台 `SysProcAttr`、日志 fd 重定向的原因（slog 默认 handler 启动即绑定 stderr）、`/api/shutdown` 与 CSRF 豁免边界、令牌由子进程生成、`ControlAddr` 的通配映射、`status` 三态与退出码、`note info` 日志行、TDD 顺序（先 RED 后 GREEN）与真实二进制冒烟结果（含日志里的 `shutting down` 证据）。`### Changed` 补一条非破坏性说明（`note web` 语义不变，菜单项改后台）。

- [ ] **Step 5: Commit**

```bash
git add docs/PRD.md docs/TECH_DESIGN.md AGENTS.md CHANGELOG.md
git commit -m "docs: note web 后台运行（start/stop/status）与日志文件位置"
```
