package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-isatty"
)

// ErrNotTerminal 表示 stdin 不是终端：向导无法运行，调用方应报错而不是挂起。
var ErrNotTerminal = errors.New("note add 需要在交互式终端里运行")

// 校验口径与 internal/store 保持一致，一律按码点计数。
const (
	maxAddCategoryRunes = 30
	maxAddTitleRunes    = 200
	maxAddContentRunes  = 2000
)

// Category 是向导需要的最小分类信息；IsDefault 表示内置的「未分类」，
// 落库时它的 category_id 存 NULL，与 Web 端一致。
type Category struct {
	ID        int64
	Name      string
	Count     int
	IsDefault bool
}

// AddNote 是向导收集到的一条待创建笔记；CategoryID 为 nil 表示未分类。
type AddNote struct {
	Title      string
	Content    string
	CategoryID *int64
}

// NoteRef 是创建成功后的回执，用于打印结果。
type NoteRef struct {
	ID           string
	Title        string
	CategoryName string
}

// AddDeps 是向导接触外部世界的全部方式：cmd/note 用 store 实现，测试注入假实现。
// cli 因此不必 import internal/store。
type AddDeps struct {
	Categories     func(ctx context.Context) ([]Category, error)
	CreateCategory func(ctx context.Context, name string) (Category, error)
	CreateNote     func(ctx context.Context, in AddNote) (NoteRef, error)
}

// addStep 是向导所处的步骤。
type addStep int

const (
	stepCategory addStep = iota
	stepNewCategory
	stepTitle
	stepContent
)

// addModel 是单进程多步的 Elm 模型：切步只改 step 字段与焦点，不清屏。
type addModel struct {
	deps AddDeps
	step addStep

	all    []Category
	picker picker // 分类步的选择器：fuzzy 筛选、光标与高亮都归它
	chosen *Category

	create    textinput.Model
	createErr string

	title    textinput.Model
	titleErr string

	body textarea.Model

	status string // 底部一次性提示（保存失败、超限等）
	ref    NoteRef
	saved  bool

	width  int
	height int
}

// newAddModel 造一个只含默认值、尚未加载分类的模型。
func newAddModel(deps AddDeps) addModel {
	create := textinput.New()
	create.Placeholder = "分类名"
	create.CharLimit = maxAddCategoryRunes
	create.Prompt = "> "
	create.PromptStyle = stylePrompt

	title := textinput.New()
	title.Placeholder = "标题"
	title.CharLimit = maxAddTitleRunes
	title.Prompt = "> "
	title.PromptStyle = stylePrompt

	body := textarea.New()
	body.Placeholder = "正文（支持换行）"
	body.ShowLineNumbers = false
	body.Prompt = "" // 默认的 "┃ " 竖线在写正文时只是噪声

	m := addModel{
		deps:   deps,
		step:   stepCategory,
		picker: newCategoryPicker(nil, 80, 10),
		create: create,
		title:  title,
		body:   body,
		width:  80,
		height: 24,
	}
	m.layout()
	return m
}

// layout 按终端尺寸摆放输入组件；窄终端下夹到可用下限，避免出屏。
func (m *addModel) layout() {
	width := m.listWidth()
	m.create.Width = width
	m.title.Width = width
	m.picker.list.SetSize(width, m.categoryHeight())
	// 正文外面套一层细边框：左右各 1 列边框 + 1 列内边距 = 4 列，内容宽度要让出这 4 列。
	inner := width - 4
	if inner < 10 {
		inner = 10
	}
	m.body.SetWidth(inner)

	height := 10
	if m.height < 14 {
		height = 5
	}
	m.body.SetHeight(height)
}

// categoryHeight 是分类列表可用的行数：给顶部标题行、空行、底部键位提示与「+ 新建分类…」留位。
func (m addModel) categoryHeight() int {
	height := m.height - 6
	if height < 3 {
		height = 3
	}
	return height
}

// listWidth 是列表与输入框共用的内容宽度：给终端两侧各留 2 列，并夹到 [20, 80]。
// 重建选择器时也要走它，否则窄终端下新建分类后列宽会突然缩水。
func (m addModel) listWidth() int {
	width := m.width - 4
	if width < 20 {
		width = 20
	}
	if width > 80 {
		width = 80
	}
	return width
}

// Init 让第一步的输入组件一开始就闪烁游标；后续步进各自返回对应控件的 Blink。
func (m addModel) Init() tea.Cmd { return textinput.Blink }

