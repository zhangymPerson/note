package store

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
)

func titleTestCategory(t *testing.T, st *Store, name string) *int64 {
	t.Helper()

	cat, err := st.CreateCategory(context.Background(), name)
	if err != nil {
		t.Fatalf("create category %s: %v", name, err)
	}
	return &cat.ID
}

func TestUpsertNoteByTitleCreatesThenOverwrites(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	first, created, err := st.UpsertNoteByTitle(ctx, nil, "周报", "第一版")
	if err != nil {
		t.Fatalf("upsert first: %v", err)
	}
	if !created || first.Rev != 1 {
		t.Fatalf("expected a new note with rev 1, got created=%v rev=%d", created, first.Rev)
	}

	second, created, err := st.UpsertNoteByTitle(ctx, nil, "周报", "第二版")
	if err != nil {
		t.Fatalf("upsert second: %v", err)
	}
	if created {
		t.Fatal("expected the second upsert to overwrite")
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same note to be overwritten, got %s and %s", first.ID, second.ID)
	}
	if second.Rev != 2 {
		t.Fatalf("expected rev 2 after overwrite, got %d", second.Rev)
	}
	if second.Content == nil || *second.Content != "第二版" {
		t.Fatalf("expected content to be replaced, got %+v", second.Content)
	}
	if got := kindCount(t, st, ActivityNoteCreate); got != 1 {
		t.Fatalf("expected 1 note_create activity, got %d", got)
	}
	if got := kindCount(t, st, ActivityNoteUpdate); got != 1 {
		t.Fatalf("expected 1 note_update activity, got %d", got)
	}
}

func TestUpsertNoteByTitleRefusesTrashedTitle(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	note, _, err := st.UpsertNoteByTitle(ctx, nil, "周报", "原正文")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, _, err := st.UpsertNoteByTitle(ctx, nil, "周报", "新正文"); !errors.Is(err, ErrNoteTitleInTrash) {
		t.Fatalf("expected ErrNoteTitleInTrash, got %v", err)
	}

	trashed, err := st.GetNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("get note: %v", err)
	}
	if trashed.Content == nil || *trashed.Content != "原正文" {
		t.Fatalf("trashed note content must stay untouched, got %+v", trashed.Content)
	}
}

func TestUpsertNoteByTitleRefusesEncryptedNote(t *testing.T) {
	st := noteTestStore(t)
	configureMaster(t, st)
	ctx := context.Background()

	cipherB64 := base64.StdEncoding.EncodeToString(testCipher())
	if _, err := st.CreateNote(ctx, NoteInput{
		Title:     "加密周报",
		CipherB64: cipherB64,
		KDFParams: testKDFParams,
	}); err != nil {
		t.Fatalf("create encrypted note: %v", err)
	}

	if _, _, err := st.UpsertNoteByTitle(ctx, nil, "加密周报", "明文覆盖"); !errors.Is(err, ErrEncryptedImmutable) {
		t.Fatalf("expected ErrEncryptedImmutable, got %v", err)
	}
}

func TestUpsertNoteByTitleMatchesCaseInsensitively(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	first, _, err := st.UpsertNoteByTitle(ctx, nil, "TODO", "第一版")
	if err != nil {
		t.Fatalf("upsert TODO: %v", err)
	}
	second, created, err := st.UpsertNoteByTitle(ctx, nil, "todo", "第二版")
	if err != nil {
		t.Fatalf("upsert todo: %v", err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("expected case-insensitive match on the same note, got created=%v %s vs %s", created, second.ID, first.ID)
	}

	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE title = 'TODO' COLLATE NOCASE`).Scan(&count); err != nil {
		t.Fatalf("count notes: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected a single note, got %d", count)
	}
}

func TestFindNoteByTitleReturnsTrashedRows(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	note, _, err := st.UpsertNoteByTitle(ctx, nil, "周报", "正文")
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	found, err := st.FindNoteByTitle(ctx, nil, "周报")
	if err != nil {
		t.Fatalf("find trashed note: %v", err)
	}
	if found.DeletedAt == nil {
		t.Fatal("expected DeletedAt to be set so callers can tell it is in trash")
	}

	if _, err := st.FindNoteByTitle(ctx, nil, "没有这条"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound, got %v", err)
	}
}

func TestSoftDeleteNoteByTitleTrashesOnce(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	if _, _, err := st.UpsertNoteByTitle(ctx, nil, "周报", "正文"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	deleted, err := st.SoftDeleteNoteByTitle(ctx, nil, "周报")
	if err != nil {
		t.Fatalf("soft delete by title: %v", err)
	}
	if deleted.DeletedAt == nil {
		t.Fatal("expected the note to be in trash")
	}

	if _, err := st.SoftDeleteNoteByTitle(ctx, nil, "周报"); !errors.Is(err, ErrNoteTitleInTrash) {
		t.Fatalf("expected ErrNoteTitleInTrash on second delete, got %v", err)
	}
	if _, err := st.SoftDeleteNoteByTitle(ctx, nil, "没有这条"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound, got %v", err)
	}
}

func TestSoftDeleteCategoryNotesOnlyTouchesLiveNotes(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	work := titleTestCategory(t, st, "工作")
	for _, title := range []string{"周报", "月报", "季报"} {
		if _, _, err := st.UpsertNoteByTitle(ctx, work, title, "正文"); err != nil {
			t.Fatalf("upsert %s: %v", title, err)
		}
	}
	// 已经软删的一条 + 另一个分类的一条：都不该被这一次删除影响。
	if _, err := st.SoftDeleteNoteByTitle(ctx, work, "月报"); err != nil {
		t.Fatalf("pre-delete: %v", err)
	}
	if _, _, err := st.UpsertNoteByTitle(ctx, nil, "未分类的一条", "正文"); err != nil {
		t.Fatalf("upsert uncategorized: %v", err)
	}

	deleted, err := st.SoftDeleteCategoryNotes(ctx, work)
	if err != nil {
		t.Fatalf("soft delete category notes: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("expected 2 live notes deleted, got %d", deleted)
	}

	var live int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM notes WHERE category_id = ? AND deleted_at IS NULL`, *work,
	).Scan(&live); err != nil {
		t.Fatalf("count live notes: %v", err)
	}
	if live != 0 {
		t.Fatalf("expected no live notes left in category, got %d", live)
	}

	var uncategorized int
	if err := st.db.QueryRow(
		`SELECT COUNT(*) FROM notes WHERE category_id IS NULL AND deleted_at IS NULL`,
	).Scan(&uncategorized); err != nil {
		t.Fatalf("count uncategorized notes: %v", err)
	}
	if uncategorized != 1 {
		t.Fatalf("expected uncategorized note to survive, got %d", uncategorized)
	}
}

