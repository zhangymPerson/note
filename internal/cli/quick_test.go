package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeQuickDeps 记录调用并返回预置结果，让 Run* 的纯逻辑可以脱离数据库测。
type fakeQuickDeps struct {
	categories []Category
	ensured    []string
	lookedUp   []string

	setCalls   int
	setTitle   string
	setContent string
	created    bool
	setErr     error

	note    QuickNote
	getErr  error
	notes   []QuickNote
	deleted QuickNote
	delErr  error

	deleteNotes   int
	deleteNotesID bool
	delNotesErr   error
}

func (f *fakeQuickDeps) deps() QuickDeps {
	return QuickDeps{
		Categories: func(context.Context) ([]Category, error) { return f.categories, nil },
		Category: func(_ context.Context, name string) (Category, error) {
			f.lookedUp = append(f.lookedUp, name)
			for _, cat := range f.categories {
				if cat.Name == name {
					return cat, nil
				}
			}
			return Category{}, ErrQuickCategoryNotFound
		},
		EnsureCategory: func(_ context.Context, name string) (Category, error) {
			f.ensured = append(f.ensured, name)
			for _, cat := range f.categories {
				if cat.Name == name {
					return cat, nil
				}
			}
			return Category{ID: 9, Name: name}, nil
		},
		Set: func(_ context.Context, category Category, title, content string) (QuickNote, bool, error) {
			f.setCalls++
			f.setTitle = title
			f.setContent = content
			return QuickNote{Title: title, Content: content}, f.created, f.setErr
		},
		Get: func(context.Context, Category, string) (QuickNote, error) { return f.note, f.getErr },
		Notes: func(context.Context, Category) ([]QuickNote, error) {
			return f.notes, f.getErr
		},
		Delete: func(context.Context, Category, string) (QuickNote, error) {
			return f.deleted, f.delErr
		},
		DeleteNotes: func(context.Context, Category) (int, error) {
			f.deleteNotesID = true
			return f.deleteNotes, f.delNotesErr
		},
	}
}

func TestRunSetReportsCreatedAndUpdated(t *testing.T) {
	ctx := context.Background()
	deps := &fakeQuickDeps{categories: []Category{{ID: 2, Name: "速记", Reserved: true}}}

	deps.created = true
	text, err := RunSet(ctx, deps.deps(), Ref{Title: "周报", Category: "速记"}, "第一版")
	if err != nil {
		t.Fatalf("RunSet: %v", err)
	}
	if text != "已保存「周报」@速记\n" {
		t.Fatalf("unexpected message: %q", text)
	}
	if deps.setTitle != "周报" || deps.setContent != "第一版" {
		t.Fatalf("expected title/content to reach deps.Set, got %q / %q", deps.setTitle, deps.setContent)
	}

	deps.created = false
	text, err = RunSet(ctx, deps.deps(), Ref{Title: "周报", Category: "速记"}, "第二版")
	if err != nil {
		t.Fatalf("RunSet overwrite: %v", err)
	}
	if text != "已更新「周报」@速记\n" {
		t.Fatalf("unexpected message: %q", text)
	}
}

func TestRunSetRejectsOversizedContent(t *testing.T) {
	deps := &fakeQuickDeps{}

	if _, err := RunSet(context.Background(), deps.deps(), Ref{Title: "周报", Category: "速记"}, strings.Repeat("字", maxQuickContentRunes+1)); err == nil {
		t.Fatal("expected an error for content over the limit")
	}
	if deps.setCalls != 0 {
		t.Fatalf("expected no write for invalid content, got %d calls", deps.setCalls)
	}
}

func TestRunSetUsesEnsuredCategory(t *testing.T) {
	deps := &fakeQuickDeps{}

	if _, err := RunSet(context.Background(), deps.deps(), Ref{Title: "周报", Category: "新分类"}, "正文"); err != nil {
		t.Fatalf("RunSet: %v", err)
	}
	if len(deps.ensured) != 1 || deps.ensured[0] != "新分类" {
		t.Fatalf("expected EnsureCategory to be called for the target category, got %v", deps.ensured)
	}
}

