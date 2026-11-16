package main

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"

	"note/internal/daemon"
)

const (
	// fallbackPortStart / fallbackPortEnd 是配置端口被占用时的顺延范围（含两端）：
	// note web 默认监听 127.0.0.1:8080，8080 被占时依次尝试 8081…8090。
	fallbackPortStart = 8080
	fallbackPortEnd   = 8090
)

// probeFunc 探测 addr 上是否有 note 实例在响应 /api/info；
// 返回错误表示「不是可识别的 note 实例」（无关进程或探测超时）。
type probeFunc func(addr string) (daemon.ServerInfo, error)

// duplicateNoteError 表示 addr 上已有一个使用同一数据库的 note 实例：
// 再启动一个就是双实例抢同一个 SQLite 库，必须拒绝而不是往下一个端口躲。
type duplicateNoteError struct {
	addr string
	info daemon.ServerInfo
}

func (e *duplicateNoteError) Error() string {
	return fmt.Sprintf("%s 上已有 note 实例在运行且使用同一数据库", e.addr)
}

// listenWeb 按 addr 监听 TCP。端口被占用（EADDRINUSE）时：
//
//   - 占用方是「同一数据库的 note 实例」→ 拒绝（duplicateNoteError），
//     不重复启动第二个实例；
//   - 其它情况（无关进程、异库 note 实例）→ 顺延到
//     fallbackPortStart..fallbackPortEnd 里第一个可用端口。
//
// 全部被占用时返回带范围说明的错误。监听成功时返回已绑定的 listener，
// 调用方用 ln.Addr() 取实际端口。前台 `note web` 与后台子进程共用 serve，
// 因此两条启动路径都走这里。
func listenWeb(addr, dbPath string) (net.Listener, error) {
	return listenWebRange(addr, fallbackPortStart, fallbackPortEnd, dbPath, daemon.ProbeShort)
}

// listenWebRange 是 listenWeb 的可注入范围版本，范围与探测函数供测试使用。
func listenWebRange(addr string, start, end int, dbPath string, probe probeFunc) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if !isAddrInUse(err) {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	// 配置端口被同库 note 实例占住：直接拒绝，别等顺延扫描。
	if dup := occupiedByNote(addr, dbPath, probe); dup != nil {
		return nil, dup
	}
	host, _, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	for port := start; port <= end; port++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(port))
		if candidate == addr {
			continue // 配置端口刚试过
		}
		if ln, err := net.Listen("tcp", candidate); err == nil {
			slog.Warn("configured port in use, falling back",
				"from", addr, "to", candidate)
			return ln, nil
		}
		// 顺延候选也被同库 note 实例占住：拒绝，而不是再往下一个端口躲。
		if dup := occupiedByNote(candidate, dbPath, probe); dup != nil {
			return nil, dup
		}
	}
	return nil, fmt.Errorf(
		"listen on %s: %w（备用端口 %d-%d 也全部被占用）", addr, err, start, end)
}

// occupiedByNote 探测 addr：是 note 实例且数据库与本次相同 → 返回拒绝错误；
// 探测失败（不是 note / 超时）或数据库不同 → nil，调用方继续顺延。
func occupiedByNote(addr, dbPath string, probe probeFunc) error {
	info, err := probe(addr)
	if err != nil {
		return nil
	}
	if !sameDB(info.DBPath, dbPath) {
		return nil
	}
	return &duplicateNoteError{addr: addr, info: info}
}
