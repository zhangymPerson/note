package store

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// strPtr / int64Ptr 是 quick_test 的本地小工具（note_test 直接用 &body，这里避免重复声明变量）。
func strPtr(s string) *string { return &s }

func int64Ptr(i int64) *int64 { return &i }

func TestCreateQuickNoteAutoTitle(t *testing.T) {
	st := noteTestStore(t)
	n, err := st.CreateQuickNote(context.Background(), "买牛奶和鸡蛋", "")
	if err != nil {
		t.Fatal(err)
	}
	if n.CategoryID == nil || *n.CategoryID != int64(QuickCategoryID) {
		t.Fatalf("category = %+v", n.CategoryID)
	}
	if !regexp.MustCompile(`^速记-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`).MatchString(n.Title) {
		t.Fatalf("标题格式不符: %q", n.Title)
	}
	if n.Content == nil || *n.Content != "买牛奶和鸡蛋" {
		t.Fatalf("正文不符: %+v", n.Content)
	}
}

func TestCreateQuickNoteRetriesSameSecond(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	fixed := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	// 占住第 0 秒的标题（正常笔记）。
	occupy, err := st.CreateNote(ctx, NoteInput{
		Title: "速记-" + fixed.Format(timeLayoutLocal), Content: strPtr("占用"), CategoryID: int64Ptr(QuickCategoryID),
	})
	if err != nil {
		t.Fatal(err)
	}
	// 占住第 1 秒的标题（回收站里的笔记也要挡路）。
	if _, err := st.CreateNote(ctx, NoteInput{
		Title: "速记-" + fixed.Add(time.Second).Format(timeLayoutLocal), Content: strPtr("占用"), CategoryID: int64Ptr(QuickCategoryID),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SoftDeleteNote(ctx, occupy.ID); err != nil {
		t.Fatal(err)
	}

	n, err := st.createQuickNoteAt(ctx, "新速记", "", fixed)
	if err != nil {
		t.Fatal(err)
	}
	want := "速记-" + fixed.Add(2*time.Second).Format(timeLayoutLocal)
	if n.Title != want {
		t.Fatalf("title = %q, want %q", n.Title, want)
	}
	if n.Content == nil || *n.Content != "新速记" {
		t.Fatalf("正文不符: %+v", n.Content)
	}
}

func TestCreateQuickNoteExplicitTitleConflict(t *testing.T) {
	st := noteTestStore(t)
	ctx := context.Background()
	if _, err := st.CreateNote(ctx, NoteInput{
		Title: "我写过的", Content: strPtr("旧"), CategoryID: int64Ptr(QuickCategoryID),
	}); err != nil {
		t.Fatal(err)
	}
	_, err := st.CreateQuickNote(ctx, "新内容", "我写过的")
	if !errors.Is(err, ErrNoteTitleExists) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateQuickNoteValidation(t *testing.T) {
	st := noteTestStore(t)
	for _, tc := range []struct{ name, content, title string }{
		{"空正文", "  ", ""},
		{"正文超长", strings.Repeat("😀", maxContentRunes+1), ""},
		{"标题超长", "内容", strings.Repeat("字", maxTitleRunes+1)},
	} {
		if _, err := st.CreateQuickNote(context.Background(), tc.content, tc.title); !errors.Is(err, ErrInvalidNote) {
			t.Fatalf("%s: err = %v", tc.name, err)
		}
	}
}

func TestCreateQuickNoteRecordsActivity(t *testing.T) {
	st := noteTestStore(t)
	if _, err := st.CreateQuickNote(context.Background(), "活动", ""); err != nil {
		t.Fatal(err)
	}
	if got := kindCount(t, st, ActivityNoteCreate); got != 1 {
		t.Fatalf("expected 1 note_create activity, got %d", got)
	}
}