func (m addModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.saved = false
			return m, tea.Quit
		}
		switch m.step {
		case stepCategory:
			return m.updateCategoryStep(msg)
		case stepNewCategory:
			return m.updateNewCategoryStep(msg)
		case stepTitle:
			return m.updateTitleStep(msg)
		default:
			return m.updateContentStep(msg)
		}
	case editorFinishedMsg:
		if msg.err != nil {
			m.status = "外接编辑器出错: " + msg.err.Error()
		} else {
			m.body.SetValue(strings.TrimRight(msg.content, "\n"))
		}
		if msg.path != "" {
			os.Remove(msg.path)
		}
		return m, nil
	}

	// 其它消息（光标闪烁等）交给当前聚焦的输入组件，否则光标只闪一下就不再动。
	var cmd tea.Cmd
	switch m.step {
	case stepCategory:
		m.picker, _, cmd = m.picker.update(msg)
	case stepNewCategory:
		m.create, cmd = m.create.Update(msg)
	case stepTitle:
		m.title, cmd = m.title.Update(msg)
	case stepContent:
		m.body, cmd = m.body.Update(msg)
	}
	return m, cmd
}

// startNewCategory 进入「新建分类」子流程，焦点从过滤框交给分类名输入框。
func (m addModel) startNewCategory() addModel {
	m.step = stepNewCategory
	m.createErr = ""
	m.create.SetValue("")
	m.picker.list.FilterInput.Blur()
	m.create.Focus()
	return m
}

// updateCategoryStep 把按键交给选择器；选择器说「我不管」的只有 Enter（确认）与未过滤时的 Esc（放弃）。
func (m addModel) updateCategoryStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	next, handled, cmd := m.picker.update(msg)
	m.picker = next
	if handled {
		return m, cmd
	}

	switch msg.String() {
	case "enter":
		entry, ok := m.picker.selected().(categoryEntry)
		if !ok {
			return m, nil
		}
		if entry.placeholder {
			return m.startNewCategory(), textinput.Blink
		}
		chosen := entry.category
		m.chosen = &chosen
		m.picker.list.FilterInput.Blur()
		m.step = stepTitle
		m.title.Focus()
		return m, textinput.Blink
	case "esc":
		m.saved = false
		return m, tea.Quit
	}
	return m, nil
}

func (m addModel) updateNewCategoryStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.createErr = ""
		m.create.Blur()
		// 过滤框还留在过滤态（list 的状态没变），不把焦点还回去的话
		// 之后敲字与退格都会被静默吞掉。
		m.picker.list.FilterInput.Focus()
		m.step = stepCategory
		return m, textinput.Blink
	case "enter":
		name := strings.TrimSpace(m.create.Value())
		if err := validateCategoryName(name); err != nil {
			m.createErr = err.Error()
			return m, nil
		}
		created, err := m.deps.CreateCategory(context.Background(), name)
		if err != nil {
			m.createErr = err.Error()
			return m, nil
		}
		m.createErr = ""
		m.create.Blur()
		m.all = append(m.all, created)
		// 重建选择器（过滤函数里钉住的是占位项下标，项数变了就得重来），
		// 过滤串顺带清空，再把光标落到新分类上，省得用户再找一次。
		m.picker = newCategoryPicker(m.all, m.listWidth(), m.categoryHeight())
		m.picker.list.Select(indexOfCategory(m.all, created.ID))
		m.chosen = &created
		m.step = stepCategory
		return m, nil
	}
	var cmd tea.Cmd
	m.create, cmd = m.create.Update(msg)
	return m, cmd
}

func (m addModel) updateTitleStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.saved = false
		return m, tea.Quit
	case "enter":
		if strings.TrimSpace(m.title.Value()) == "" {
			m.titleErr = "标题不能为空"
			return m, nil
		}
		m.titleErr = ""
		m.title.Blur()
		m.step = stepContent
		m.body.Focus()
		return m, textarea.Blink
	}
	var cmd tea.Cmd
	m.title, cmd = m.title.Update(msg)
	return m, cmd
}

func (m addModel) updateContentStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.saved = false
		return m, tea.Quit
	case "ctrl+d":
		if utf8.RuneCountInString(m.body.Value()) > maxAddContentRunes {
			m.status = fmt.Sprintf("正文最多 %d 码点", maxAddContentRunes)
			return m, nil
		}
		ref, err := m.deps.CreateNote(context.Background(), buildAddNote(m))
		if err != nil {
			m.status = "新建笔记失败: " + err.Error()
			return m, nil
		}
		m.ref, m.saved, m.status = ref, true, ""
		return m, tea.Quit
	case "ctrl+e":
		return m.openExternalEditor()
	}
	m.status = ""
	var cmd tea.Cmd
	m.body, cmd = m.body.Update(msg)
	return m, cmd
}

// editorFinishedMsg 是外接编辑器退出后的回执。
type editorFinishedMsg struct {
	path    string
	content string
	err     error
}

