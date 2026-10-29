package template

import (
	"os"
	"runtime"
	"path/filepath"
	"strings"
	"testing"
)

func TestPathSitsNextToConfigFile(t *testing.T) {
	got := Path(filepath.Join("/home", "me", ".config", "note", "note.yaml"))
	want := filepath.Join("/home", "me", ".config", "note", "note-example.md")
	if got != want {
		t.Fatalf("Path() = %q, want %q", got, want)
	}
}

func TestDefaultIsSimpleMarkdownNote(t *testing.T) {
	body := Default()
	if strings.TrimSpace(body) == "" {
		t.Fatal("内置默认模板不能为空")
	}
	for _, marker := range []string{"# ", "- ", "```"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("默认模板缺少 markdown 标记 %q", marker)
		}
	}
}

func TestEnsureCreatesFileOnceAndNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", FileName)

	created, err := Ensure(path)
	if err != nil || !created {
		t.Fatalf("首次 Ensure() = %v, %v; want created=true, nil", created, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != Default() {
		t.Fatalf("首次创建的内容应为内置默认值: %v", err)
	}

	if err := os.WriteFile(path, []byte("# 我改过的模板"), 0o644); err != nil {
		t.Fatalf("准备自定义模板: %v", err)
	}
	created, err = Ensure(path)
	if err != nil || created {
		t.Fatalf("二次 Ensure() = %v, %v; want created=false, nil", created, err)
	}
	data, err = os.ReadFile(path)
	if err != nil || string(data) != "# 我改过的模板" {
		t.Fatalf("Ensure 覆盖了用户模板: %q, %v", data, err)
	}
}

func TestLoadFallsBackToDefaultWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	content, fromDefault, err := Load(path)
	if err != nil || !fromDefault || content != Default() {
		t.Fatalf("Load(缺失) = %q, %v, %v; want 内置默认值, true, nil", content, fromDefault, err)
	}
}

func TestLoadReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(path, []byte("# 自定义"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, fromDefault, err := Load(path)
	if err != nil || fromDefault || content != "# 自定义" {
		t.Fatalf("Load(存在) = %q, %v, %v", content, fromDefault, err)
	}
}

func TestSaveWritesAtomicallyAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)

	if err := Save(path, "# 第一版"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(path, "# 第二版"); err != nil {
		t.Fatalf("Save 覆盖: %v", err)
	}
	content, fromDefault, err := Load(path)
	if err != nil || fromDefault || content != "# 第二版" {
		t.Fatalf("Save 后内容 = %q, %v, %v", content, fromDefault, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("目录里只剩模板文件，发现临时文件残留: %v", names)
	}

	// Windows 没有 POSIX 权限位，Stat 只会汇报只读属性，这里只校验内容与文件名。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o644 {
			t.Fatalf("模板文件权限 = %o, want 644", mode)
		}
	}
}
