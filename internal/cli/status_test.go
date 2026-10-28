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
	for _, want := range []string{
		"note web · 未运行", "note.log（未创建）",
		"note.json（残留 pid 4242，已清理）", "note web start",
	} {
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
	cases := map[int64]string{0: "0 秒", 5: "5 秒", 90: "1 分 30 秒", 11520: "3 小时 12 分"}
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
