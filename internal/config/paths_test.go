package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// homeEnvFor 把当前平台的用户目录环境变量指到 base，
// 使 os.UserConfigDir / DefaultDataDir 的解析结果可预测。
func homeEnvFor(t *testing.T, base string) {
	t.Helper()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("AppData", base)
	case "darwin", "ios":
		t.Setenv("HOME", base)
	default:
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "config"))
		t.Setenv("XDG_DATA_HOME", filepath.Join(base, "share"))
	}
}

// expectedConfigDir 返回各平台惯例的配置目录（base 为用户根）。
func expectedConfigDir(base string) string {
	switch runtime.GOOS {
	case "windows":
		return base
	case "darwin", "ios":
		return filepath.Join(base, "Library", "Application Support")
	default:
		return filepath.Join(base, "config")
	}
}

// expectedDataDir 返回各平台惯例的数据目录（base 为用户根）。
func expectedDataDir(base string) string {
	switch runtime.GOOS {
	case "windows":
		return base
	case "darwin", "ios":
		return filepath.Join(base, "Library", "Application Support")
	default:
		return filepath.Join(base, "share")
	}
}

func TestDefaultConfigFile(t *testing.T) {
	base := t.TempDir()
	homeEnvFor(t, base)

	got, err := DefaultConfigFile()
	if err != nil {
		t.Fatalf("default config file: %v", err)
	}
	want := filepath.Join(expectedConfigDir(base), "note", "note.yaml")
	if got != want {
		t.Fatalf("default config file = %q, want %q", got, want)
	}
}

func TestDefaultDBFile(t *testing.T) {
	base := t.TempDir()
	homeEnvFor(t, base)

	got, err := DefaultDBFile()
	if err != nil {
		t.Fatalf("default db file: %v", err)
	}
	want := filepath.Join(expectedDataDir(base), "note", "notes.db")
	if got != want {
		t.Fatalf("default db file = %q, want %q", got, want)
	}
}

func TestDefaultDataDirLinuxFallsBackToHome(t *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		t.Skip("XDG 回退只在 Linux/类 Unix 生效")
	}
	base := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", base)

	got, err := DefaultDataDir()
	if err != nil {
		t.Fatalf("default data dir: %v", err)
	}
	want := filepath.Join(base, ".local", "share", "note")
	if got != want {
		t.Fatalf("default data dir = %q, want %q", got, want)
	}
}
