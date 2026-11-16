package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"note/internal/daemon"
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

// occupy 在指定端口上挂一个只监听不响应的 listener，模拟「被无关进程占用」。
func occupy(t *testing.T, port int) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("occupy port %d: %v", port, err)
	}
	return ln
}

// noNoteProbe 是一个永远探测不到 note 实例的 probe：占用方是无关进程。
func noNoteProbe(string) (daemon.ServerInfo, error) {
	return daemon.ServerInfo{}, errors.New("not a note instance")
}

// noteOn 返回一个「只在指定端口上发现同库 note 实例」的 probe。
func noteOn(port int, dbPath string) probeFunc {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	return func(got string) (daemon.ServerInfo, error) {
		if got == addr {
			return daemon.ServerInfo{DBPath: dbPath}, nil
		}
		return daemon.ServerInfo{}, errors.New("not a note instance")
	}
}

// TestListenWebFallsBack 占用配置端口后，listenWeb 应顺延到备用范围内的
// 第一个可用端口，而不是直接失败。
func TestListenWebFallsBack(t *testing.T) {
	base := freePort(t)
	blocker := occupy(t, base)
	defer blocker.Close()

	ln, err := listenWebRange(fmt.Sprintf("127.0.0.1:%d", base), base, base+10, "/data/notes.db", noNoteProbe)
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
		blockers = append(blockers, occupy(t, port))
	}

	_, err := listenWebRange(fmt.Sprintf("127.0.0.1:%d", base), base, base+10, "/data/notes.db", noNoteProbe)
	if err == nil {
		t.Fatal("expected error when all fallback ports are occupied")
	}
	if !strings.Contains(err.Error(), "全部被占用") {
		t.Fatalf("error = %q, want range-exhausted message", err)
	}
}

// TestListenWebNonOccupancyError 非「地址被占用」的监听错误原样返回，不触发顺延。
func TestListenWebNonOccupancyError(t *testing.T) {
	_, err := listenWebRange("127.0.0.1:99999", 8080, 8090, "/data/notes.db", noNoteProbe)
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

// TestListenWebRefusesDuplicateOnConfiguredPort 配置端口本身被同库 note 实例
// 占住时，拒绝启动而不是顺延到下一个端口再起一个双实例。
func TestListenWebRefusesDuplicateOnConfiguredPort(t *testing.T) {
	base := freePort(t)
	blocker := occupy(t, base)
	defer blocker.Close()
	addr := fmt.Sprintf("127.0.0.1:%d", base)

	_, err := listenWebRange(addr, base, base+10, "/data/notes.db", noteOn(base, "/data/notes.db"))
	var dup *duplicateNoteError
	if !errors.As(err, &dup) {
		t.Fatalf("err = %v, want *duplicateNoteError", err)
	}
	if dup.addr != addr {
		t.Fatalf("dup.addr = %q, want %q", dup.addr, addr)
	}
}

// TestListenWebRefusesDuplicateDuringFallback 顺延扫描中遇到同库 note 实例
// 占住的候选端口时，拒绝启动而不是继续往下一个端口躲。
func TestListenWebRefusesDuplicateDuringFallback(t *testing.T) {
	base := freePort(t)
	blocker := occupy(t, base)
	defer blocker.Close()
	blocker2 := occupy(t, base+1)
	defer blocker2.Close()
	// base 被无关进程占用；base+1 被「同库 note 实例」占用；base+2 空闲。
	probe := func(got string) (daemon.ServerInfo, error) {
		if got == fmt.Sprintf("127.0.0.1:%d", base+1) {
			return daemon.ServerInfo{DBPath: "/data/notes.db"}, nil
		}
		return daemon.ServerInfo{}, errors.New("not a note instance")
	}

	_, err := listenWebRange(fmt.Sprintf("127.0.0.1:%d", base), base, base+10, "/data/notes.db", probe)
	var dup *duplicateNoteError
	if !errors.As(err, &dup) {
		t.Fatalf("err = %v, want *duplicateNoteError", err)
	}
	if dup.addr != fmt.Sprintf("127.0.0.1:%d", base+1) {
		t.Fatalf("dup.addr = %q, want base+1", dup.addr)
	}
}

// TestListenWebSkipsDifferentDBInstance 顺延扫描遇到的 note 实例用的是另一份
// 数据库时合法：继续顺延到下一个空闲端口，不拒绝。
func TestListenWebSkipsDifferentDBInstance(t *testing.T) {
	base := freePort(t)
	blocker := occupy(t, base)
	defer blocker.Close()
	blocker2 := occupy(t, base+1)
	defer blocker2.Close()
	// base 被无关进程占用；base+1 被「异库 note 实例」占用；base+2 空闲。
	probe := func(got string) (daemon.ServerInfo, error) {
		if got == fmt.Sprintf("127.0.0.1:%d", base+1) {
			return daemon.ServerInfo{DBPath: "/other/notes.db"}, nil
		}
		return daemon.ServerInfo{}, errors.New("not a note instance")
	}

	ln, err := listenWebRange(fmt.Sprintf("127.0.0.1:%d", base), base, base+10, "/data/notes.db", probe)
	if err != nil {
		t.Fatalf("listenWebRange: %v", err)
	}
	defer ln.Close()
	if got := ln.Addr().(*net.TCPAddr).Port; got != base+2 {
		t.Fatalf("fallback port = %d, want %d", got, base+2)
	}
}