func TestRunGetReturnsContentWithTrailingNewline(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct{ in, want string }{
		{in: "abc", want: "abc\n"},
		{in: "abc\n", want: "abc\n"},
		{in: "", want: "\n"},
	} {
		deps := &fakeQuickDeps{
			categories: []Category{{ID: 2, Name: "速记", Reserved: true}},
			note:       QuickNote{Title: "周报", Content: tc.in},
		}
		got, err := RunGet(ctx, deps.deps(), Ref{Title: "周报", Category: "速记"})
		if err != nil {
			t.Fatalf("RunGet(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("RunGet(%q) = %q, want %q", tc.in, got, tc.want)
		}
		if deps.lookedUp[0] != "速记" {
			t.Fatalf("expected the category to be resolved first, got %v", deps.lookedUp)
		}
	}
}

func TestRunGetMapsTrashedAndEncrypted(t *testing.T) {
	ctx := context.Background()

	trashed := &fakeQuickDeps{
		categories: []Category{{ID: 2, Name: "速记", Reserved: true}},
		note:       QuickNote{Title: "周报", Trashed: true},
	}
	if _, err := RunGet(ctx, trashed.deps(), Ref{Title: "周报", Category: "速记"}); !errors.Is(err, ErrQuickInTrash) {
		t.Fatalf("expected ErrQuickInTrash, got %v", err)
	}

	encrypted := &fakeQuickDeps{
		categories: []Category{{ID: 2, Name: "速记", Reserved: true}},
		note:       QuickNote{Title: "周报", Encrypted: true},
	}
	if _, err := RunGet(ctx, encrypted.deps(), Ref{Title: "周报", Category: "速记"}); !errors.Is(err, ErrNoteEncrypted) {
		t.Fatalf("expected ErrNoteEncrypted, got %v", err)
	}
}

func TestRunGetRejectsCategoryOnly(t *testing.T) {
	deps := &fakeQuickDeps{}
	if _, err := RunGet(context.Background(), deps.deps(), Ref{Category: "工作", CategoryOnly: true}); err == nil {
		t.Fatal("expected a usage error when only a category is given")
	}
}

func TestRunGetCategoryListsTitlesOnlyByDefault(t *testing.T) {
	deps := &fakeQuickDeps{
		categories: []Category{{ID: 2, Name: "速记"}},
		notes: []QuickNote{
			{Title: "周报", UpdatedAt: "2026-11-02T08:00:00Z", Content: "本周的结论"},
			{Title: "月报", Encrypted: true, UpdatedAt: "2026-11-01T08:00:00Z"},
		},
	}

	text, err := RunGetCategory(context.Background(), deps.deps(), Ref{Category: "速记", CategoryOnly: true}, false)
	if err != nil {
		t.Fatalf("RunGetCategory: %v", err)
	}
	// 默认只给标题：时间与正文要 --detail 才出现，管道里不该混进多余列。
	if text != "周报\n月报\n" {
		t.Fatalf("got %q", text)
	}
}

func TestRunGetCategoryDetailPrintsTable(t *testing.T) {
	deps := &fakeQuickDeps{
		categories: []Category{{ID: 2, Name: "速记"}},
		notes: []QuickNote{
			{Title: "周报", UpdatedAt: "2026-11-02T08:00:00Z", Content: "本周的结论\n\n下一周的计划"},
			{Title: "月报", Encrypted: true, UpdatedAt: "2026-11-01T08:00:00Z"},
			{Title: "空笔记", UpdatedAt: "2026-10-31T08:00:00Z"},
		},
	}

	text, err := RunGetCategory(context.Background(), deps.deps(), Ref{Category: "速记", CategoryOnly: true}, true)
	if err != nil {
		t.Fatalf("RunGetCategory --detail: %v", err)
	}

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 rows, got %q", text)
	}
	header := lines[0]
	for _, want := range []string{"标题", "更新时间", "正文"} {
		if !strings.Contains(header, want) {
			t.Fatalf("expected header %q in %q", want, header)
		}
	}
	if !strings.Contains(lines[1], "周报") || !strings.Contains(lines[1], "2026-11-02T08:00:00Z") {
		t.Fatalf("expected title and time in %q", lines[1])
	}
	// 正文折成一行：表格里不该出现换行。
	if !strings.Contains(lines[1], "本周的结论 下一周的计划") {
		t.Fatalf("expected folded content in %q", lines[1])
	}
	if !strings.Contains(lines[2], "（加密）") {
		t.Fatalf("expected an encrypted marker in %q", lines[2])
	}
	if !strings.Contains(lines[3], "（空）") {
		t.Fatalf("expected an empty-content marker in %q", lines[3])
	}
}

