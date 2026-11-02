package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// legacySchemaVersion 是 0007 之前的最后一版 schema：迁移测试要靠它造「存量库」。
const legacySchemaVersion = 6

// openLegacyStore 建一个 user_version = 6 的库（只执行 0001–0006 的 SQL），
// 供迁移测试插入「唯一索引落不下去」的存量数据。
func openLegacyStore(t *testing.T) (*sql.DB, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=foreign_keys(ON)")
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	items, err := loadMigrations()
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	for _, item := range items {
		if item.version > legacySchemaVersion {
			continue
		}
		if _, err := db.Exec(item.body); err != nil {
			t.Fatalf("apply legacy migration %s: %v", item.name, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", legacySchemaVersion)); err != nil {
		t.Fatalf("set legacy schema version: %v", err)
	}
	return db, path
}

// insertLegacyNote 直接写 notes 表：迁移发生在 Open 里，那时还拿不到 *Store。
func insertLegacyNote(t *testing.T, db *sql.DB, id, title, updatedAt string, categoryID, deletedAt any) {
	t.Helper()

	const createdAt = "2026-01-01T00:00:00Z"
	if _, err := db.Exec(
		`INSERT INTO notes (id, title, content, created_at, updated_at, category_id, deleted_at)
         VALUES (?, ?, '正文', ?, ?, ?, ?)`,
		id, title, createdAt, updatedAt, categoryID, deletedAt,
	); err != nil {
		t.Fatalf("insert legacy note %s: %v", id, err)
	}
}

// openMigrated 打开存量库，触发 0007 迁移。
func openMigrated(t *testing.T, path string) *Store {
	t.Helper()

	st, err := Open(path)
	if err != nil {
		t.Fatalf("open store to run migration: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

func noteTitle(t *testing.T, st *Store, id string) string {
	t.Helper()

	var title string
	if err := st.db.QueryRow(`SELECT title FROM notes WHERE id = ?`, id).Scan(&title); err != nil {
		t.Fatalf("read title of %s: %v", id, err)
	}
	return title
}

func TestMigrationAddsQuickCategoryAndUniqueTitleIndexes(t *testing.T) {
	_, path := openLegacyStore(t)
	st := openMigrated(t, path)
	ctx := context.Background()

	var version int
	if err := st.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != 7 {
		t.Fatalf("expected schema version 7, got %d", version)
	}

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("expected 未分类 + 速记, got %+v", cats)
	}
	if cats[1].ID != QuickCategoryID || cats[1].Name != QuickCategoryName {
		t.Fatalf("expected quick category id=%d name=%q, got %+v", QuickCategoryID, QuickCategoryName, cats[1])
	}
	if !cats[1].Reserved || !cats[0].Reserved {
		t.Fatalf("expected both built-in categories reserved, got %+v", cats)
	}

	for _, name := range []string{"idx_notes_title_category", "idx_notes_title_uncategorized"} {
		var found string
		err := st.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'index' AND name = ?`, name,
		).Scan(&found)
		if err != nil {
			t.Fatalf("index %s missing: %v", name, err)
		}
	}
}

func TestMigrationDedupesTitlesKeepingLiveAndNewest(t *testing.T) {
	db, path := openLegacyStore(t)
	insertLegacyNote(t, db, "n1", "周报", "2026-01-09T00:00:00Z", nil, "2026-01-10T00:00:00Z")
	insertLegacyNote(t, db, "n2", "周报", "2026-01-02T00:00:00Z", nil, nil)
	insertLegacyNote(t, db, "n3", "周报", "2026-01-03T00:00:00Z", nil, nil)

	st := openMigrated(t, path)

	// 活着的、最新的保住原名；活着的旧一条与回收站里那条都改名。
	if got := noteTitle(t, st, "n3"); got != "周报" {
		t.Fatalf("expected newest live note to keep its title, got %q", got)
	}
	if got := noteTitle(t, st, "n2"); got != "周报 (2)" {
		t.Fatalf("expected second live note renamed to 周报 (2), got %q", got)
	}
	if got := noteTitle(t, st, "n1"); got != "周报 (3)" {
		t.Fatalf("expected trashed note renamed to 周报 (3), got %q", got)
	}
}

func TestMigrationGroupsTitlesCaseInsensitively(t *testing.T) {
	db, path := openLegacyStore(t)
	if _, err := db.Exec(
		`INSERT INTO categories (id, name, created_at) VALUES (5, '工作', '2026-01-01T00:00:00Z')`,
	); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	insertLegacyNote(t, db, "c1", "TODO", "2026-02-02T00:00:00Z", 5, nil)
	insertLegacyNote(t, db, "c2", "todo", "2026-02-01T00:00:00Z", 5, nil)

	st := openMigrated(t, path)

	if got := noteTitle(t, st, "c1"); got != "TODO" {
		t.Fatalf("expected newest note to keep its title, got %q", got)
	}
	if got := noteTitle(t, st, "c2"); got != "todo (2)" {
		t.Fatalf("expected case-variant duplicate to be renamed, got %q", got)
	}

	var count int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM notes WHERE category_id = 5`,
	).Scan(&count); err != nil {
		t.Fatalf("count notes in category: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected both notes to stay in category 5, got %d", count)
	}
}

func TestMigrationFallbackSuffixWhenCandidateTaken(t *testing.T) {
	db, path := openLegacyStore(t)
	insertLegacyNote(t, db, "dup00001", "周报", "2026-03-02T00:00:00Z", nil, nil)
	insertLegacyNote(t, db, "dup00002", "周报", "2026-03-01T00:00:00Z", nil, nil)
	// 用户手写的「周报 (2)」占住了默认后缀，落选那条必须换一个名字。
	insertLegacyNote(t, db, "taken001", "周报 (2)", "2026-03-03T00:00:00Z", nil, nil)

	st := openMigrated(t, path)

	renamed := noteTitle(t, st, "dup00002")
	if !strings.HasPrefix(renamed, "周报 (2 · ") || !strings.HasSuffix(renamed, ")") {
		t.Fatalf("expected fallback suffix with id prefix, got %q", renamed)
	}
	if !strings.Contains(renamed, "dup00002"[:8]) {
		t.Fatalf("expected fallback suffix to carry the note id prefix, got %q", renamed)
	}
	if got := noteTitle(t, st, "taken001"); got != "周报 (2)" {
		t.Fatalf("expected the user's own title to stay untouched, got %q", got)
	}
}

func TestMigrationTruncatesLongRenamedTitles(t *testing.T) {
	long := strings.Repeat("字", 200)
	db, path := openLegacyStore(t)
	insertLegacyNote(t, db, "long0001", long, "2026-04-02T00:00:00Z", nil, nil)
	insertLegacyNote(t, db, "long0002", long, "2026-04-01T00:00:00Z", nil, nil)

	st := openMigrated(t, path)

	renamed := noteTitle(t, st, "long0002")
	if !strings.HasSuffix(renamed, " (2)") {
		t.Fatalf("expected a (2) suffix, got %q", renamed)
	}
	if got := utf8.RuneCountInString(renamed); got != 200 {
		t.Fatalf("expected renamed title to be truncated to 200 runes, got %d", got)
	}
	if got := noteTitle(t, st, "long0001"); got != long {
		t.Fatalf("expected the winner to keep the full title")
	}
}
