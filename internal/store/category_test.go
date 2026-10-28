package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	st, err := Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return st
}

// insertTestNote 直接写 notes 表：Task 4 的 CreateNote 还不存在。
func insertTestNote(t *testing.T, st *Store, id string, categoryID any) {
	t.Helper()
	const ts = "2026-01-01T00:00:00Z"

	_, err := st.db.Exec(
		`INSERT INTO notes (id, title, content, created_at, updated_at, category_id)
         VALUES (?, '标题', '正文', ?, ?, ?)`,
		id, ts, ts, categoryID,
	)
	if err != nil {
		t.Fatalf("insert note %s: %v", id, err)
	}
}

func TestCategoryCRUDAndCounts(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	work, err := st.CreateCategory(ctx, "work")
	if err != nil {
		t.Fatalf("create category work: %v", err)
	}
	study, err := st.CreateCategory(ctx, "study")
	if err != nil {
		t.Fatalf("create category study: %v", err)
	}
	if work.ID <= 1 || study.ID <= work.ID {
		t.Fatalf("unexpected ids: %d, %d", work.ID, study.ID)
	}

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 3 {
		t.Fatalf("expected 3 categories, got %d", len(cats))
	}
	if cats[0].ID != 1 || cats[0].Name != "未分类" {
		t.Fatalf("expected default category first, got %+v", cats[0])
	}
	for _, c := range cats {
		if c.NoteCount != 0 {
			t.Fatalf("expected empty note_count, got %+v", c)
		}
	}

	insertTestNote(t, st, "n1", work.ID)
	insertTestNote(t, st, "n2", work.ID)
	insertTestNote(t, st, "n3", nil)

	cats, err = st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories after notes: %v", err)
	}
	if cats[0].NoteCount != 1 || cats[1].NoteCount != 2 || cats[2].NoteCount != 0 {
		t.Fatalf("unexpected note counts: %+v", cats)
	}

	if err := st.RenameCategory(ctx, study.ID, "阅读"); err != nil {
		t.Fatalf("rename category: %v", err)
	}
	cats, err = st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories after rename: %v", err)
	}
	if cats[2].Name != "阅读" {
		t.Fatalf("expected renamed category, got %+v", cats[2])
	}

	if err := st.RenameCategory(ctx, 999, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound renaming missing category, got %v", err)
	}
	if err := st.DeleteCategory(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting missing category, got %v", err)
	}
}

// 「未分类」在库里是 notes.category_id IS NULL（见 sql/0004 头注释），与 id=1 行
// 展示等价：计数与筛选都必须把 NULL 算进默认分类，否则未分类笔记既看不见也统计不到。
func TestDefaultCategoryCoversUncategorizedNotes(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	work, err := st.CreateCategory(ctx, "work")
	if err != nil {
		t.Fatalf("create category work: %v", err)
	}
	insertTestNote(t, st, "n1", nil)
	insertTestNote(t, st, "n2", nil)
	insertTestNote(t, st, "n3", work.ID)

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if cats[0].ID != 1 || cats[0].NoteCount != 2 {
		t.Fatalf("expected default category to count uncategorized notes, got %+v", cats[0])
	}

	uncat, err := st.ListNotes(ctx, NoteFilter{CategoryID: defaultCategoryID, Page: 1})
	if err != nil {
		t.Fatalf("list notes in default category: %v", err)
	}
	if uncat.Total != 2 || len(uncat.Items) != 2 {
		t.Fatalf("expected 2 uncategorized notes, got total %d items %d", uncat.Total, len(uncat.Items))
	}
	for _, item := range uncat.Items {
		if item.CategoryID != nil {
			t.Fatalf("expected nil category id, got %+v", item)
		}
	}

	all, err := st.ListNotes(ctx, NoteFilter{Page: 1})
	if err != nil {
		t.Fatalf("list all notes: %v", err)
	}
	if all.Total != 3 {
		t.Fatalf("expected 3 notes total, got %d", all.Total)
	}
}

func TestCategoryNameValidationAndUniqueness(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if _, err := st.CreateCategory(ctx, "work"); err != nil {
		t.Fatalf("create category: %v", err)
	}
	if _, err := st.CreateCategory(ctx, "WORK"); !errors.Is(err, ErrCategoryExists) {
		t.Fatalf("expected ErrCategoryExists for case-insensitive duplicate, got %v", err)
	}
	if _, err := st.CreateCategory(ctx, "work"); !errors.Is(err, ErrCategoryExists) {
		t.Fatalf("expected ErrCategoryExists for exact duplicate, got %v", err)
	}
	if _, err := st.CreateCategory(ctx, "未分类"); !errors.Is(err, ErrCategoryExists) {
		t.Fatalf("expected ErrCategoryExists for default name, got %v", err)
	}

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("expected rejected duplicates to leave 2 categories, got %d", len(cats))
	}

	if err := st.RenameCategory(ctx, cats[1].ID, "未分类"); !errors.Is(err, ErrCategoryExists) {
		t.Fatalf("expected ErrCategoryExists renaming onto default name, got %v", err)
	}
	// 改名同样要走 NOCASE 唯一约束：把另一个分类改成只差大小写的同名，是撞名不是改名。
	temp, err := st.CreateCategory(ctx, "temp")
	if err != nil {
		t.Fatalf("create category temp: %v", err)
	}
	if err := st.RenameCategory(ctx, temp.ID, "WORK"); !errors.Is(err, ErrCategoryExists) {
		t.Fatalf("expected ErrCategoryExists for case-differing rename, got %v", err)
	}
}

func TestDeleteCategoryGuards(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	if err := st.DeleteCategory(ctx, 1); !errors.Is(err, ErrReservedCategory) {
		t.Fatalf("expected ErrReservedCategory deleting default, got %v", err)
	}
	if err := st.RenameCategory(ctx, 1, "杂记"); !errors.Is(err, ErrReservedCategory) {
		t.Fatalf("expected ErrReservedCategory renaming default, got %v", err)
	}

	work, err := st.CreateCategory(ctx, "work")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	study, err := st.CreateCategory(ctx, "study")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}

	insertTestNote(t, st, "n1", work.ID)

	var notEmpty ErrCategoryNotEmpty
	if err := st.DeleteCategory(ctx, work.ID); !errors.As(err, &notEmpty) {
		t.Fatalf("expected ErrCategoryNotEmpty, got %v", err)
	}
	if notEmpty.Count != 1 {
		t.Fatalf("expected count 1, got %d", notEmpty.Count)
	}

	if _, err := st.db.Exec(`UPDATE notes SET category_id = ? WHERE id = 'n1'`, study.ID); err != nil {
		t.Fatalf("move note: %v", err)
	}
	if err := st.DeleteCategory(ctx, work.ID); err != nil {
		t.Fatalf("delete now-empty category: %v", err)
	}

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if len(cats) != 2 {
		t.Fatalf("expected 2 categories after delete, got %d", len(cats))
	}
}

func TestErrCategoryNotEmptyMessage(t *testing.T) {
	if got := (ErrCategoryNotEmpty{Count: 3}).Error(); got != "分类下仍有 3 条笔记" {
		t.Fatalf("unexpected message: %q", got)
	}
}
