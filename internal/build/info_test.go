package build

import (
	"strings"
	"testing"
)

// 包级 var 会被测试直接改写，每个用例结束时必须还原，否则污染同包其它测试。
func capture() (hash, dirty, date string) {
	return GitHash, GitDirty, BuildDate
}

func restore(t *testing.T, hash, dirty, date string) {
	t.Helper()
	GitHash, GitDirty, BuildDate = hash, dirty, date
}

func TestDescribeUsesInjectedValues(t *testing.T) {
	hash, dirty, date := capture()
	defer restore(t, hash, dirty, date)

	GitHash, GitDirty, BuildDate = "abc1234", "true", "2026-10-27T09:00:00Z"

	info := Describe()
	if !info.Injected {
		t.Errorf("injected build must report Injected=true")
	}
	if info.GitHash != "abc1234" {
		t.Errorf("GitHash = %q, want %q", info.GitHash, "abc1234")
	}
	if info.BuildDate != "2026-10-27T09:00:00Z" {
		t.Errorf("BuildDate = %q, want %q", info.BuildDate, "2026-10-27T09:00:00Z")
	}
	if info.GitDirty != "true" {
		t.Errorf("GitDirty = %q, want %q", info.GitDirty, "true")
	}
	if info.Version != Version {
		t.Errorf("Version = %q, want %q", info.Version, Version)
	}
	want := Version + " (abc1234, 2026-10-27T09:00:00Z, dirty)"
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

func TestLabelNeverShowsEmptyParens(t *testing.T) {
	hash, dirty, date := capture()
	defer restore(t, hash, dirty, date)

	GitHash, GitDirty, BuildDate = "abc1234", "", "2026-10-27T09:00:00Z"
	label := Describe().Label()
	if strings.Contains(label, "( )") || strings.Contains(label, "()") || strings.Contains(label, ", )") {
		t.Errorf("Label() = %q, clean build must not leave an empty slot", label)
	}
	if strings.HasSuffix(label, ", ") || strings.Contains(label, ", ,") {
		t.Errorf("Label() = %q, trailing or doubled separator", label)
	}
}
