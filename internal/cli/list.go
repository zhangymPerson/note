package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

// ListItem 是 note list 的一行：只带展示需要的字段，不含完整正文。
type ListItem struct {
	ID           string
	Title        string
	CategoryName string
	// Summary 是明文笔记正文的前 140 码点；加密笔记为空。
	Summary string
	// Encrypted 为真表示这条笔记在库里只有密文，CLI 解不开。
	Encrypted bool
	// Trashed 为真表示这条笔记在回收站里。
	Trashed bool
	// UpdatedAt 是 UTC 秒级文本，原样来自 store。
	UpdatedAt string
}

// FilterTarget 是 fuzzy 匹配的目标：标题 + 分类。
// 摘要不进目标：库里存的是原始正文，随便一个词都可能命中摘要而把列表搅乱，
// 而 CLI 找笔记的主线是「按标题找」。
func (i ListItem) FilterTarget() string {
	category := i.CategoryName
	if category == "" {
		category = "未分类"
	}
	return i.Title + " " + category
}

// ListDeps 让 cli 不必依赖持久层。
type ListDeps struct {
	// Notes 返回全部（deleted 为真时只返回回收站）笔记。
	Notes func(ctx context.Context, deleted bool) ([]ListItem, error)
}

// ListResult 是 note list 的结果：Text 非空表示非交互路径已经算好了要打印的文本，
// Selected 非空表示用户在 TUI 里选中了一条；两者互斥，都为空表示空库（非交互路径）或用户直接退出。
// cli 不写 stdout，打印交给 cmd/note。
type ListResult struct {
	Text     string
	Selected *ListItem
}

// RunList 跑 note list：stdout 与 stdin 都是终端且未给 --plain 时进 TUI，否则输出纯文本。
func RunList(ctx context.Context, deps ListDeps, opts ListOptions) (ListResult, error) {
	interactive := !opts.Plain && IsTerminal(os.Stdout) && IsTerminal(os.Stdin)
	return runList(ctx, deps, opts, interactive)
}

// runList 是 RunList 的可测内核：interactive 由调用方给出，测试可以强制两条路径。
func runList(ctx context.Context, deps ListDeps, opts ListOptions, interactive bool) (ListResult, error) {
	items, err := deps.Notes(ctx, opts.Trash)
	if err != nil {
		return ListResult{}, err
	}

	if !interactive {
		// 空结果一个字节都不输出，否则 `note list --plain | wc -l` 会被提示文案污染
		return ListResult{Text: FormatListPlain(items, opts.Limit)}, nil
	}
	if len(items) == 0 {
		if opts.Trash {
			return ListResult{Text: "回收站是空的\n"}, nil
		}
		return ListResult{Text: "还没有笔记，用 note add 新建一条\n"}, nil
	}

	selected, ok, err := runListTUI(items)
	if err != nil {
		return ListResult{}, err
	}
	if !ok {
		return ListResult{}, nil
	}
	return ListResult{Selected: &selected}, nil
}

// FormatListPlain 渲染非交互输出（tabwriter 对齐的四列，一行一条）；
// limit 大于 0 时只取前 limit 条，空输入返回空串。
func FormatListPlain(items []ListItem, limit int) string {
	if len(items) == 0 {
		return ""
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	for _, item := range items {
		title := item.Title
		if item.Encrypted {
			title += " [加密]"
		}
		// 时间原样输出 UTC 秒级文本：可排序、可被脚本消费，不做时区转换
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.ID, title, item.CategoryName, item.UpdatedAt)
	}
	_ = w.Flush()
	return b.String()
}

// runListTUI 跑交互列表；ok=false 表示用户没选就退出。
func runListTUI(items []ListItem) (ListItem, bool, error) {
	final, err := tea.NewProgram(newListModel(noteEntries(items)), tea.WithAltScreen()).Run()
	if err != nil {
		return ListItem{}, false, err
	}
	model, ok := final.(listModel)
	if !ok || model.selected == nil {
		return ListItem{}, false, nil
	}
	return model.selected.item, true, nil
}

// listModel 是 note list 的交互模型：picker 负责筛选与选择，选中就退出程序。
type listModel struct {
	picker   picker
	count    int
	selected *listEntry
}

func newListModel(items []list.Item) listModel {
	return listModel{
		picker: newPicker(items, 80, 24, listDelegate{}, nil),
		count:  len(items),
	}
}

func (m listModel) Init() tea.Cmd { return nil }

func (m listModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		// 顶部一行标题 + 空行 + 底部一行键位提示，剩下的给列表
		height := value.Height - 3
		if height < 3 {
			height = 3
		}
		m.picker.list.SetSize(value.Width, height)
		return m, nil
	case tea.KeyMsg:
		switch value.String() {
		case "ctrl+c":
			m.selected = nil
			return m, tea.Quit
		case "enter":
			if entry, ok := m.picker.selected().(listEntry); ok {
				m.selected = &entry
			}
			return m, tea.Quit
		}
	}

	next, handled, cmd := m.picker.update(msg)
	if !handled {
		// 未过滤时的 Esc：退出且不选
		m.selected = nil
		return m, tea.Quit
	}
	m.picker = next
	return m, cmd
}

