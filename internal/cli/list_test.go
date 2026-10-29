package cli

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

func plainItem(id, title, category, stamp string) ListItem {
	return ListItem{ID: id, Title: title, CategoryName: category, UpdatedAt: stamp}
}

func TestFormatListPlainAlignsColumns(t *testing.T) {
	items := []ListItem{
		plainItem("9f2c1a3b-4d5e-4f60-8a71-2b3c4d5e6f70", "会议纪要", "工作", "2026-10-29T14:03:05Z"),
		{ID: "b1c2d3e4-5f60-4a71-8b92-3c4d5e6f7081", Title: "加密的那条", CategoryName: "私密", Encrypted: true, UpdatedAt: "2026-10-26T22:40:11Z"},
	}
	out := FormatListPlain(items, 0)

	if strings.Contains(out, "\x1b[") {
		t.Fatalf("纯文本输出不该带 ANSI：%q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("应当两行，得到 %d 行：%q", len(lines), out)
	}
	for _, want := range []string{
		"9f2c1a3b-4d5e-4f60-8a71-2b3c4d5e6f70", "会议纪要", "工作", "2026-10-29T14:03:05Z",
		"b1c2d3e4-5f60-4a71-8b92-3c4d5e6f7081", "加密的那条 [加密]", "私密", "2026-10-26T22:40:11Z",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("输出缺少 %q：\n%s", want, out)
		}
	}
	// 分类列必须对齐：两行的分类起始显示列相同（标题宽度不同，靠 tabwriter 补齐）
	if start0, start1 := columnOf(lines[0], "工作"), columnOf(lines[1], "私密"); start0 != start1 {
		t.Fatalf("分类列没有对齐：%d vs %d\n%s", start0, start1, out)
	}
}

func TestFormatListPlainLimit(t *testing.T) {
	items := []ListItem{
		plainItem("a", "一", "工作", "2026-10-29T00:00:00Z"),
		plainItem("b", "二", "工作", "2026-10-28T00:00:00Z"),
		plainItem("c", "三", "工作", "2026-10-27T00:00:00Z"),
	}
	if got := strings.Count(FormatListPlain(items, 2), "\n"); got != 2 {
		t.Fatalf("limit 2 应当输出两行，得到 %d 行", got)
	}
	if got := strings.Count(FormatListPlain(items, 0), "\n"); got != 3 {
		t.Fatalf("limit 0 应当输出全部三行，得到 %d 行", got)
	}
}

func TestFormatListPlainEmptyIsEmptyString(t *testing.T) {
	if got := FormatListPlain(nil, 0); got != "" {
		t.Fatalf("空结果必须一个字节都不输出，得到 %q", got)
	}
	if got := FormatListPlain([]ListItem{}, 10); got != "" {
		t.Fatalf("空结果必须一个字节都不输出，得到 %q", got)
	}
}

