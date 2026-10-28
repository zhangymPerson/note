package daemon

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestStatePathsShareDatabaseDirectory(t *testing.T) {
	db := filepath.Join("C:", "users", "me", "note", "notes.db")
	if got, want := StatePath(db), filepath.Join("C:", "users", "me", "note", "note.json"); got != want {
		t.Fatalf("StatePath = %q, want %q", got, want)
	}
	if got, want := LogPath(db), filepath.Join("C:", "users", "me", "note", "note.log"); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}

func TestSaveLoadRoundTripWithTightPermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "note.json")
	token, err := NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	want := State{
		Pid: 4242, Addr: "127.0.0.1:8080", URLs: []string{"http://127.0.0.1:8080"},
		StartedAt: "2026-10-28T09:12:33Z", DB: filepath.Join(dir, "notes.db"),
		Log: filepath.Join(dir, "note.log"), Token: token, Version: "1.5.0",
	}
	if err := Save(path, want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Pid != want.Pid || got.Addr != want.Addr || got.Token != want.Token ||
		got.StartedAt != want.StartedAt || got.Version != want.Version || len(got.URLs) != 1 {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	// Windows 的 mode 位是合成的（一律 0666），权限靠用户目录 ACL；
	// POSIX 上这里才是真正的「同一台机器上别的用户读不到令牌」。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("状态文件权限 = %o，令牌可被其他用户读取", perm)
		}
	}
}

func TestSaveFailsWhenTargetIsNotADirectory(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if err := Save(filepath.Join(blocker, "note.json"), State{Pid: 1}); err == nil {
		t.Fatal("父路径是普通文件时应报错，而不是静默成功")
	}
}

func TestSaveReplacesExistingFileAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.json")
	if err := Save(path, State{Pid: 1, Token: "a"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := Save(path, State{Pid: 2, Token: "b"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path)
	if err != nil || got.Pid != 2 || got.Token != "b" {
		t.Fatalf("load = %+v, %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("目录里残留临时文件: %v", entries)
	}
}

func TestLoadMissingFileIsNotExist(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "note.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}

func TestLoadCorruptFileReportsParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.json")
	if err := os.WriteFile(path, []byte(`{"pid": 12`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := Load(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want 损坏文件的解析错误", err)
	}
}

func TestRemoveIfPidOnlyRemovesMatchingPid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.json")
	if err := Save(path, State{Pid: 7}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := RemoveIfPid(path, 8); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("pid 不匹配时不应删除: %v", err)
	}
	if err := RemoveIfPid(path, 7); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := Load(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid 匹配时应删除，err = %v", err)
	}
	if err := RemoveIfPid(path, 7); err != nil {
		t.Fatalf("文件不存在时应返回 nil: %v", err)
	}
}

func TestNewTokenIs32BytesBase64URL(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token = %q (len %d), want 32 字节 base64url", token, len(raw))
	}
	other, err := NewToken()
	if err != nil || other == token {
		t.Fatalf("两次生成的令牌相同或出错: %v", err)
	}
}
