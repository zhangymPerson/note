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
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("日志里没有子进程输出（err=%v）", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