func (m listModel) View() string {
	var b strings.Builder
	b.WriteString(styleBrand.Render("note list"))
	b.WriteString(styleMuted.Render(fmt.Sprintf(" · %d 篇", m.count)))
	b.WriteString("\n\n")
	b.WriteString(m.picker.view())
	b.WriteString("\n")
	b.WriteString(help([][2]string{{"输入", "筛选"}, {"↑/↓", "选择"}, {"Enter", "预览"}, {"Esc", "退出"}}))
	b.WriteString(styleMuted.Render(fmt.Sprintf("  %d/%d", m.picker.list.Paginator.Page+1, m.picker.list.Paginator.TotalPages)))
	return b.String()
}

// listEntry 把 ListItem 适配成 bubbles 的 list.Item。
type listEntry struct {
	item ListItem
}

func (e listEntry) FilterValue() string { return e.item.FilterTarget() }

func (e listEntry) Title() string       { return e.item.Title }
func (e listEntry) Description() string { return e.item.Summary }

// noteEntries 把列表项包成 bubbles 需要的 []list.Item。
func noteEntries(items []ListItem) []list.Item {
	entries := make([]list.Item, 0, len(items))
	for _, item := range items {
		entries = append(entries, listEntry{item: item})
	}
	return entries
}

// listDelegate 画 note list 的两行行：第一行标题 + 右对齐的「分类 · 时间」，
// 第二行摘要。恒为两行高，分页与视口才不会因为内容长度抖动。
type listDelegate struct{}

func (d listDelegate) Height() int  { return 2 }
func (d listDelegate) Spacing() int { return 0 }

func (d listDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d listDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(listEntry)
	if !ok {
		return
	}
	width := m.Width()
	if width <= 0 {
		// 还没拿到窗口尺寸时不画：画出一行超宽内容会把分页与视口算乱
		return
	}

	selected := index == m.Index()
	title := entry.item.Title
	if m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied {
		title = highlightMatches(title, m.MatchesForItem(index))
	}
	if selected {
		title = styleSelected.Render(title)
	}

	mark := "   "
	if selected {
		mark = " > "
	}
	// 标题可能比终端还宽：pickRow 会夹到列表宽度，否则这一项会折行、
	// 而 delegate 声明的 Height 仍是 2，分页与视口随之错位。
	line := pickRow(mark, title, styleMuted.Render(entry.item.meta()), width)

	summary := listRowSummary(entry.item.Summary, width-4)
	// 注意：这里不能补结尾换行——bubbles 自己在两项之间插 `Spacing()+1` 个换行
	//（见 list.go 的 populatedView），多发一个会让每项占 Height()+1 行，
	// 分页与视口按 Height() 算，`note list` 会因此溢出终端。
	fmt.Fprintf(w, "%s\n   %s", line, styleMuted.Render(summary))
}

// meta 是行右侧的辅助信息：分类 · 本地时间，加密笔记再追一个标记。
func (i ListItem) meta() string {
	category := i.CategoryName
	if category == "" {
		category = "未分类"
	}
	parts := []string{category, localStamp(i.UpdatedAt)}
	if i.Encrypted {
		parts = append(parts, "加密")
	}
	return strings.Join(parts, " · ")
}

// localStamp 把 UTC 秒级文本按本机时区显示到分钟（与 Web 端列表同口径）；
// 解析失败时原样返回，绝不吞掉一个看起来不对的时间。
func localStamp(value string) string {
	parsed, err := time.Parse("2006-01-02T15:04:05Z", value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

// listRowSummary 把库里的原始正文摘要压成单行并按显示宽度截断。
// 不折叠空白的话，带换行的摘要会把「每项两行高」撑坏，分页与视口都跟着错；
// 宽度预算非正（终端窄到放不下游标与缩进）时干脆不显示，
// 绝不能返回未截断的长串。
func listRowSummary(content string, width int) string {
	flat := strings.Join(strings.Fields(content), " ")
	if width <= 0 {
		return ""
	}
	return truncateDisplay(flat, width)
}

// truncateDisplay 按显示宽度截断（CJK 与 emoji 占两列），超长时补省略号。
func truncateDisplay(text string, limit int) string {
	if limit <= 0 || uniseg.StringWidth(text) <= limit {
		return text
	}

	var b strings.Builder
	used := 0
	graphemes := uniseg.NewGraphemes(text)
	for graphemes.Next() {
		width := graphemes.Width()
		if used+width > limit-1 { // 给省略号留一列
			break
		}
		b.WriteString(graphemes.Str())
		used += width
	}
	return b.String() + "…"
}
