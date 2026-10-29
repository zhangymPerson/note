package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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

	all         []Category
	filtered    []Category
	filter      string // 与 filterInput.Value() 同步，供过滤与高亮使用
	filterInput textinput.Model
	cursor      int // 0..len(filtered)；等于 len(filtered) 表示末尾的「+ 新建分类…」行
	chosen      *Category

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
	filter := textinput.New()
	filter.Placeholder = "过滤分类…"
	filter.Prompt = "> "
	filter.PromptStyle = stylePrompt
	filter.Focus()

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

	m := addModel{deps: deps, step: stepCategory, filterInput: filter, create: create, title: title, body: body, width: 80, height: 24}
	m.layout()
	return m
}

// layout 按终端尺寸摆放输入组件；窄终端下夹到可用下限，避免出屏。
func (m *addModel) layout() {
	width := m.width - 4
	if width < 20 {
		width = 20
	}
	if width > 80 {
		width = 80
	}
	m.filterInput.Width = width
	m.create.Width = width
	m.title.Width = width
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

// Init 让分类步的过滤框一开始就闪烁游标；后续步进各自返回对应控件的 Blink。
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
		m.filterInput, cmd = m.filterInput.Update(msg)
	case stepNewCategory:
		m.create, cmd = m.create.Update(msg)
	case stepTitle:
		m.title, cmd = m.title.Update(msg)
	case stepContent:
		m.body, cmd = m.body.Update(msg)
	}
	return m, cmd
}

// applyFilter 重算过滤结果并把光标夹回合法范围。
func (m *addModel) applyFilter() {
	m.filtered = filterCategories(m.all, m.filter)
	if m.cursor > len(m.filtered) {
		m.cursor = len(m.filtered)
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// startNewCategory 进入「新建分类」子流程，焦点从过滤框交给分类名输入框。
func (m addModel) startNewCategory() addModel {
	m.step = stepNewCategory
	m.createErr = ""
	m.create.SetValue("")
	m.filterInput.Blur()
	m.create.Focus()
	return m
}

func (m addModel) updateCategoryStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.saved = false
		return m, tea.Quit
	case "up", "ctrl+k", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "ctrl+j", "ctrl+n":
		if m.cursor < len(m.filtered) {
			m.cursor++
		}
		return m, nil
	case "j", "k":
		// 过滤为空时 j/k 当上下移动（gum filter 的习惯）；已经在过滤就照常输入。
		if m.filter == "" {
			if msg.String() == "j" {
				if m.cursor < len(m.filtered) {
					m.cursor++
				}
			} else if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		}
	case "enter":
		if m.cursor >= len(m.filtered) {
			return m.startNewCategory(), textinput.Blink
		}
		chosen := m.filtered[m.cursor]
		m.chosen = &chosen
		m.filterInput.Blur()
		m.step = stepTitle
		m.title.Focus()
		return m, textinput.Blink
	}

	// 其余按键（可见字符与退格都算）进过滤框，列表随之重算。
	previous := m.filter
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filter = m.filterInput.Value()
	if m.filter != previous {
		m.cursor = 0
	}
	m.applyFilter()
	return m, cmd
}

func (m addModel) updateNewCategoryStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.createErr = ""
		m.create.Blur()
		m.filterInput.Focus()
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
		m.filter = ""
		m.cursor = 0
		m.applyFilter()
		m.cursor = indexOfCategory(m.filtered, created.ID)
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
	m.all, m.filtered = cats, cats

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
	b.WriteString(m.filterInput.View())
	b.WriteString("\n\n")
	for i, c := range m.filtered {
		b.WriteString(styleCursor.Render(cursorMark(i == m.cursor)))
		name := highlight(c.Name, m.filter)
		if i == m.cursor {
			name = styleSelected.Render(name)
		}
		b.WriteString(name + styleMuted.Render(fmt.Sprintf("  %d 篇", c.Count)) + "\n")
	}
	if m.cursor == len(m.filtered) {
		b.WriteString(styleCursor.Render(cursorMark(true)) + styleSelected.Render("+ 新建分类…") + "\n")
	} else {
		b.WriteString(styleCursor.Render(cursorMark(false)) + styleMuted.Render("+ 新建分类…") + "\n")
	}
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

// cursorMark 返回列表行前缀。整体比过滤框的 "> " 再缩一列，
// 免得输入框的提示符和选中行标记挤在同一列上、看不出哪个在动。
func cursorMark(selected bool) string {
	if selected {
		return " > "
	}
	return "   "
}

// counter 渲染 "N/上限" 计数：未超限压暗，超限变红并带感叹号。
func counter(used, limit int, over bool) string {
	text := fmt.Sprintf("%d/%d", used, limit)
	if over {
		return "  " + styleWarn.Render("! "+text)
	}
	return "  " + styleMuted.Render(text)
}

// filterCategories 按名字做不区分大小写的子串过滤，保持输入顺序。
func filterCategories(all []Category, query string) []Category {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return all
	}
	var out []Category
	for _, c := range all {
		if strings.Contains(strings.ToLower(c.Name), query) {
			out = append(out, c)
		}
	}
	return out
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
