package timefmt

import (
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

func TestDisplayTimeUsesLocalZone(t *testing.T) {
	withFixedZone(t, 8*3600)

	utc := time.Date(2026, 10, 27, 1, 2, 3, 0, time.UTC)
	if got, want := DisplayTime(utc), "2026-10-27 09:02:03"; got != want {
		t.Fatalf("DisplayTime = %q, want %q", got, want)
	}
}

func TestDisplayTimeDropsFractionalSeconds(t *testing.T) {
	withFixedZone(t, 0)

	t0 := time.Date(2026, 10, 27, 1, 2, 3, 999_000_000, time.UTC)
	if got, want := DisplayTime(t0), "2026-10-27 01:02:03"; got != want {
		t.Fatalf("DisplayTime = %q, want %q", got, want)
	}
}

func TestDisplayConvertsUTCSecondText(t *testing.T) {
	withFixedZone(t, 8*3600)

	// 库里存的是 UTC 秒级文本，展示时必须按本机时区换过来。
	if got, want := Display("2026-10-27T01:02:03Z"), "2026-10-27 09:02:03"; got != want {
		t.Fatalf("Display = %q, want %q", got, want)
	}
}

func TestDisplayConvertsOffsetText(t *testing.T) {
	withFixedZone(t, 8*3600)

	// justfile 注入的构建时间是带偏移的 RFC3339，绝对时刻必须保持不变。
	if got, want := Display("2026-10-27T09:02:03+08:00"), "2026-10-27 09:02:03"; got != want {
		t.Fatalf("Display = %q, want %q", got, want)
	}
}

func TestDisplayPassesThroughDisplayText(t *testing.T) {
	withFixedZone(t, 8*3600)

	// 已经是展示格式的串（如状态文件由旧版写入）再格式化一次不该变样。
	if got, want := Display("2026-10-27 09:02:03"), "2026-10-27 09:02:03"; got != want {
		t.Fatalf("Display = %q, want %q", got, want)
	}
}

func TestDisplayKeepsUnparsable(t *testing.T) {
	withFixedZone(t, 8*3600)

	for _, in := range []string{"", "not-a-time", "2026-10-27"} {
		if got := Display(in); got != in {
			t.Fatalf("Display(%q) = %q，应当原样返回", in, got)
		}
	}
}
