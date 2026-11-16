package main

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
)

const (
	// fallbackPortStart / fallbackPortEnd 是配置端口被占用时的顺延范围（含两端）：
	// note web 默认监听 127.0.0.1:8080，8080 被占时依次尝试 8081…8090。
	fallbackPortStart = 8080
	fallbackPortEnd   = 8090
)

// listenWeb 按 addr 监听 TCP；端口被占用（EADDRINUSE）时在同一主机上顺延到
// fallbackPortStart..fallbackPortEnd 里第一个可用端口，全部被占用时返回带范围
// 说明的错误。监听成功时返回已绑定的 listener，调用方用 ln.Addr() 取实际端口。
// 前台 `note web` 与后台子进程共用 serve，因此两条启动路径都走这里。
func listenWeb(addr string) (net.Listener, error) {
	return listenWebRange(addr, fallbackPortStart, fallbackPortEnd)
}

// listenWebRange 是 listenWeb 的可注入范围版本，范围参数供测试使用。
func listenWebRange(addr string, start, end int) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err == nil {
		return ln, nil
	}
	if !isAddrInUse(err) {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
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
	}
	return nil, fmt.Errorf(
		"listen on %s: %w（备用端口 %d-%d 也全部被占用）", addr, err, start, end)
}
