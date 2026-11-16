package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// freePort 借一个本机空闲端口（bind :0 拿到后立刻释放，供测试占用）。
// 释放到重新绑定的窗口极小，测试在本机回环上进行，不涉网络。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return port
}

// TestListenWebFallsBack 占用配置端口后，listenWeb 应顺延到备用范围内的
// 第一个可用端口，而不是直接失败。
func TestListenWebFallsBack(t *testing.T) {
	base := freePort(t)
	blocker, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", base))
	if err != nil {
		t.Fatalf("occupy port %d: %v", base, err)
	}
	defer blocker.Close()

	ln, err := listenWebRange(fmt.Sprintf("127.0.0.1:%d", base), base, base+10)
	if err != nil {
		t.Fatalf("listenWebRange: %v", err)
	}
	defer ln.Close()

	got := ln.Addr().(*net.TCPAddr).Port
	if got == base || got < base || got > base+10 {
		t.Fatalf("fallback port = %d, want in (%d, %d]", got, base, base+10)
	}
}

// TestListenWebAllOccupied 备用范围全部被占用时返回带范围说明的错误。
func TestListenWebAllOccupied(t *testing.T) {
	base := freePort(t)
	var blockers []net.Listener
	defer func() {
		for _, l := range blockers {
			l.Close()
		}
	}()
	for port := base; port <= base+10; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatalf("occupy port %d: %v", port, err)
		}
		blockers = append(blockers, ln)
	}

	_, err := listenWebRange(fmt.Sprintf("127.0.0.1:%d", base), base, base+10)
	if err == nil {
		t.Fatal("expected error when all fallback ports are occupied")
	}
	if !strings.Contains(err.Error(), "全部被占用") {
		t.Fatalf("error = %q, want range-exhausted message", err)
	}
}

// TestListenWebNonOccupancyError 非「地址被占用」的监听错误原样返回，不触发顺延。
func TestListenWebNonOccupancyError(t *testing.T) {
	_, err := listenWebRange("127.0.0.1:99999", 8080, 8090)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:99999") {
		t.Fatalf("error = %q, want configured address in message", err)
	}
	if strings.Contains(err.Error(), "全部被占用") {
		t.Fatalf("error = %q, must not claim range exhausted", err)
	}
}
