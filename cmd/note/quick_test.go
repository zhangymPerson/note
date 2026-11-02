package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"note/internal/cli"
	"note/internal/store"
)

// 两处常量各存一份（store 是库里的事实，cli 是 UI 文案），必须一致：
// 不一致时 note set 会把笔记写进「速记」，而 note get 去找另一个名字。
func TestQuickCategoryNamesMatch(t *testing.T) {
	if cli.QuickCategoryName != store.QuickCategoryName {
		t.Fatalf("cli.QuickCategoryName = %q, store.QuickCategoryName = %q",
			cli.QuickCategoryName, store.QuickCategoryName)
	}
}

func TestQuickCategoryIDMapsUncategorizedToNull(t *testing.T) {
	if got := quickCategoryID(cli.Category{ID: store.DefaultCategoryID, IsDefault: true}); got != nil {
		t.Fatalf("内置「未分类」应当映射成 category_id IS NULL，得到 %v", *got)
	}
	got := quickCategoryID(cli.Category{ID: store.QuickCategoryID, Reserved: true})
	if got == nil || *got != store.QuickCategoryID {
		t.Fatalf("「速记」应当映射成 id %d，得到 %v", store.QuickCategoryID, got)
	}
}

// 走一遍真实数据库：写入 → 读回 → 覆盖 → 列分类 → 软删 → 再读。
func TestQuickDepsRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	deps := quickDeps(st)

	ref := cli.Ref{Title: "部署命令", Category: store.QuickCategoryName}
	message, err := cli.RunSet(ctx, deps, ref, "make deploy")
	if err != nil {
		t.Fatalf("RunSet: %v", err)
	}
	if message != "已保存「部署命令」@速记\n" {
		t.Fatalf("unexpected message: %q", message)
	}

	text, err := cli.RunGet(ctx, deps, ref)
	if err != nil {
		t.Fatalf("RunGet: %v", err)
	}
	if text != "make deploy\n" {
		t.Fatalf("unexpected content: %q", text)
	}

	// 同名再写一次是覆盖：同一条笔记、正文换掉。
	if _, err := cli.RunSet(ctx, deps, ref, "make deploy ENV=prod"); err != nil {
		t.Fatalf("RunSet overwrite: %v", err)
	}
	page, err := st.ListNotes(ctx, store.NoteFilter{Page: 1})
	if err != nil {
		t.Fatalf("list notes: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("同名覆盖不该新建第二条，实际 %d 条", page.Total)
	}

	// set 顺带建分类：新分类不在初始的 categories 里。
	work := cli.Ref{Title: "周报", Category: "工作"}
	if _, err := cli.RunSet(ctx, deps, work, "本周结论"); err != nil {
		t.Fatalf("RunSet 新分类: %v", err)
	}
	if _, err := st.CategoryByName(ctx, "工作"); err != nil {
		t.Fatalf("expected the category to be created: %v", err)
	}

	if _, err := cli.RunSet(ctx, deps, work, "本周结论 v2"); err != nil {
		t.Fatalf("RunSet 同分类同名: %v", err)
	}

	list, err := cli.RunGetCategories(ctx, deps)
	if err != nil {
		t.Fatalf("RunGetCategories: %v", err)
	}
	for _, want := range []string{"未分类", "速记", "工作", "固定"} {
		if !strings.Contains(list, want) {
			t.Fatalf("分类表应当包含 %q：%q", want, list)
		}
	}

	rows, err := cli.RunGetCategory(ctx, deps, cli.Ref{Category: "工作", CategoryOnly: true})
	if err != nil {
		t.Fatalf("RunGetCategory: %v", err)
	}
	if !strings.Contains(rows, "周报\t") {
		t.Fatalf("分类列表应当含「周报」：%q", rows)
	}

	if _, err := cli.RunDel(ctx, deps, ref); err != nil {
		t.Fatalf("RunDel: %v", err)
	}
	if _, err := cli.RunGet(ctx, deps, ref); !errors.Is(err, cli.ErrQuickInTrash) {
		t.Fatalf("删除后应当报「在回收站里」，得到 %v", err)
	}
	if _, err := cli.RunSet(ctx, deps, ref, "又一条"); !errors.Is(err, cli.ErrQuickInTrash) {
		t.Fatalf("回收站同名应当拒绝覆盖，得到 %v", err)
	}

	// 整分类删除：只删未删除的，返回条数。
	deleted, err := cli.RunDelCategory(ctx, deps, cli.Ref{Category: "工作", CategoryOnly: true})
	if err != nil {
		t.Fatalf("RunDelCategory: %v", err)
	}
	if deleted != "已移入回收站 1 条（分类「工作」）\n" {
		t.Fatalf("unexpected message: %q", deleted)
	}
	if _, err := cli.RunDelCategory(ctx, deps, cli.Ref{Category: "工作", CategoryOnly: true}); !errors.Is(err, cli.ErrQuickNothingDeleted) {
		t.Fatalf("再次整分类删除应当报没删到，得到 %v", err)
	}
}

func TestQuickDepsMapsMissingNotesAndCategories(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	deps := quickDeps(st)

	if _, err := cli.RunGet(ctx, deps, cli.Ref{Title: "没有这条", Category: store.QuickCategoryName}); !errors.Is(err, cli.ErrQuickNotFound) {
		t.Fatalf("expected ErrQuickNotFound, got %v", err)
	}
	if _, err := cli.RunGet(ctx, deps, cli.Ref{Title: "没有这条", Category: "没有这个分类"}); !errors.Is(err, cli.ErrQuickCategoryNotFound) {
		t.Fatalf("expected ErrQuickCategoryNotFound, got %v", err)
	}
}

// 未分类在库里是 category_id IS NULL，走「未分类」这个名字也必须读写到同一批笔记。
func TestQuickDepsUncategorizedRoundTrip(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	deps := quickDeps(st)

	ref := cli.Ref{Title: "随手记", Category: "未分类"}
	if _, err := cli.RunSet(ctx, deps, ref, "正文"); err != nil {
		t.Fatalf("RunSet 未分类: %v", err)
	}

	note, err := st.FindNoteByTitle(ctx, nil, "随手记")
	if err != nil {
		t.Fatalf("未分类笔记应当落成 category_id IS NULL: %v", err)
	}
	if note.CategoryID != nil {
		t.Fatalf("expected nil category_id, got %v", *note.CategoryID)
	}

	rows, err := cli.RunGetCategory(ctx, deps, cli.Ref{Category: "未分类", CategoryOnly: true})
	if err != nil {
		t.Fatalf("RunGetCategory 未分类: %v", err)
	}
	if !strings.Contains(rows, "随手记\t") {
		t.Fatalf("未分类列表应当含「随手记」：%q", rows)
	}
}
