package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "note.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadDefaultsWhenDefaultConfigMissing(t *testing.T) {
	base := t.TempDir()
	homeEnvFor(t, base)

	cfg, warnings, err := Load(nil)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.AllowRemote || cfg.ConfigFound {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	wantConfig, err := DefaultConfigFile()
	if err != nil || cfg.ConfigPath != wantConfig {
		t.Fatalf("config path = %q, want %q (err %v)", cfg.ConfigPath, wantConfig, err)
	}
	wantDB, err := DefaultDBFile()
	if err != nil || cfg.DBPath != wantDB {
		t.Fatalf("db path = %q, want %q (err %v)", cfg.DBPath, wantDB, err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %v", warnings)
	}
}

func TestLoadFileAndFlagOverrides(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 127.0.0.1:9090
  allow_remote: false
database:
  path: ./tmp/test.db
`)

	cfg, warnings, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load file config: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" || cfg.DBPath != "./tmp/test.db" {
		t.Fatalf("unexpected file config: %+v", cfg)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	cfg, _, err = Load([]string{"--config", path, "--addr", "127.0.0.1:9999", "--db", "./override.db"})
	if err != nil {
		t.Fatalf("load overrides: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9999" || cfg.DBPath != "./override.db" {
		t.Fatalf("flags did not override config: %+v", cfg)
	}
}

func TestLoadEmptyConfigUsesDefaults(t *testing.T) {
	path := writeConfig(t, "")
	base := t.TempDir()
	homeEnvFor(t, base)

	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	wantDB, err := DefaultDBFile()
	if err != nil {
		t.Fatalf("default db: %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.DBPath != wantDB {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if !cfg.ConfigFound {
		t.Fatal("expected ConfigFound for an existing file")
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 127.0.0.1:8080
  unknown: true
`)

	if _, _, err := Load([]string{"--config", path}); err == nil {
		t.Fatal("expected unknown key to fail")
	}
}

func TestLoadRejectsNonLoopbackWithoutAllowRemote(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 0.0.0.0:8080
`)

	if _, _, err := Load([]string{"--config", path}); err == nil {
		t.Fatal("expected non-loopback address to fail")
	}

	cfg, warnings, err := Load([]string{"--config", path, "--allow-remote"})
	if err != nil {
		t.Fatalf("allow remote: %v", err)
	}
	if !cfg.AllowRemote {
		t.Fatal("expected allow remote to be enabled")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "remote binding") {
		t.Fatalf("expected remote binding warning, got %v", warnings)
	}
}

func TestLoadPortOverridesPortOnly(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 127.0.0.1:9090
database:
  path: ./tmp/test.db
`)

	// --port 只换端口，host 沿用配置文件
	cfg, _, err := Load([]string{"--config", path, "--port", "7070"})
	if err != nil {
		t.Fatalf("port override: %v", err)
	}
	if cfg.Addr != "127.0.0.1:7070" {
		t.Fatalf("addr = %q, want 127.0.0.1:7070", cfg.Addr)
	}

	// -p 短参数等价；同时给 --addr 时 host 取 --addr
	cfg, _, err = Load([]string{"--config", path, "--addr", "localhost:8080", "-p", "7071"})
	if err != nil {
		t.Fatalf("short port override: %v", err)
	}
	if cfg.Addr != "localhost:7071" {
		t.Fatalf("addr = %q, want localhost:7071", cfg.Addr)
	}

	// 未提供 --port 时端口原样保留
	cfg, _, err = Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("no port override: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Fatalf("addr = %q, want 127.0.0.1:9090", cfg.Addr)
	}

	// IPv6 host 换端口后仍带方括号
	cfg, _, err = Load([]string{"--config", path, "--addr", "[::1]:8080", "--port", "7072"})
	if err != nil {
		t.Fatalf("ipv6 port override: %v", err)
	}
	if cfg.Addr != "[::1]:7072" {
		t.Fatalf("addr = %q, want [::1]:7072", cfg.Addr)
	}
}

func TestLoadRejectsInvalidPortOverride(t *testing.T) {
	path := writeConfig(t, "")

	if _, _, err := Load([]string{"--config", path, "--port", "0"}); err == nil {
		t.Fatal("expected port 0 to fail validation")
	}
	if _, _, err := Load([]string{"--config", path, "--port", "70000"}); err == nil {
		t.Fatal("expected port 70000 to fail validation")
	}
}

func TestLoadRejectsUnexpectedPositional(t *testing.T) {
	path := writeConfig(t, "")

	if _, _, err := Load([]string{"--config", path, "extra"}); err == nil {
		t.Fatal("expected unexpected positional to fail")
	}
}

func TestLoadRemoteAliasForAllowRemote(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 0.0.0.0:8080
`)

	cfg, warnings, err := Load([]string{"--config", path, "--remote"})
	if err != nil {
		t.Fatalf("remote alias: %v", err)
	}
	if !cfg.AllowRemote {
		t.Fatal("expected --remote to enable AllowRemote")
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "remote binding") {
		t.Fatalf("expected remote binding warning, got %v", warnings)
	}
}

func TestLoadRejectsMissingExplicitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	if _, _, err := Load([]string{"--config", path}); err == nil {
		t.Fatal("expected missing explicit config to fail")
	}
}

func TestLoadShortConfigFlag(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 127.0.0.1:9191
database:
  path: ./tmp/test.db
`)

	// -c 与 --config 同义
	cfg, _, err := Load([]string{"-c", path})
	if err != nil {
		t.Fatalf("short config flag: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9191" || cfg.ConfigPath != path || !cfg.ConfigFound {
		t.Fatalf("unexpected config: %+v", cfg)
	}

	// 显式 -c 指向不存在的文件仍应报错
	missing := filepath.Join(t.TempDir(), "missing.yaml")
	if _, _, err := Load([]string{"-c", missing}); err == nil {
		t.Fatal("expected missing explicit -c config to fail")
	}
}

func TestLoadUserSection(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 127.0.0.1:8080
database:
  path: ./data/notes.db
user:
  name: 阿澜
  name_set: true
`)

	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load user config: %v", err)
	}
	if cfg.UserName != "阿澜" || !cfg.NameSet {
		t.Fatalf("unexpected user config: %+v", cfg)
	}
}

func TestSaveUserNameRoundTrip(t *testing.T) {
	path := writeConfig(t, `server:
  addr: 127.0.0.1:9090
database:
  path: ./data/notes.db
user:
  name: ""
  name_set: false
`)

	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load before save: %v", err)
	}
	if err := cfg.SaveUserName("小北"); err != nil {
		t.Fatalf("save name: %v", err)
	}

	reloaded, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load after save: %v", err)
	}
	if reloaded.UserName != "小北" || !reloaded.NameSet {
		t.Fatalf("name not persisted: %+v", reloaded)
	}
	if reloaded.Addr != "127.0.0.1:9090" || reloaded.DBPath != "./data/notes.db" {
		t.Fatalf("save lost other settings: %+v", reloaded)
	}
}