func TestRunGetCategoryEmptyIsBlankNotError(t *testing.T) {
	deps := &fakeQuickDeps{categories: []Category{{ID: 2, Name: "速记"}}}

	text, err := RunGetCategory(context.Background(), deps.deps(), Ref{Category: "速记", CategoryOnly: true}, true)
	if err != nil {
		t.Fatalf("RunGetCategory: %v", err)
	}
	if text != "" {
		t.Fatalf("expected no output for an empty category, got %q", text)
	}
}

func TestRunGetCategoriesPrintsHeaderAndReserved(t *testing.T) {
	deps := &fakeQuickDeps{categories: []Category{
		{ID: 1, Name: "未分类", Count: 3, Reserved: true},
		{ID: 2, Name: "速记", Count: 1, Reserved: true},
		{ID: 3, Name: "工作", Count: 2},
	}}

	text, err := RunGetCategories(context.Background(), deps.deps())
	if err != nil {
		t.Fatalf("RunGetCategories: %v", err)
	}

	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header + 3 rows, got %q", text)
	}
	for _, header := range []string{"ID", "分类", "笔记数", "备注"} {
		if !strings.Contains(lines[0], header) {
			t.Fatalf("expected header %q in %q", header, lines[0])
		}
	}
	for _, line := range lines[1:3] {
		if !strings.Contains(line, "固定") {
			t.Fatalf("expected built-in categories to be marked 固定, got %q", line)
		}
	}
	if strings.Contains(lines[3], "固定") {
		t.Fatalf("expected user categories to have an empty remark, got %q", lines[3])
	}
	if strings.TrimRight(lines[3], " 	") != lines[3] {
		t.Fatalf("expected no trailing padding on remark-less rows, got %q", lines[3])
	}
	for _, want := range []string{"未分类", "速记", "工作", "3", "1", "2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in output %q", want, text)
		}
	}
}

func TestRunDelReportsTrashAndMapsMissing(t *testing.T) {
	ctx := context.Background()
	deps := &fakeQuickDeps{categories: []Category{{ID: 2, Name: "速记"}}, deleted: QuickNote{Title: "周报"}}

	text, err := RunDel(ctx, deps.deps(), Ref{Title: "周报", Category: "速记"})
	if err != nil {
		t.Fatalf("RunDel: %v", err)
	}
	if text != "已移入回收站「周报」@速记\n" {
		t.Fatalf("unexpected message: %q", text)
	}

	missing := &fakeQuickDeps{categories: []Category{{ID: 2, Name: "速记"}}, delErr: ErrQuickNotFound}
	if _, err := RunDel(ctx, missing.deps(), Ref{Title: "没有", Category: "速记"}); !errors.Is(err, ErrQuickNotFound) {
		t.Fatalf("expected ErrQuickNotFound, got %v", err)
	}
}

func TestRunDelCategoryReportsCount(t *testing.T) {
	ctx := context.Background()
	deps := &fakeQuickDeps{categories: []Category{{ID: 3, Name: "工作"}}, deleteNotes: 3}

	text, err := RunDelCategory(ctx, deps.deps(), Ref{Category: "工作", CategoryOnly: true})
	if err != nil {
		t.Fatalf("RunDelCategory: %v", err)
	}
	if text != "已移入回收站 3 条（分类「工作」）\n" {
		t.Fatalf("unexpected message: %q", text)
	}

	empty := &fakeQuickDeps{categories: []Category{{ID: 3, Name: "工作"}}, deleteNotes: 0}
	if _, err := RunDelCategory(ctx, empty.deps(), Ref{Category: "工作", CategoryOnly: true}); !errors.Is(err, ErrQuickNothingDeleted) {
		t.Fatalf("expected ErrQuickNothingDeleted, got %v", err)
	}
}

func TestRunDelRejectsCategoryOnly(t *testing.T) {
	deps := &fakeQuickDeps{}
	if _, err := RunDel(context.Background(), deps.deps(), Ref{Category: "工作", CategoryOnly: true}); err == nil {
		t.Fatal("expected a usage error when only a category is given")
	}
}
