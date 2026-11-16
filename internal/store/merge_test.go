package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestMergeNotes(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()

	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: strPtr("乙")})
	if err != nil {
		t.Fatal(err)
	}
	before := kindCount(t, st, ActivityNoteCreate)

	n, err := st.MergeNotes(ctx, []string{a.ID, b.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	const wantJoined = "甲\n\n---\n\n乙"
	if n.Content == nil || *n.Content != wantJoined {
		t.Fatalf("合并正文 = %+v, want %q", n.Content, wantJoined)
	}
	if n.CategoryID != nil {
		t.Fatalf("合并笔记应在未分类, category = %+v", n.CategoryID)
	}
	if !regexp.MustCompile(`^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`).MatchString(n.Title) {
		t.Fatalf("标题格式不符: %q", n.Title)
	}
	if kindCount(t, st, ActivityNoteCreate) != before+1 {
		t.Fatalf("expected 1 note_create for merge, got %d", kindCount(t, st, ActivityNoteCreate)-before)
	}

	// 原文一行不写：仍在原分类、正文原样。
	got, err := st.GetNote(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content == nil || *got.Content != "甲" {
		t.Fatalf("原文 A 被改动: %+v", got.Content)
	}
	gotB, err := st.GetNote(ctx, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotB.Content == nil || *gotB.Content != "乙" {
		t.Fatalf("原文 B 被改动: %+v", gotB.Content)
	}
}

func TestMergeNotesDefaultTitleFormat(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: strPtr("乙")})
	if err != nil {
		t.Fatal(err)
	}

	n, err := st.MergeNotes(ctx, []string{a.ID, b.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`).MatchString(n.Title) {
		t.Fatalf("自动标题格式不符: %q", n.Title)
	}
	if n.CategoryID != nil {
		t.Fatalf("自动标题的合并笔记也应落未分类, category = %+v", n.CategoryID)
	}
}

func TestMergeNotesExplicitTitle(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: strPtr("乙")})
	if err != nil {
		t.Fatal(err)
	}

	n, err := st.MergeNotes(ctx, []string{a.ID, b.ID}, "  合并周报  ")
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "合并周报" {
		t.Fatalf("title = %q, want 合并周报", n.Title)
	}
	if n.CategoryID != nil {
		t.Fatalf("显式标题的合并笔记也应落未分类, category = %+v", n.CategoryID)
	}
	if n.Content == nil || *n.Content != "甲\n\n---\n\n乙" {
		t.Fatalf("合并正文 = %+v, want %q", n.Content, "甲\n\n---\n\n乙")
	}
}

func TestMergeNotesSeparatesEveryPair(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: strPtr("乙")})
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateNote(ctx, NoteInput{Title: "C", Content: strPtr("丙")})
	if err != nil {
		t.Fatal(err)
	}

	n, err := st.MergeNotes(ctx, []string{a.ID, b.ID, c.ID}, "")
	if err != nil {
		t.Fatal(err)
	}
	const wantThree = "甲\n\n---\n\n乙\n\n---\n\n丙"
	if n.Content == nil || *n.Content != wantThree {
		t.Fatalf("合并正文 = %+v, want %q", n.Content, wantThree)
	}
}

func TestMergeNotesAutoTitleRetriesSameSecond(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	fixed := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	// 占住第 0 秒（正常笔记）与第 1 秒（回收站里的笔记也要挡路）的 合并-<time> 标题，都在未分类。
	occupy, err := st.CreateNote(ctx, NoteInput{Title: "合并-" + fixed.Format(timeLayoutLocal), Content: strPtr("占用")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNote(ctx, NoteInput{Title: "合并-" + fixed.Add(time.Second).Format(timeLayoutLocal), Content: strPtr("占用")}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDeleteNote(ctx, occupy.ID); err != nil {
		t.Fatal(err)
	}

	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: strPtr("乙")})
	if err != nil {
		t.Fatal(err)
	}

	n, err := st.mergeNotesAt(ctx, []string{a.ID, b.ID}, "", fixed)
	if err != nil {
		t.Fatal(err)
	}
	want := "合并-" + fixed.Add(2*time.Second).Format(timeLayoutLocal)
	if n.Title != want {
		t.Fatalf("title = %q, want %q", n.Title, want)
	}
}

func TestMergeNotesValidation(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MergeNotes(ctx, []string{a.ID}, ""); !errors.Is(err, ErrInvalidNote) {
		t.Fatalf("少于两条: err = %v, want ErrInvalidNote", err)
	}
	if _, err := st.MergeNotes(ctx, []string{a.ID, a.ID}, ""); !errors.Is(err, ErrInvalidNote) {
		t.Fatalf("重复 id: err = %v, want ErrInvalidNote", err)
	}
	if _, err := st.MergeNotes(ctx, []string{}, ""); !errors.Is(err, ErrInvalidNote) {
		t.Fatalf("空 ids: err = %v, want ErrInvalidNote", err)
	}
}

func TestMergeNotesMissingNote(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MergeNotes(ctx, []string{a.ID, "missing-id"}, ""); !errors.Is(err, ErrNoteNotFound) {
		t.Fatalf("err = %v, want ErrNoteNotFound", err)
	}
}

func TestMergeNotesTrashedNote(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: strPtr("乙")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDeleteNote(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MergeNotes(ctx, []string{a.ID, b.ID}, ""); !errors.Is(err, ErrNoteInTrash) {
		t.Fatalf("err = %v, want ErrNoteInTrash", err)
	}
}

func TestMergeNotesEncryptedNote(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	configureMaster(t, st)

	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: strPtr("甲")})
	if err != nil {
		t.Fatal(err)
	}
	enc := createEncryptedNote(t, st, ctx, "密文", nil)

	if _, err := st.MergeNotes(ctx, []string{a.ID, enc.ID}, ""); !errors.Is(err, ErrNoteEncrypted) {
		t.Fatalf("err = %v, want ErrNoteEncrypted", err)
	}
}

func TestMergeNotesContentTooLong(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	long := strings.Repeat("字", 1100) // 两条各 1100 码点，拼接后 2200 > 2000
	a, err := st.CreateNote(ctx, NoteInput{Title: "A", Content: &long})
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.CreateNote(ctx, NoteInput{Title: "B", Content: &long})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MergeNotes(ctx, []string{a.ID, b.ID}, ""); !errors.Is(err, ErrInvalidNote) {
		t.Fatalf("err = %v, want ErrInvalidNote", err)
	}
}
