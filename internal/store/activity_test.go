package store

import (
	"context"
	"errors"
	"testing"
)

func kindCount(t *testing.T, st *Store, kind string) int {
	t.Helper()

	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM activities WHERE kind = ?`, kind).Scan(&count); err != nil {
		t.Fatalf("count %s activities: %v", kind, err)
	}
	return count
}

func TestActivityEventsRecordedForNoteAndCategoryCRUD(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	cat, err := st.CreateCategory(ctx, "工作")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	if err := st.RenameCategory(ctx, cat.ID, "工作台"); err != nil {
		t.Fatalf("rename category: %v", err)
	}

	created, err := st.CreateNote(ctx, NoteInput{Title: "周报", Content: &body, CategoryID: &cat.ID})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := st.UpdateNote(ctx, created.ID, NoteInput{Title: "周报二", Content: &body}); err != nil {
		t.Fatalf("update note: %v", err)
	}
	if _, err := st.ViewNote(ctx, created.ID); err != nil {
		t.Fatalf("view note: %v", err)
	}
	if _, err := st.ListNotes(ctx, NoteFilter{Q: "周报", Page: 1}); err != nil {
		t.Fatalf("search notes: %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, created.ID); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if err := st.DeleteCategory(ctx, cat.ID); err != nil {
		t.Fatalf("delete category: %v", err)
	}

	for _, tc := range []struct {
		kind string
		want int
	}{
		{ActivityCategoryCreate, 1},
		{ActivityCategoryRename, 1},
		{ActivityCategoryDelete, 1},
		{ActivityNoteCreate, 1},
		{ActivityNoteUpdate, 1},
		{ActivityNoteDelete, 1},
		{ActivityNoteView, 1},
		{ActivitySearch, 1},
	} {
		if got := kindCount(t, st, tc.kind); got != tc.want {
			t.Fatalf("%s events: expected %d, got %d", tc.kind, tc.want, got)
		}
	}

	stats, err := st.GetStats(ctx, 7)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.TotalEvents != 8 {
		t.Fatalf("expected 8 total events, got %d", stats.TotalEvents)
	}
	if stats.TotalSearches != 1 {
		t.Fatalf("expected 1 search, got %d", stats.TotalSearches)
	}
	if stats.ActiveDays != 1 {
		t.Fatalf("expected 1 active day, got %d", stats.ActiveDays)
	}
	if len(stats.Days) != 1 || stats.Days[0].Count != 8 {
		t.Fatalf("expected today to aggregate all 8 events, got %+v", stats.Days)
	}
	if len(stats.EventsByKind) != 8 {
		t.Fatalf("expected 8 kind buckets, got %+v", stats.EventsByKind)
	}
	for _, item := range stats.EventsByKind {
		if item.Count != 1 {
			t.Fatalf("expected every kind once, got %+v", stats.EventsByKind)
		}
	}
	if len(stats.TopQueries) != 1 || stats.TopQueries[0].Query != "周报" || stats.TopQueries[0].Count != 1 {
		t.Fatalf("expected top query 周报, got %+v", stats.TopQueries)
	}
}

func TestActivityEventsSkippedWhenWriteFails(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	cat, err := st.CreateCategory(ctx, "临时")
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	note, err := st.CreateNote(ctx, NoteInput{Title: "草稿", Content: &body, CategoryID: &cat.ID})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	// 失败路径不能留下事件：删除还有笔记的分类、更新不存在的笔记
	if err := st.DeleteCategory(ctx, cat.ID); !errors.As(err, &ErrCategoryNotEmpty{}) {
		t.Fatalf("expected category_not_empty, got %v", err)
	}
	if _, err := st.UpdateNote(ctx, "8f1a1a1a-0000-4000-8000-000000000000", NoteInput{Title: "无", Content: &body}); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected note not found, got %v", err)
	}
	if _, err := st.SoftDeleteNote(ctx, "8f1a1a1a-0000-4000-8000-000000000000"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected note not found, got %v", err)
	}
	if err := st.RenameCategory(ctx, defaultCategoryID, "改名"); !errors.Is(err, ErrReservedCategory) {
		t.Fatalf("expected reserved category, got %v", err)
	}

	for _, kind := range []string{ActivityNoteUpdate, ActivityNoteDelete, ActivityCategoryDelete, ActivityCategoryRename} {
		if got := kindCount(t, st, kind); got != 0 {
			t.Fatalf("%s should not be recorded on failed write, got %d", kind, got)
		}
	}

	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("delete note: %v", err)
	}
	if err := st.DeleteCategory(ctx, cat.ID); err != nil {
		t.Fatalf("delete category: %v", err)
	}
	if got := kindCount(t, st, ActivityCategoryDelete); got != 1 {
		t.Fatalf("expected 1 category delete event, got %d", got)
	}
}

// 「查看」事件只在用户打开笔记时记：内部回读（GetNote）、取不存在的 id、
// 回收站里的笔记都不记。
func TestViewNoteRecordsOnlyUserReads(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	body := "正文"
	note, err := st.CreateNote(ctx, NoteInput{Title: "路线", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if got := kindCount(t, st, ActivityNoteView); got != 0 {
		t.Fatalf("CreateNote 成功后内部回读不该记查看，got %d", got)
	}

	if _, err := st.GetNote(ctx, note.ID); err != nil {
		t.Fatalf("get note: %v", err)
	}
	if got := kindCount(t, st, ActivityNoteView); got != 0 {
		t.Fatalf("GetNote 是内部读取，不该记查看，got %d", got)
	}

	if _, err := st.ViewNote(ctx, note.ID); err != nil {
		t.Fatalf("view note: %v", err)
	}
	if got := kindCount(t, st, ActivityNoteView); got != 1 {
		t.Fatalf("ViewNote 应记 1 次查看，got %d", got)
	}

	if _, err := st.ViewNote(ctx, "8f1a1a1a-0000-4000-8000-000000000000"); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("expected note not found, got %v", err)
	}
	if got := kindCount(t, st, ActivityNoteView); got != 1 {
		t.Fatalf("失败的查看不该记事件，got %d", got)
	}

	if _, err := st.SoftDeleteNote(ctx, note.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	if _, err := st.ViewNote(ctx, note.ID); err != nil {
		t.Fatalf("view trashed note: %v", err)
	}
	if got := kindCount(t, st, ActivityNoteView); got != 1 {
		t.Fatalf("回收站里的笔记不该记查看，got %d", got)
	}
}
