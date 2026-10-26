package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCreatesDatabaseAndAppliesMigrations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})

	var version int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected schema version 1, got %d", version)
	}

	var mode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("expected WAL journal mode, got %q", mode)
	}

	var name string
	err = store.db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'notes'").Scan(&name)
	if err != nil {
		t.Fatalf("find notes table: %v", err)
	}
}

func TestOpenExistingDatabaseIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")

	first, err := Open(path)
	if err != nil {
		t.Fatalf("open first store: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("close second store: %v", err)
		}
	})

	var version int
	if err := second.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected schema version 1, got %d", version)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("open raw database: %v", err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw database: %v", err)
	}

	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected newer-schema error, got %v", err)
	}
}

func TestOpenRejectsInvalidDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o600); err != nil {
		t.Fatalf("write invalid database: %v", err)
	}

	if _, err := Open(path); err == nil {
		t.Fatal("expected invalid database to be rejected")
	}
}