func TestListRowSummaryCollapsesWhitespace(t *testing.T) {
	if got := listRowSummary("# 标题\n\n第一行\n第二行", 80); got != "# 标题 第一行 第二行" {
		t.Fatalf("摘要应当折叠成单行，得到 %q", got)
	}
	if got := listRowSummary("工作", 80); got != "工作" {
		t.Fatalf("短摘要应当原样返回，得到 %q", got)
	}

	// 长 CJK 串按显示宽度截断（CJK 占两列），不是按码点
	got := listRowSummary(strings.Repeat("中", 30), 10)
	if width := uniseg.StringWidth(got); width > 10 {
		t.Fatalf("截断后显示宽度 %d 超过上限 10：%q", width, got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("被截断时应当补省略号：%q", got)
	}
}

func TestListItemFilterTargetUsesTitleAndCategory(t *testing.T) {
	item := plainItem("a", "会议纪要", "工作", "2026-10-29T00:00:00Z")
	if got := item.FilterTarget(); got != "会议纪要 工作" {
		t.Fatalf("FilterTarget = %q", got)
	}
	item.CategoryName = ""
	if got := item.FilterTarget(); got != "会议纪要 未分类" {
		t.Fatalf("未分类时 FilterTarget = %q", got)
	}
}

func TestRunListPlainPathReturnsText(t *testing.T) {
	var gotDeleted bool
	deps := ListDeps{Notes: func(ctx context.Context, deleted bool) ([]ListItem, error) {
		gotDeleted = deleted
		return []ListItem{plainItem("a", "甲", "工作", "2026-10-29T00:00:00Z")}, nil
	}}

	res, err := runList(context.Background(), deps, ListOptions{Trash: true}, false)
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if res.Selected != nil {
		t.Fatalf("纯文本路径不该返回选中项：%+v", res.Selected)
	}
	if !strings.Contains(res.Text, "甲") {
		t.Fatalf("纯文本输出应当含标题，得到 %q", res.Text)
	}
	if !gotDeleted {
		t.Fatal("--trash 应当透传给 deps")
	}
}

func TestRunListEmptyLibraryInteractiveHint(t *testing.T) {
	empty := ListDeps{Notes: func(ctx context.Context, deleted bool) ([]ListItem, error) {
		return nil, nil
	}}

	res, err := runList(context.Background(), empty, ListOptions{}, true)
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(res.Text, "还没有笔记") {
		t.Fatalf("空库的交互路径应当给提示，得到 %q", res.Text)
	}

	res, err = runList(context.Background(), empty, ListOptions{Trash: true}, true)
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(res.Text, "回收站是空的") {
		t.Fatalf("空回收站应当给对应提示，得到 %q", res.Text)
	}

	res, err = runList(context.Background(), empty, ListOptions{Plain: true}, false)
	if err != nil {
		t.Fatalf("runList: %v", err)
	}
	if res.Text != "" {
		t.Fatalf("非交互路径空结果必须一个字节都不输出，得到 %q", res.Text)
	}
}

func TestListModelEnterSelectsHighlightedItem(t *testing.T) {
	model := newTestListModel(
		plainItem("a", "会议纪要", "工作", "2026-10-29T00:00:00Z"),
		plainItem("b", "工作笔记", "工作", "2026-10-28T00:00:00Z"),
		plainItem("c", "读书笔记", "阅读", "2026-10-27T00:00:00Z"),
	)

	model = feedList(t, model, keyMsg("工"))
	model = feedList(t, model, keyMsg("作笔"))
	if model.selected != nil {
		t.Fatalf("还没按 Enter 就选中了 %+v", model.selected)
	}

	model = feedList(t, model, keyMsg("enter"))
	if model.selected == nil {
		t.Fatal("Enter 应当选中当前高亮项")
	}
	if model.selected.item.Title != "工作笔记" {
		t.Fatalf("选中的应当是「工作笔记」，得到 %q", model.selected.item.Title)
	}
}

func TestListModelEscWithoutSelectionQuits(t *testing.T) {
	model := newTestListModel(plainItem("a", "会议纪要", "工作", "2026-10-29T00:00:00Z"))

	updated, cmd := model.Update(keyMsg("esc"))
	final, ok := updated.(listModel)
	if !ok {
		t.Fatalf("模型类型变了：%T", updated)
	}
	if final.selected != nil {
		t.Fatalf("Esc 退出不该有选中项：%+v", final.selected)
	}
	if cmd == nil {
		t.Fatal("Esc 应当让模型发出退出命令")
	}
}

func TestListModelViewShowsCountAndHelp(t *testing.T) {
	model := newTestListModel(
		plainItem("a", "会议纪要", "工作", "2026-10-29T00:00:00Z"),
		plainItem("b", "工作笔记", "工作", "2026-10-28T00:00:00Z"),
	)
	view := model.View()
	for _, want := range []string{"note list", "2 篇", "Enter", "Esc"} {
		if !strings.Contains(view, want) {
			t.Fatalf("列表视图缺少 %q：\n%s", want, view)
		}
	}
}

// columnOf 返回 sub 在 line 里的显示列（按 rune 计），用来断言列对齐。
func columnOf(line, sub string) int {
	return utf8.RuneCountInString(line[:strings.Index(line, sub)])
}

func entriesOf(items ...ListItem) []list.Item {
	entries := make([]list.Item, 0, len(items))
	for _, item := range items {
		entries = append(entries, listEntry{item: item})
	}
	return entries
}

// newTestListModel 造一个交互列表模型，并关掉过滤框的光标闪烁：
// 闪烁在真实运行时是后台定时器（530ms 一跳），测试里每个按键都要白等一个 tick。
func newTestListModel(items ...ListItem) listModel {
	model := newListModel(entriesOf(items...))
	model.picker.list.FilterInput.Cursor.SetMode(cursor.CursorStatic)
	return model
}

// feedList 喂一条消息并把它返回的命令跑到底：bubbles 的过滤结果要等
// FilterMatchesMsg 回来才落地，可见项与选中项都受它影响。
func feedList(t *testing.T, m listModel, msg tea.Msg) listModel {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(listModel)
	if !ok {
		t.Fatalf("模型类型变了：%T", updated)
	}
	return settleList(t, next, cmd)
}

func settleList(t *testing.T, m listModel, cmd tea.Cmd) listModel {
	t.Helper()
	if cmd == nil {
		return m
	}
	switch value := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, sub := range value {
			m = settleList(t, m, sub)
		}
		return m
	case list.FilterMatchesMsg:
		updated, next := m.Update(value)
		model, ok := updated.(listModel)
		if !ok {
			t.Fatalf("模型类型变了：%T", updated)
		}
		return settleList(t, model, next)
	default:
		// 其余命令（光标闪烁的初始消息）与过滤无关；把它们喂回模型会拉起
		// 500ms 一跳的定时器，在测试里就是死循环。
		return m
	}
}
