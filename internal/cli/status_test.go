package cli

import (
	"strings"
	"testing"
	"time"
)

// withFixedZone 把 time.Local 临时换成固定时区，避免用例结果依赖跑测试机器的时区。
func withFixedZone(t *testing.T, offsetSeconds int) {
	t.Helper()
	saved := time.Local
	time.Local = time.FixedZone("TEST", offsetSeconds)
	t.Cleanup(func() { time.Local = saved })
}

func TestFormatStatusRunning(t *testing.T) {
	// timefmt 包内部换固定时区，验证启动时间按本机时区展示到秒。
	withFixedZone(t, 8*3600)

	out := FormatStatus(StatusData{
		Running: true, Pid: 12345, URLs: []string{"http://127.0.0.1:8080"},
		StartedAt: "2026-10-28T01:12:33Z", UptimeSeconds: 11520,
		LogPath: "/data/note.log", LogExists: true, LogSizeBytes: 1 << 20,
		DBPath: "/data/notes.db", Version: "1.5.0 (418917b)",
		StatePath: "/data/note.json", StateExists: true,
	})
	for _, want := range []string{
		"note web · 运行中", "12345", "http://127.0.0.1:8080",
		"2026-10-28 09:12:33（已运行 3 小时 12 分）", "/data/note.log（1.0 MB）",
		"/data/notes.db", "1.5.0 (418917b)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("status 缺少 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "T09:12:33") || strings.Contains(out, "Z（") {
		t.Fatalf("启动时间必须是本机时区的年月日 时分秒，不带 T/Z：\n%s", out)
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

// TestFormatStatusAlignsMultipleURLs 多地址（通配监听）续行必须按标签列对齐：
// 缩进写死空格数在 CJK 标签下会错位，这里按「地址」列位置断言。
func TestFormatStatusAlignsMultipleURLs(t *testing.T) {
	out := FormatStatus(StatusData{
		Running: true,
		Pid:     1234,
		URLs:    []string{"http://192.168.1.5:8080", "http://10.0.0.2:8080"},
		LogPath: "/data/note.log",
		DBPath:  "/data/notes.db",
	})
	lines := strings.Split(out, "\n")
	// 比的是显示列（CJK 标签的字节数 ≠ 列数），不是字节下标。
	column := func(line, marker string) int {
		idx := strings.Index(line, marker)
		if idx < 0 {
			return -1
		}
		return displayWidth(line[:idx])
	}
	firstCol := -1
	secondCol := -1
	for _, line := range lines {
		if c := column(line, "http://192.168.1.5:8080"); c >= 0 {
			firstCol = c
		}
		if c := column(line, "http://10.0.0.2:8080"); c >= 0 {
			secondCol = c
		}
	}
	if firstCol <= 0 || secondCol != firstCol {
		t.Fatalf("多地址未对齐：first=%d second=%d\n%s", firstCol, secondCol, out)
	}
}