func TestListNotesInCategoryOrdersByUpdatedAt(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	work := titleTestCategory(t, st, "工作")
	for _, title := range []string{"早", "晚", "中"} {
		if _, _, err := st.UpsertNoteByTitle(ctx, work, title, "正文"); err != nil {
			t.Fatalf("upsert %s: %v", title, err)
		}
	}
	for title, stamp := range map[string]string{
		"早": "2026-01-01T00:00:00Z",
		"中": "2026-01-02T00:00:00Z",
		"晚": "2026-01-03T00:00:00Z",
	} {
		if _, err := st.db.Exec(
			`UPDATE notes SET updated_at = ? WHERE title = ?`, stamp, title,
		); err != nil {
			t.Fatalf("stamp %s: %v", title, err)
		}
	}
	if _, _, err := st.UpsertNoteByTitle(ctx, nil, "未分类的一条", "正文"); err != nil {
		t.Fatalf("upsert uncategorized: %v", err)
	}

	items, err := st.ListNotesInCategory(ctx, *work)
	if err != nil {
		t.Fatalf("list notes in category: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 notes, got %d: %+v", len(items), items)
	}
	want := []string{"晚", "中", "早"}
	for i, title := range want {
		if items[i].Title != title {
			t.Fatalf("expected %v, got %+v", want, items)
		}
	}

	uncategorized, err := st.ListNotesInCategory(ctx, DefaultCategoryID)
	if err != nil {
		t.Fatalf("list uncategorized: %v", err)
	}
	if len(uncategorized) != 1 || uncategorized[0].Title != "未分类的一条" {
		t.Fatalf("expected only the uncategorized note, got %+v", uncategorized)
	}
}

func TestCreateAndUpdateNoteMapTitleConflict(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	body := "正文"

	work := titleTestCategory(t, st, "工作")
	other := titleTestCategory(t, st, "生活")

	first, err := st.CreateNote(ctx, NoteInput{Title: "周报", Content: &body, CategoryID: work})
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if _, err := st.CreateNote(ctx, NoteInput{Title: "周报", Content: &body, CategoryID: work}); !errors.Is(err, ErrNoteTitleExists) {
		t.Fatalf("expected ErrNoteTitleExists for duplicate title, got %v", err)
	}
	// 换一个分类就是另一条：唯一索引是 (分类, 标题)。
	if _, err := st.CreateNote(ctx, NoteInput{Title: "周报", Content: &body, CategoryID: other}); err != nil {
		t.Fatalf("create same title in another category: %v", err)
	}

	renamed := "月报"
	if _, err := st.CreateNote(ctx, NoteInput{Title: renamed, Content: &body, CategoryID: work}); err != nil {
		t.Fatalf("create second: %v", err)
	}
	if _, err := st.UpdateNote(ctx, first.ID, NoteInput{Title: renamed, Content: &body, CategoryID: work}); !errors.Is(err, ErrNoteTitleExists) {
		t.Fatalf("expected ErrNoteTitleExists when renaming onto a taken title, got %v", err)
	}

	// 撞上回收站里的那条：调用方要能区分「活着已有」与「在回收站里」。
	if _, err := st.SoftDeleteNote(ctx, first.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := st.CreateNote(ctx, NoteInput{Title: "周报", Content: &body, CategoryID: work}); !errors.Is(err, ErrNoteTitleInTrash) {
		t.Fatalf("expected ErrNoteTitleInTrash, got %v", err)
	}
}