// openExternalEditor 把正文交给 $EDITOR/$VISUAL 编辑；未设置时只回显，不中断向导。
func (m addModel) openExternalEditor() (tea.Model, tea.Cmd) {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = strings.TrimSpace(os.Getenv("VISUAL"))
	}
	if editor == "" {
		m.status = "未设置 $EDITOR，无法外接编辑"
		return m, nil
	}

	file, err := os.CreateTemp("", "note-add-*.md")
	if err != nil {
		m.status = "新建临时文件失败: " + err.Error()
		return m, nil
	}
	path := file.Name()
	if _, err := file.WriteString(m.body.Value()); err != nil {
		file.Close()
		os.Remove(path)
		m.status = "写入临时文件失败: " + err.Error()
		return m, nil
	}
	file.Close()

	fields := strings.Fields(editor)
	args := append(append([]string{}, fields[1:]...), path)
	cmd := exec.Command(fields[0], args...)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		content, readErr := os.ReadFile(path)
		if err == nil {
			err = readErr
		}
		return editorFinishedMsg{path: path, content: string(content), err: err}
	})
}

// buildAddNote 把模型状态翻译成落库入参：未分类映射成 nil（与 Web 端一致）。
func buildAddNote(m addModel) AddNote {
	note := AddNote{
		Title:   strings.TrimSpace(m.title.Value()),
		Content: m.body.Value(),
	}
	if m.chosen != nil && !m.chosen.IsDefault {
		id := m.chosen.ID
		note.CategoryID = &id
	}
	return note
}

// isTerminal 判断文件描述符是否连接着终端。
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}

// RunAdd 加载分类后跑完整向导；saved=false 表示用户放弃（Esc/Ctrl+C）。
func RunAdd(ctx context.Context, deps AddDeps) (NoteRef, bool, error) {
	return runAddWizard(ctx, deps, isTerminal(os.Stdin))
}

// runAddWizard 是 RunAdd 的可测内核：interactive=false 时立刻返回 ErrNotTerminal。
func runAddWizard(ctx context.Context, deps AddDeps, interactive bool) (NoteRef, bool, error) {
	if !interactive {
		return NoteRef{}, false, ErrNotTerminal
	}
	cats, err := deps.Categories(ctx)
	if err != nil {
		return NoteRef{}, false, err
	}

	m := newAddModel(deps)
	m.all = cats
	m.picker = newCategoryPicker(cats, m.listWidth(), m.categoryHeight())

	program := tea.NewProgram(m, tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		return NoteRef{}, false, err
	}
	model, ok := final.(addModel)
	if !ok {
		return NoteRef{}, false, nil
	}
	return model.ref, model.saved, nil
}

func (m addModel) View() string {
	switch m.step {
	case stepCategory:
		return m.viewCategory()
	case stepNewCategory:
		return m.viewNewCategory()
	case stepTitle:
		return m.viewTitle()
	default:
		return m.viewContent()
	}
}

func (m addModel) viewCategory() string {
	var b strings.Builder
	b.WriteString(header("选择分类", "1/3"))
	b.WriteString("\n\n")
	b.WriteString(m.picker.view())
	b.WriteString("\n")
	b.WriteString(help([][2]string{{"输入", "过滤"}, {"↑/↓", "选择"}, {"Enter", "确认"}, {"Esc", "放弃"}}) + "\n")
	return b.String()
}

func (m addModel) viewNewCategory() string {
	var b strings.Builder
	b.WriteString(header("新建分类", ""))
	b.WriteString("\n\n")
	b.WriteString(m.create.View() + "\n")
	b.WriteString(counter(utf8.RuneCountInString(strings.TrimSpace(m.create.Value())), maxAddCategoryRunes, false) + "\n")
	if m.createErr != "" {
		b.WriteString(styleWarn.Render("! "+m.createErr) + "\n")
	}
	b.WriteString("\n" + help([][2]string{{"Enter", "创建"}, {"Esc", "返回分类列表"}}) + "\n")
	return b.String()
}

func (m addModel) viewTitle() string {
	var b strings.Builder
	b.WriteString(header("标题", "2/3"))
	if m.chosen != nil {
		b.WriteString(styleMuted.Render(" · 分类 " + m.chosen.Name))
	}
	b.WriteString("\n\n")
	b.WriteString(m.title.View() + "\n")
	b.WriteString(counter(utf8.RuneCountInString(strings.TrimSpace(m.title.Value())), maxAddTitleRunes, false) + "\n")
	if m.titleErr != "" {
		b.WriteString(styleWarn.Render("! "+m.titleErr) + "\n")
	}
	b.WriteString("\n" + help([][2]string{{"Enter", "下一步"}, {"Esc", "放弃"}}) + "\n")
	return b.String()
}

