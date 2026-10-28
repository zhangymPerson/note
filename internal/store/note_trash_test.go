package store

import (
	"context"
	"errors"
	"testing"
)

func TestSoftDeleteHidesNoteFromEveryDefaultRead(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "回收站正文"
	cat, err := st.CreateCategory(ctx, "工作")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	note, err := st.CreateNote(ctx, NoteInput{Title: "要删的笔记", Content: &body, CategoryID: &cat.ID})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	active, err := st.ListNotes(ctx, NoteFilter{Page: 1})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if active.Total != 0 || len(active.Items) != 0 {
		t.Fatalf("trashed note must not appear in the default list: %+v", active)
	}

	found, err := st.ListNotes(ctx, NoteFilter{Q: "要删的", Page: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if found.Total != 0 {
		t.Fatalf("trashed note must not be searchable: %+v", found)
	}

	cats, err := st.ListCategories(ctx)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	for _, item := range cats {
		if item.ID == cat.ID && item.NoteCount != 0 {
			t.Fatalf("category note_count must ignore trashed notes, got %d", item.NoteCount)
		}
	}

	trash, err := st.ListNotes(ctx, NoteFilter{Page: 1, Deleted: true})
	if err != nil {
		t.Fatalf("list trash: %v", err)
	}
	if trash.Total != 1 || len(trash.Items) != 1 || trash.Items[0].ID != note.ID {
		t.Fatalf("expected the note in the recycle bin, got %+v", trash)
	}
	if trash.Items[0].DeletedAt == nil {
		t.Fatal("expected deleted_at on the recycle bin row")
	}
}

func TestRestoreNoteKeepsCategoryAndTimestamps(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	cat, err := st.CreateCategory(ctx, "生活")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	note, err := st.CreateNote(ctx, NoteInput{Title: "买牛奶", Content: &body, CategoryID: &cat.ID})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	restored, err := st.RestoreNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.DeletedAt != nil {
		t.Fatalf("expected deleted_at cleared, got %q", *restored.DeletedAt)
	}
	if restored.CategoryID == nil || *restored.CategoryID != cat.ID {
		t.Fatalf("expected category %d to survive, got %+v", cat.ID, restored.CategoryID)
	}
	if restored.UpdatedAt != note.UpdatedAt || restored.Rev != note.Rev {
		t.Fatalf("restore must not touch updated_at/rev: %+v vs %+v", restored, note)
	}

	active, err := st.ListNotes(ctx, NoteFilter{Page: 1})
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if active.Total != 1 || active.Items[0].ID != note.ID {
		t.Fatalf("expected the note back in the default list, got %+v", active)
	}
}

func TestRestoreAndPurgeGuards(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	note, err := st.CreateNote(ctx, NoteInput{Title: "守卫", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// 未进回收站的笔记不能恢复/彻底删除：409 而不是 404，前端才能给出可读提示
	if _, err := st.RestoreNote(ctx, note.ID); !errors.Is(err, ErrNoteNotInTrash) {
		t.Fatalf("expected ErrNoteNotInTrash on restore, got %v", err)
	}
	if err := st.PurgeNote(ctx, note.ID); !errors.Is(err, ErrNoteNotInTrash) {
		t.Fatalf("expected ErrNoteNotInTrash on purge, got %v", err)
	}
	// 不存在的一律 404
	if _, err := st.RestoreNote(ctx, "missing-id"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on restore of missing note, got %v", err)
	}
	if err := st.PurgeNote(ctx, "missing-id"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on purge of missing note, got %v", err)
	}

	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	// 重复删除也是 404（已经不在活动列表里了）
	if _, err := st.SoftDeleteNote(ctx, note.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on repeated delete, got %v", err)
	}
	if err := st.PurgeNote(ctx, note.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, err := st.RestoreNote(ctx, note.ID); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected ErrNoteNotFound on restore after purge, got %v", err)
	}
}

func TestSoftDeleteRecordsEvents(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	note, err := st.CreateNote(ctx, NoteInput{Title: "事件", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := st.RestoreNote(ctx, note.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete again: %v", err)
	}
	if err := st.PurgeNote(ctx, note.ID); err != nil {
		t.Fatalf("purge: %v", err)
	}

	for _, tc := range []struct {
		kind string
		want int
	}{
		{ActivityNoteCreate, 1},
		{ActivityNoteDelete, 2},
		{ActivityNoteRestore, 1},
		{ActivityNotePurge, 1},
	} {
		if got := kindCount(t, st, tc.kind); got != tc.want {
			t.Fatalf("%s events: expected %d, got %d", tc.kind, tc.want, got)
		}
	}
}

func TestDeleteCategoryWithOnlyTrashedNotesDetachesThem(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	cat, err := st.CreateCategory(ctx, "临时分类")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	note, err := st.CreateNote(ctx, NoteInput{Title: "归档", Content: &body, CategoryID: &cat.ID})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// 分类里只剩回收站笔记时允许删除分类（计数只算未删除的笔记）
	if err := st.DeleteCategory(ctx, cat.ID); err != nil {
		t.Fatalf("delete category with only trashed notes: %v", err)
	}
	restored, err := st.RestoreNote(ctx, note.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.CategoryID != nil {
		t.Fatalf("expected detached category after category delete, got %d", *restored.CategoryID)
	}
	// category_id IS NULL 在前端与「未分类」等价（同现有列表口径），后端不伪造名字
	if restored.CategoryName != "" {
		t.Fatalf("expected empty category_name for detached note, got %q", restored.CategoryName)
	}
}
