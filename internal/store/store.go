package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	migrations "note/sql"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is empty")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}

	_, statErr := os.Stat(abs)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("stat database: %w", statErr)
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=secure_delete(ON)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)",
		filepath.ToSlash(abs),
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if err := s.initialize(exists); err != nil {
		db.Close()
		return nil, err
	}
	if !exists {
		slog.Info("database initialized", "path", abs)
	}

	if err := os.Chmod(abs, 0o600); err != nil {
		slog.Warn("could not set database permissions", "error", err)
	}

	return s, nil
}

func (s *Store) Close() error {
	var errs []error
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		errs = append(errs, fmt.Errorf("wal checkpoint: %w", err))
	}
	if err := s.db.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (s *Store) initialize(fresh bool) error {
	if !fresh {
		var result string
		if err := s.db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
			return fmt.Errorf("database integrity check: %w", err)
		}
		if result != "ok" {
			return fmt.Errorf("database integrity check failed: %s", result)
		}
	}
	return s.migrate()
}

type migration struct {
	version int
	name    string
	body    string
}

func (s *Store) migrate() error {
	items, err := loadMigrations()
	if err != nil {
		return err
	}

	latest := 0
	if len(items) > 0 {
		latest = items[len(items)-1].version
	}

	var current int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if current > latest {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, latest)
	}
	if current == latest {
		return nil
	}

	for _, item := range items {
		if item.version <= current {
			continue
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", item.name, err)
		}
		if _, err := tx.Exec(item.body); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", item.name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", item.version)); err != nil {
			tx.Rollback()
			return fmt.Errorf("set schema version %d: %w", item.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", item.name, err)
		}
		slog.Info("database migrated", "version", item.version)
	}

	return nil
}

func loadMigrations() ([]migration, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}

	items := make([]migration, 0, len(names))
	seen := make(map[int]string, len(names))

	for _, name := range names {
		version, err := parseVersion(name)
		if err != nil {
			return nil, err
		}
		if other, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %d: %s and %s", version, other, name)
		}
		seen[version] = name

		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		items = append(items, migration{version: version, name: name, body: string(body)})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].version < items[j].version
	})

	return items, nil
}

func parseVersion(name string) (int, error) {
	base := strings.TrimSuffix(name, filepath.Ext(name))
	prefix, _, ok := strings.Cut(base, "_")
	if !ok {
		return 0, fmt.Errorf("migration %s: expected <version>_<name>.sql", name)
	}

	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("migration %s: invalid version %q", name, prefix)
	}

	return version, nil
}