func (m addModel) viewContent() string {
	var b strings.Builder
	b.WriteString(header("正文", "3/3"))
	b.WriteString("\n")
	// gum write 的 header：正文框上方一行标题，后面带当前分类与已填标题的上下文。
	b.WriteString(styleBrand.Render("新建笔记"))
	if m.chosen != nil {
		b.WriteString(styleMuted.Render(" · 分类 " + m.chosen.Name))
	}
	if title := strings.TrimSpace(m.title.Value()); title != "" {
		b.WriteString(styleMuted.Render(" · 标题 " + title))
	}
	b.WriteString("\n")
	b.WriteString(styleBox.Render(m.body.View()) + "\n")
	used := utf8.RuneCountInString(m.body.Value())
	if used > maxAddContentRunes {
		b.WriteString(counter(used, maxAddContentRunes, true) + "\n")
		b.WriteString(styleWarn.Render(fmt.Sprintf("! 超出 %d 个码点，先删掉一些再用 Ctrl+D 保存", used-maxAddContentRunes)) + "\n")
	} else {
		b.WriteString(counter(used, maxAddContentRunes, false) + "\n")
	}
	if m.status != "" {
		b.WriteString(styleWarn.Render("! "+m.status) + "\n")
	}
	b.WriteString("\n" + help([][2]string{{"Enter", "换行"}, {"Ctrl+D", "完成"}, {"Ctrl+E", "用 $EDITOR 编辑"}, {"Esc", "放弃"}}) + "\n")
	return b.String()
}

// counter 渲染 "N/上限" 计数：未超限压暗，超限变红并带感叹号。
func counter(used, limit int, over bool) string {
	text := fmt.Sprintf("%d/%d", used, limit)
	if over {
		return "  " + styleWarn.Render("! "+text)
	}
	return "  " + styleMuted.Render(text)
}

// newCategoryRow 是分类列表末尾常驻的「就地新建」行。
const newCategoryRow = "+ 新建分类…"

// categoryEntry 把分类适配成 bubbles 的 list.Item；placeholder 表示末尾的「+ 新建分类…」行。
type categoryEntry struct {
	category    Category
	placeholder bool
}

func (e categoryEntry) Title() string {
	if e.placeholder {
		return newCategoryRow
	}
	return e.category.Name
}

func (e categoryEntry) FilterValue() string { return e.Title() }

// newCategoryPicker 造分类选择器：分类项 + 末尾常驻的「+ 新建分类…」。
// 过滤函数把最后一项钉住，因此任何过滤串下都能就地新建分类。
func newCategoryPicker(categories []Category, width, height int) picker {
	items := make([]list.Item, 0, len(categories)+1)
	for _, category := range categories {
		items = append(items, categoryEntry{category: category})
	}
	items = append(items, categoryEntry{placeholder: true})

	return newPicker(items, width, height, categoryDelegate{}, pinnedLastFilter(len(categories)))
}

// categoryDelegate 画分类行：名字（选中加粗、命中高亮）+ 右对齐的「N 篇」。
type categoryDelegate struct{}

func (d categoryDelegate) Height() int  { return 1 }
func (d categoryDelegate) Spacing() int { return 0 }

func (d categoryDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d categoryDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	entry, ok := item.(categoryEntry)
	if !ok {
		return
	}
	width := m.Width()
	if width <= 0 {
		return
	}

	selected := index == m.Index()
	name := entry.Title()
	if m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied {
		name = highlightMatches(name, m.MatchesForItem(index))
	}
	if selected {
		name = styleSelected.Render(name)
	}

	mark := "   "
	if selected {
		mark = " > "
	}
	line := styleCursor.Render(mark) + name

	// 占位行没有篇数，右对齐信息留空
	meta := ""
	if !entry.placeholder {
		meta = styleMuted.Render(fmt.Sprintf("%d 篇", entry.category.Count))
	}
	if gap := width - lipgloss.Width(line) - lipgloss.Width(meta); gap > 0 {
		line += strings.Repeat(" ", gap)
	}
	line += meta

	fmt.Fprintf(w, "%s\n", ansi.Truncate(line, width, "…"))
}

// validateCategoryName 校验新分类名：非空且不超过 30 码点。
func validateCategoryName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("分类名不能为空")
	}
	if utf8.RuneCountInString(name) > maxAddCategoryRunes {
		return fmt.Errorf("分类名最多 %d 个字符", maxAddCategoryRunes)
	}
	return nil
}

// indexOfCategory 返回指定 id 在列表中的下标，找不到返回 0。
func indexOfCategory(list []Category, id int64) int {
	for i, c := range list {
		if c.ID == id {
			return i
		}
	}
	return 0
}
