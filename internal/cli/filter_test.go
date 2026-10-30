package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// noteItem 造一条带全文正文的列表项。
func noteItem(id, title, content string) ListItem {
	return ListItem{ID: id, Title: title, Content: content, UpdatedAt: "2026-10-29T00:00:00Z"}
}

func TestNoteFilterMatchesTitleFuzzyAndContentSubstring(t *testing.T) {
	items := []ListItem{
		noteItem("a", "会议纪要", "预算与排期"),
		noteItem("b", "周报", "本周完成了三件小事"),
		noteItem("c", "随手记", "买牛奶"),
	}
	entries := entriesOf(items...)
	targets := make([]string, len(entries))
	for i, entry := range entries {
		targets[i] = entry.FilterValue()
	}
	filter := noteFilter(contentsOf(entries))

	// 正文命中：标题里没有「牛奶」
	ranks := filter("牛奶", targets)
	if len(ranks) != 1 || ranks[0].Index != 2 {
		t.Fatalf("正文子串应当命中第 3 条，得到 %+v", ranks)
	}
	if len(ranks[0].MatchedIndexes) != 0 {
		t.Fatalf("正文命中不该给标题下标（会拿去高亮错字），得到 %v", ranks[0].MatchedIndexes)
	}

	// 标题 fuzzy 命中仍然生效
	ranks = filter("会议", targets)
	if len(ranks) == 0 || ranks[0].Index != 0 {
		t.Fatalf("标题 fuzzy 应当命中第 1 条，得到 %+v", ranks)
	}

	// 标题与正文同时命中时只出现一次
	ranks = filter("预算", targets)
	if len(ranks) != 1 || ranks[0].Index != 0 {
		t.Fatalf("不应重复计数，得到 %+v", ranks)
	}

	// 空过滤词由选择器自己处理（bubbles 用全量项填 filteredItems），
	// 这里的过滤器不需要为此特殊照顾，只保证不 panic、不重复
	if ranks = filter("", targets); len(ranks) != 0 {
		t.Fatalf("空过滤词交给选择器处理，过滤器本身应当返回空，得到 %+v", ranks)
	}

	// 大小写不敏感（ASCII）
	ascii := []ListItem{noteItem("x", "TODO", "Fix the BUG")}
	asciiEntries := entriesOf(ascii...)
	if ranks = noteFilter(contentsOf(asciiEntries))("bug", []string{asciiEntries[0].FilterValue()}); len(ranks) != 1 {
		t.Fatalf("正文子串应当忽略大小写，得到 %+v", ranks)
	}
}

// 正文命中的项要在交互列表里被筛出来，且第二行显示命中处附近的一小段。
func TestListModelFilterFindsContentOnlyMatch(t *testing.T) {
	model := newTestListModel(
		noteItem("a", "会议纪要", "先排期再谈预算"),
		noteItem("b", "随手记", "买牛奶"),
	)
	// 只敲能被 fuzzy 目标命中的字符不算，这里模拟输入「牛奶」
	for _, r := range "牛奶" {
		model = feedList(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}

	visible := model.picker.list.VisibleItems()
	if len(visible) != 1 {
		t.Fatalf("应当只剩一条（正文命中），得到 %d 条", len(visible))
	}
	entry, ok := visible[0].(listEntry)
	if !ok || entry.item.ID != "b" {
		t.Fatalf("应当筛出「随手记」，得到 %+v", visible[0])
	}

	// 第二行显示命中处：视图里要能看到「买牛奶」，且带命中高亮（拿掉 ANSI 后仍然可读）
	view := ansi.Strip(model.picker.view())
	if !strings.Contains(view, "买牛奶") {
		t.Fatalf("正文命中时第二行应当显示命中片段：\n%s", view)
	}
}

func TestSnippetAroundWindow(t *testing.T) {
	long := strings.Repeat("前", 40) + "关键词" + strings.Repeat("后", 40)

	got := snippetAround(long, "关键词", 60)
	if !strings.Contains(got, "关键词") {
		t.Fatalf("片段里应当含命中词：%q", got)
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("前面被裁掉时应当补省略号：%q", got)
	}
	if width := ansi.StringWidth(got); width > 61 {
		t.Fatalf("片段不该超过宽度预算（60 + 前置省略号）：%q（%d 列）", got, width)
	}

	if got := snippetAround("短正文", "不存在", 30); got != "" {
		t.Fatalf("正文里没有命中词时应当返回空，得到 %q", got)
	}
	if got := snippetAround("", "词", 30); got != "" {
		t.Fatalf("空正文应当返回空，得到 %q", got)
	}
	// 换行被折叠：列表行必须是单行，否则每项两行高的约定就破了
	if got := snippetAround("第一行\n关键词\n第三行", "关键词", 30); strings.Contains(got, "\n") {
		t.Fatalf("片段应当折叠换行，得到 %q", got)
	}
}

func TestHighlightTermKeepsTextAndMarksHits(t *testing.T) {
	// 颜色取决于终端色彩配置，单测只固定「文字不变」这条不变量；
	// 染色效果在真终端里验证（与 styleMatch 的其余用法一致）。
	got := highlightTerm("先谈预算再排期", "预算")
	if plain := ansi.Strip(got); plain != "先谈预算再排期" {
		t.Fatalf("高亮不能改动文字，得到 %q", plain)
	}

	// 大小写不敏感，且重复出现都要命中
	if plain := ansi.Strip(highlightTerm("BUG 与 bug", "bug")); plain != "BUG 与 bug" {
		t.Fatalf("大小写不敏感且不改写文本，得到 %q", plain)
	}

	if got := highlightTerm("没有命中", ""); got != "没有命中" {
		t.Fatalf("空词应当原样返回，得到 %q", got)
	}
}
