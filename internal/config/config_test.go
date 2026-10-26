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
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	cfg, warnings, err := Load(nil)
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.DBPath != DefaultDBPath || cfg.AllowRemote {
		t.Fatalf("unexpected defaults: %+v", cfg)
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

	cfg, _, err := Load([]string{"--config", path})
	if err != nil {
		t.Fatalf("load empty config: %v", err)
	}
	if cfg.Addr != DefaultAddr || cfg.DBPath != DefaultDBPath {
		t.Fatalf("unexpected defaults: %+v", cfg)
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

func TestLoadRejectsMissingExplicitConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.yaml")

	if _, _, err := Load([]string{"--config", path}); err == nil {
		t.Fatal("expected missing explicit config to fail")
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
