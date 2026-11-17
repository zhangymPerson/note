package build

import (
	"strings"
	"testing"
	"time"
)

// 包级 var 会被测试直接改写，每个用例结束时必须还原，否则污染同包其它测试。
func capture() (hash, dirty, date string) {
	return GitHash, GitDirty, BuildDate
}

// withFixedZone 把 time.Local 临时换成固定时区，让构建时间的断言不依赖跑测试的机器。
func withFixedZone(t *testing.T, offsetSeconds int) {
	t.Helper()
	saved := time.Local
	time.Local = time.FixedZone("TEST", offsetSeconds)
	t.Cleanup(func() { time.Local = saved })
}

func restore(t *testing.T, hash, dirty, date string) {
	t.Helper()
	GitHash, GitDirty, BuildDate = hash, dirty, date
}

func TestDescribeUsesInjectedValues(t *testing.T) {
	withFixedZone(t, 8*3600)
	hash, dirty, date := capture()
	defer restore(t, hash, dirty, date)

	GitHash, GitDirty, BuildDate = "abc1234", "true", "2026-10-27T09:00:00+08:00"

	info := Describe()
	if !info.Injected {
		t.Errorf("injected build must report Injected=true")
	}
	if info.GitHash != "abc1234" {
		t.Errorf("GitHash = %q, want %q", info.GitHash, "abc1234")
	}
	// 展示口径：本机时区、年月日 时分秒、没有 T/Z 后缀
	if info.BuildDate != "2026-10-27 09:00:00" {
		t.Errorf("BuildDate = %q, want %q", info.BuildDate, "2026-10-27 09:00:00")
	}
	if info.GitDirty != "true" {
		t.Errorf("GitDirty = %q, want %q", info.GitDirty, "true")
	}
	if info.Version != Version {
		t.Errorf("Version = %q, want %q", info.Version, Version)
	}
	want := Version + " (abc1234, 2026-10-27 09:00:00, dirty)"
	if got := info.Label(); got != want {
		t.Errorf("Label() = %q, want %q", got, want)
	}
}

func TestDescribeFallsBackWhenNotInjected(t *testing.T) {
	hash, dirty, date := capture()
	defer restore(t, hash, dirty, date)

	GitHash, GitDirty, BuildDate = "", "", ""

	info := Describe()
	if info.Injected {
		t.Errorf("build without ldflags must report Injected=false")
	}
	label := info.Label()
	if !strings.Contains(label, "（开发构建）") {
		t.Errorf("Label() = %q, want it to mark a development build", label)
	}
	if strings.Contains(label, "<nil>") {
		t.Errorf("Label() = %q, must not leak a nil formatting", label)
	}
}

func TestDisplayBuildDateUsesLocalZone(t *testing.T) {
	// 回落路径的 vcs.time 是 UTC；注入值则是 justfile 按构建机时区生成的带偏移串。
	// 展示口径统一成本机时区的年月日 时分秒，绝对时刻不变。
	withFixedZone(t, 8*3600)

	got := displayBuildDate("2026-10-27T09:00:00Z")
	if want := "2026-10-27 17:00:00"; got != want {
		t.Fatalf("displayBuildDate = %q, want %q", got, want)
	}
	if parsed, err := time.ParseInLocation("2006-01-02 15:04:05", got, time.Local); err != nil {
		t.Fatalf("displayBuildDate 必须产出展示格式: %q (%v)", got, err)
	} else if want := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC); !parsed.Equal(want) {
		t.Errorf("displayBuildDate 改动了时刻: %q", got)
	}
}

func TestDisplayBuildDateKeepsUnparsable(t *testing.T) {
	// 解析不了就原样返回：信息页缺一行也比 Describe 出错好。
	for _, in := range []string{"", "not-a-time"} {
		if got := displayBuildDate(in); got != in {
			t.Errorf("displayBuildDate(%q) = %q，应当原样返回", in, got)
		}
	}
}

func TestLabelNeverShowsEmptyParens(t *testing.T) {
	hash, dirty, date := capture()
	defer restore(t, hash, dirty, date)

	GitHash, GitDirty, BuildDate = "abc1234", "", "2026-10-27T09:00:00+08:00"
	label := Describe().Label()
	if strings.Contains(label, "( )") || strings.Contains(label, "()") || strings.Contains(label, ", )") {
		t.Errorf("Label() = %q, clean build must not leave an empty slot", label)
	}
	if strings.HasSuffix(label, ", ") || strings.Contains(label, ", ,") {
		t.Errorf("Label() = %q, trailing or doubled separator", label)
	}
}
