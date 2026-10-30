package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/rivo/uniseg"
)

// ListItem 是 note list 的一行：只带展示需要的字段与过滤用的正文。
type ListItem struct {
	ID           string
	Title        string
	CategoryName string
	// Summary 是明文笔记正文的前 140 码点；加密笔记为空。
	Summary string
	// Content 是明文笔记的完整正文，只用于本地过滤（不进列表展示）；
	// 加密笔记为空——CLI 拿不到明文，只能按标题/分类命中。
	Content string
	// Encrypted 为真表示这条笔记在库里只有密文，CLI 解不开。
	Encrypted bool
	// Trashed 为真表示这条笔记在回收站里。
	Trashed bool
	// UpdatedAt 是 UTC 秒级文本，原样来自 store。
	UpdatedAt string
}

// FilterTarget 是 fuzzy 匹配的目标：标题 + 分类。
// 正文不进这个目标：fuzzy 是子序列匹配，套在长正文上几乎什么词都能命中，
// 列表会失去筛选意义；正文改由 noteFilter 做子串匹配（见该函数注释）。
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

// PickNote 是 note show 不带 id 时的选择器：走与 note list 同一个交互列表，
// 选中即返回那条笔记。调用方必须先确认终端可用——这里强制走交互路径，
// 不接受纯文本降级（note show 没有 id 时不该静默输出一屏列表）。
func PickNote(ctx context.Context, deps ListDeps) (ListResult, error) {
	return runList(ctx, deps, ListOptions{}, true)
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
		picker: newPicker(items, 80, 24, listDelegate{}, noteFilter(contentsOf(items))),
		count:  len(items),
	}
}

// contentsOf 取出每一项的完整正文，下标与 items 对齐（bubbles 的过滤器
// 收到的 targets 也是同样顺序，两者靠下标对应）。
func contentsOf(items []list.Item) []string {
	contents := make([]string, len(items))
	for i, item := range items {
		if entry, ok := item.(listEntry); ok {
			contents[i] = entry.item.Content
		}
	}
	return contents
}

// noteFilter 让「标题/分类 fuzzy」与「正文子串」都能命中：
// fuzzy 套在长正文上会把任何子序列都算命中，所以正文改用不区分大小写的子串；
// 标题/分类命中的项保持在前面（DefaultFilter 按匹配度排好），正文命中的追加在后面。
func noteFilter(contents []string) list.FilterFunc {
	return func(term string, targets []string) []list.Rank {
		ranks := list.DefaultFilter(term, targets)
		needle := strings.ToLower(strings.TrimSpace(term))
		if needle == "" {
			return ranks
		}

		matched := make(map[int]bool, len(ranks))
		for _, rank := range ranks {
			matched[rank.Index] = true
		}
		for i := range targets {
			if matched[i] || i >= len(contents) || contents[i] == "" {
				continue
			}
			if strings.Contains(strings.ToLower(contents[i]), needle) {
				// 正文命中不给 MatchedIndexes：那些下标是相对标题的，别拿去高亮
				ranks = append(ranks, list.Rank{Index: i})
			}
		}
		return ranks
	}
}

// snippetAround 取正文里命中词附近的一小段作为列表第二行：从命中处往前推 12 个
// 字符作为上下文起点，按显示宽度截断，前面被裁掉时补省略号。
// 返回空串表示正文里没有这个词（此时第二行照旧显示摘要）。
func snippetAround(content, term string, width int) string {
	flat := strings.Join(strings.Fields(content), " ")
	needle := []rune(strings.ToLower(strings.TrimSpace(term)))
	if flat == "" || len(needle) == 0 || width <= 0 {
		return ""
	}

	runes := []rune(flat)
	index := -1
	for i := 0; i+len(needle) <= len(runes); i++ {
		if runesMatchFold(runes[i:i+len(needle)], needle) {
			index = i
			break
		}
	}
	if index < 0 {
		return ""
	}

	start := 0
	if index > 12 {
		start = index - 12
	}
	snippet := truncateDisplay(string(runes[start:]), width)
	if start > 0 {
		snippet = "…" + snippet
	}
	return snippet
}

// highlightTerm 把 text 里出现的 term 染色（按 Unicode 小写比较，逐 rune 对齐，
// 不用 ToLower 切字节——那会在少数语言里错位）。
func highlightTerm(text, term string) string {
	needle := []rune(strings.ToLower(term))
	if len(needle) == 0 {
		return text
	}

	runes := []rune(text)
	var b strings.Builder
	for i := 0; i < len(runes); {
		if i+len(needle) <= len(runes) && runesMatchFold(runes[i:i+len(needle)], needle) {
			b.WriteString(styleMatch.Render(string(runes[i : i+len(needle)])))
			i += len(needle)
			continue
		}
		b.WriteRune(runes[i])
		i++
	}
	return b.String()
}

// runesMatchFold 判断 value 是否在忽略大小写后等于 lowered。
func runesMatchFold(value, lowered []rune) bool {
	for i, r := range value {
		if unicode.ToLower(r) != lowered[i] {
			return false
		}
	}
	return true
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
	// 过滤中与过滤已应用的两种状态都要算：match 只在这两种状态里可用
	term := ""
	matches := []int(nil)
	if state := m.FilterState(); state == list.Filtering || state == list.FilterApplied {
		term = strings.TrimSpace(m.FilterInput.Value())
		matches = m.MatchesForItem(index)
		title = highlightMatches(title, matches)
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

	// 第二行默认是摘要；如果过滤词只命中正文（标题没命中），就把摘要换成
	// 正文里命中处的那一小段并染色——否则用户看不出这条为什么被筛出来。
	summary := listRowSummary(entry.item.Summary, width-4)
	if len(matches) == 0 {
		if snippet := snippetAround(entry.item.Content, term, width-4); snippet != "" {
			summary = highlightTerm(snippet, term)
		}
	}
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
