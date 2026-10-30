package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fakeBackend 是 AddDeps 的假实现：记录调用、按需返回错误，让模型逻辑可以脱离 TTY 与数据库测试。
type fakeBackend struct {
	createCategoryErr error
	createNoteErr     error

	createdCategories []string
	createdNotes      []AddNote
	// noCategories 为真时返回空分类列表（覆盖「一个分类都没有」的最短路径）
	noCategories bool
}

func (b *fakeBackend) categories(context.Context) ([]Category, error) {
	if b.noCategories {
		return []Category{}, nil
	}
	return []Category{
		{ID: 1, Name: "未分类", Count: 3, IsDefault: true},
		{ID: 2, Name: "工作", Count: 5},
		{ID: 3, Name: "读书笔记"},
	}, nil
}

func (b *fakeBackend) createCategory(_ context.Context, name string) (Category, error) {
	if b.createCategoryErr != nil {
		return Category{}, b.createCategoryErr
	}
	b.createdCategories = append(b.createdCategories, name)
	return Category{ID: int64(100 + len(b.createdCategories)), Name: name}, nil
}

func (b *fakeBackend) createNote(_ context.Context, in AddNote) (NoteRef, error) {
	if b.createNoteErr != nil {
		return NoteRef{}, b.createNoteErr
	}
	b.createdNotes = append(b.createdNotes, in)
	return NoteRef{ID: "note-1", Title: in.Title, CategoryName: "工作"}, nil
}

func (b *fakeBackend) deps() AddDeps {
	return AddDeps{
		Categories:     b.categories,
		CreateCategory: b.createCategory,
		CreateNote:     b.createNote,
	}
}

// newTestAddModel 造一个已加载分类、绑定假实现、尺寸固定的模型。
func newTestAddModel(b *fakeBackend) addModel {
	deps := b.deps()
	m := newAddModel(deps)
	cats, err := deps.Categories(context.Background())
	if err != nil {
		panic(err)
	}
	m.all = cats
	m.picker = newCategoryPicker(cats, 80, 10)
	m.width, m.height = 100, 30
	// 光标闪烁在真实运行时是后台定时器（530ms 一跳），测试里全部关掉，
	// 否则每次敲键都要白等一个 tick。
	m.picker.list.FilterInput.Cursor.SetMode(cursor.CursorStatic)
	m.create.Cursor.SetMode(cursor.CursorStatic)
	m.title.Cursor.SetMode(cursor.CursorStatic)
	m.body.Cursor.SetMode(cursor.CursorStatic)
	m.layout()
	return m
}

// press 把一个键名喂给模型，并把命令跑到底（过滤结果要等 FilterMatchesMsg 落地）。
func press(m addModel, name string) addModel {
	next, cmd := pressCmd(m, name)
	return settleAdd(next, cmd)
}

// keyMsgFor 把键名翻译成 tea.KeyMsg。
func keyMsgFor(name string) tea.KeyMsg {
	var msg tea.KeyMsg
	switch name {
	case "enter", "esc", "backspace", "tab", "up", "down":
		msg = tea.KeyMsg{Type: tea.KeyType(map[string]tea.KeyType{
			"enter": tea.KeyEnter, "esc": tea.KeyEsc, "backspace": tea.KeyBackspace,
			"tab": tea.KeyTab, "up": tea.KeyUp, "down": tea.KeyDown,
		}[name])}
	default:
		switch name {
		case "ctrl+j":
			msg = tea.KeyMsg{Type: tea.KeyCtrlJ}
		case "ctrl+k":
			msg = tea.KeyMsg{Type: tea.KeyCtrlK}
		case "ctrl+d":
			msg = tea.KeyMsg{Type: tea.KeyCtrlD}
		case "ctrl+e":
			msg = tea.KeyMsg{Type: tea.KeyCtrlE}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		default:
			panic("未知键名: " + name)
		}
	}
	return msg
}

// feed 喂一条消息并把它产生的命令跑到底：bubbles 的过滤结果要等 FilterMatchesMsg
// 回来才落地，不跑命令看到的还是旧列表。
func feed(m addModel, msg tea.Msg) addModel {
	updated, cmd := m.Update(msg)
	next, ok := updated.(addModel)
	if !ok {
		return m
	}
	return settleAdd(next, cmd)
}

// pressCmd 与 press 一样喂按键，但把命令原样返回：放弃向导这类行为只能从命令看出来。
func pressCmd(m addModel, name string) (addModel, tea.Cmd) {
	updated, cmd := m.Update(keyMsgFor(name))
	next, ok := updated.(addModel)
	if !ok {
		return m, cmd
	}
	return next, cmd
}

func settleAdd(m addModel, cmd tea.Cmd) addModel {
	if cmd == nil {
		return m
	}
	switch value := cmd().(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, sub := range value {
			m = settleAdd(m, sub)
		}
		return m
	case list.FilterMatchesMsg:
		updated, next := m.Update(value)
		model, ok := updated.(addModel)
		if !ok {
			return m
		}
		return settleAdd(model, next)
	default:
		// 其余命令（光标闪烁的初始消息）与过滤无关，喂回去会拉起定时器
		return m
	}
}

// typeText 逐个码点喂给模型，模拟用户敲字。
func typeText(m addModel, text string) addModel {
	for _, r := range text {
		m = feed(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// pickCategory 在分类步选中指定名字的分类：敲名字把它筛出来再回车。
func pickCategory(t *testing.T, m addModel, name string) addModel {
	t.Helper()
	m = typeText(m, name)
	m = press(m, "enter")
	if m.step != stepTitle {
		t.Fatalf("选分类后应进入标题步，当前 step=%v status=%q", m.step, m.status)
	}
	return m
}

// categoryNames 返回分类步当前可见的行（含常驻的「+ 新建分类…」占位项）。
func categoryNames(m addModel) []string {
	items := m.picker.list.VisibleItems()
	names := make([]string, 0, len(items))
	for _, item := range items {
		entry, ok := item.(categoryEntry)
		if !ok {
			continue
		}
		names = append(names, entry.Title())
	}
	return names
}

func TestValidateCategoryNameRuneBoundaries(t *testing.T) {
	if err := validateCategoryName(strings.Repeat("笔", 30)); err != nil {
		t.Fatalf("30 码点应通过，得到 %v", err)
	}
	if err := validateCategoryName(strings.Repeat("笔", 31)); err == nil {
		t.Fatal("31 码点应被拒绝")
	}
	if err := validateCategoryName("   "); err == nil {
		t.Fatal("空白分类名应被拒绝")
	}
}

func TestNewCategorySuccessReturnsToCategoryAndSelectsIt(t *testing.T) {
	backend := &fakeBackend{}
	m := newTestAddModel(backend)

	// 敲一个匹配不上的串，把列表收敛到只剩占位项，再回车
	m = typeText(m, "新分类前先清场")
	if got := categoryNames(m); len(got) != 1 || got[0] != newCategoryRow {
		t.Fatalf("应当只剩占位项，得到 %v", got)
	}
	m = press(m, "enter")
	if m.step != stepNewCategory {
		t.Fatalf("回车应进入新建分类步，当前 step=%v", m.step)
	}

	m = typeText(m, "随手记")
	m = press(m, "enter")

	if m.step != stepCategory {
		t.Fatalf("新建成功后应回到分类步，当前 step=%v", m.step)
	}
	if m.createErr != "" {
		t.Fatalf("成功后不该有回显，得到 %q", m.createErr)
	}
	if len(backend.createdCategories) != 1 || backend.createdCategories[0] != "随手记" {
		t.Fatalf("CreateCategory 应收到「随手记」，得到 %v", backend.createdCategories)
	}
	if m.chosen == nil || m.chosen.Name != "随手记" || m.chosen.ID != 101 {
		t.Fatalf("新分类应被选中，chosen=%+v", m.chosen)
	}
	if len(m.all) != 4 {
		t.Fatalf("新分类应加入列表，得到 %d 条", len(m.all))
	}
}

func TestNewCategoryRejectionKeepsStep(t *testing.T) {
	backend := &fakeBackend{createCategoryErr: errors.New("分类「工作」已存在")}
	m := newTestAddModel(backend)
	m = typeText(m, "新建入口")
	m = press(m, "enter")

	m = typeText(m, "工作")
	m = press(m, "enter")

	if m.step != stepNewCategory {
		t.Fatalf("创建失败应停在本步，当前 step=%v", m.step)
	}
	if m.createErr != "分类「工作」已存在" {
		t.Fatalf("应原样回显错误，得到 %q", m.createErr)
	}
	if len(m.all) != 3 {
		t.Fatalf("失败不该改动分类列表，得到 %d 条", len(m.all))
	}
}

func TestAddCategoryStepKeepsCreateRow(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})

	m = typeText(m, "谁都匹配不上")
	if got := categoryNames(m); len(got) != 1 || got[0] != newCategoryRow {
		t.Fatalf("零命中时应当只剩占位项，得到 %v", got)
	}

	m = press(m, "enter")
	if m.step != stepNewCategory {
		t.Fatalf("零命中时回车应当进新建分类步，当前 step=%v", m.step)
	}
}

func TestTitleStepRequiresNonEmpty(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	m = pickCategory(t, m, "工作")

	m = typeText(m, "   ")
	m = press(m, "enter")
	if m.step != stepTitle {
		t.Fatalf("空标题应停在标题步，当前 %v", m.step)
	}
	if m.titleErr == "" {
		t.Fatal("空标题应给出回显")
	}

	m = typeText(m, "会议纪要")
	m = press(m, "enter")
	if m.step != stepContent {
		t.Fatalf("合法标题应进入正文步，当前 %v", m.step)
	}
	if m.titleErr != "" {
		t.Fatalf("合法标题不该带回显，得到 %q", m.titleErr)
	}
}

func TestContentSavesWithSelectedCategory(t *testing.T) {
	backend := &fakeBackend{}
	m := newTestAddModel(backend)
	m = pickCategory(t, m, "工作")
	m = typeText(m, "会议纪要")
	m = press(m, "enter")
	m = typeText(m, "第一行")
	m = press(m, "enter")
	m = typeText(m, "第二行")
	m = press(m, "ctrl+d")

	if !m.saved {
		t.Fatalf("Ctrl+D 应保存成功，status=%q", m.status)
	}
	if len(backend.createdNotes) != 1 {
		t.Fatalf("CreateNote 应被调用一次，得到 %d 次", len(backend.createdNotes))
	}
	got := backend.createdNotes[0]
	if got.Title != "会议纪要" {
		t.Fatalf("标题 = %q", got.Title)
	}
	if got.Content != "第一行\n第二行" {
		t.Fatalf("正文应保留换行，得到 %q", got.Content)
	}
	if got.CategoryID == nil || *got.CategoryID != 2 {
		t.Fatalf("应写入所选分类 2，得到 %v", got.CategoryID)
	}
	if m.ref.ID == "" {
		t.Fatal("成功回执应带上笔记 id")
	}
}

func TestContentSavesDefaultAsNilCategory(t *testing.T) {
	backend := &fakeBackend{}
	m := newTestAddModel(backend)
	m = pickCategory(t, m, "未分类")
	m = typeText(m, "随手记")
	m = press(m, "enter")
	m = typeText(m, "正文")
	m = press(m, "ctrl+d")

	if len(backend.createdNotes) != 1 {
		t.Fatalf("CreateNote 应被调用一次，得到 %d 次", len(backend.createdNotes))
	}
	if backend.createdNotes[0].CategoryID != nil {
		t.Fatalf("未分类应写 nil，得到 %v", *backend.createdNotes[0].CategoryID)
	}
}

func TestContentRuneLimitBlocksSave(t *testing.T) {
	backend := &fakeBackend{}
	m := newTestAddModel(backend)
	m = pickCategory(t, m, "未分类")
	m = typeText(m, "边界")
	m = press(m, "enter")

	m.body.SetValue(strings.Repeat("字", maxAddContentRunes)) // 恰好 2000 码点
	m = press(m, "ctrl+d")
	if len(backend.createdNotes) != 1 {
		t.Fatalf("2000 码点应能保存，status=%q", m.status)
	}

	m2 := newTestAddModel(backend)
	m2 = pickCategory(t, m2, "未分类")
	m2 = typeText(m2, "超限")
	m2 = press(m2, "enter")
	m2.body.SetValue(strings.Repeat("😀", maxAddContentRunes+1)) // emoji 也是 1 码点
	m2 = press(m2, "ctrl+d")
	if len(backend.createdNotes) != 1 {
		t.Fatal("2001 码点不该落库")
	}
	if m2.status == "" {
		t.Fatal("超限应给出回显")
	}
	if m2.step != stepContent {
		t.Fatalf("超限应停在正文步，当前 %v", m2.step)
	}
}

func TestContentSaveFailureKeepsEditing(t *testing.T) {
	backend := &fakeBackend{createNoteErr: errors.New("database is locked")}
	m := newTestAddModel(backend)
	m = pickCategory(t, m, "未分类")
	m = typeText(m, "标题")
	m = press(m, "enter")
	m = typeText(m, "正文")
	m = press(m, "ctrl+d")

	if m.saved {
		t.Fatal("保存失败不该标记为已保存")
	}
	if m.step != stepContent {
		t.Fatalf("保存失败应停在正文步，当前 %v", m.step)
	}
	if !strings.Contains(m.status, "database is locked") {
		t.Fatalf("应回显错误原因，得到 %q", m.status)
	}
}

func TestEscAbandonsWithoutSaving(t *testing.T) {
	backend := &fakeBackend{}
	m := newTestAddModel(backend)
	m = pickCategory(t, m, "工作")
	m = press(m, "esc")

	if m.saved {
		t.Fatal("Esc 不该保存")
	}
	if len(backend.createdNotes) != 0 {
		t.Fatalf("Esc 后不该调用 CreateNote，得到 %d 次", len(backend.createdNotes))
	}
}

func TestRunAddRefusesNonTerminalStdin(t *testing.T) {
	_, saved, err := runAddWizard(context.Background(), (&fakeBackend{}).deps(), false)
	if !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("非终端应返回 ErrNotTerminal，得到 %v", err)
	}
	if saved {
		t.Fatal("非终端不该保存")
	}
}

func TestBuildAddNoteDropsDefaultCategoryID(t *testing.T) {
	def := Category{ID: 1, Name: "未分类", IsDefault: true}
	m := newTestAddModel(&fakeBackend{})
	m.chosen = &def
	m.title.SetValue("标题")
	m.body.SetValue("正文")
	if got := buildAddNote(m); got.CategoryID != nil {
		t.Fatalf("未分类应得到 nil，得到 %v", *got.CategoryID)
	}

	work := Category{ID: 2, Name: "工作"}
	m.chosen = &work
	got := buildAddNote(m)
	if got.CategoryID == nil || *got.CategoryID != 2 {
		t.Fatalf("应指向分类 2，得到 %v", got.CategoryID)
	}
	if got.Title != "标题" || got.Content != "正文" {
		t.Fatalf("标题/正文透传错误: %+v", got)
	}
}

func TestAddCategoryStepFuzzyFilters(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	if !m.picker.list.FilterInput.Focused() {
		t.Fatal("分类步的过滤框应当在聚焦状态（否则光标不闪、敲字也进不去）")
	}

	m = typeText(m, "读书")
	got := categoryNames(m)
	if len(got) != 2 || got[0] != "读书笔记" || got[1] != newCategoryRow {
		t.Fatalf("应当只筛出「读书笔记」并保留占位项，得到 %v", got)
	}
	if m.picker.list.FilterValue() != "读书" {
		t.Fatalf("过滤串应当是「读书」，得到 %q", m.picker.list.FilterValue())
	}

	m = press(m, "backspace")
	m = press(m, "backspace")
	if got := categoryNames(m); len(got) != 4 {
		t.Fatalf("退格删完过滤串后应当恢复全部三项分类 + 占位项，得到 %v", got)
	}
}

func TestAddCategoryStepTreatsJAndKAsFilterChars(t *testing.T) {
	for _, letter := range []string{"j", "k"} {
		m := newTestAddModel(&fakeBackend{})

		m = typeText(m, letter)
		if got := m.picker.list.FilterValue(); got != letter {
			t.Fatalf("%s 应当是过滤字符（不再当导航键），得到过滤串 %q", letter, got)
		}
		if got := categoryNames(m); len(got) != 1 || got[0] != newCategoryRow {
			t.Fatalf("「%s」匹配不上任何分类，应当只剩占位项，得到 %v", letter, got)
		}
	}
}

func TestAddCategoryStepEscAbandonsOnlyWhenFilterEmpty(t *testing.T) {
	// 过滤中：Esc 只清过滤串，向导不退出
	m := newTestAddModel(&fakeBackend{})
	m = typeText(m, "读")
	m, cmd := pressCmd(m, "esc")
	if cmd != nil {
		t.Fatal("过滤中的 Esc 不该让向导退出")
	}
	if m.step != stepCategory {
		t.Fatalf("过滤中的 Esc 应当留在分类步，当前 step=%v", m.step)
	}
	if got := m.picker.list.FilterValue(); got != "" {
		t.Fatalf("过滤中的 Esc 应当清空过滤串，得到 %q", got)
	}

	// 未过滤：再按一次才放弃
	m, cmd = pressCmd(m, "esc")
	if cmd == nil {
		t.Fatal("未过滤时的 Esc 应当让向导退出")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Fatalf("未过滤时的 Esc 应当发出 tea.Quit，得到 %T", msg)
	}
	if m.saved {
		t.Fatal("放弃向导时 saved 应当为 false")
	}
}

func TestAddCategoryStepKeepsTypingAfterEscFromNewCategory(t *testing.T) {
	// 从新建分类步 Esc 返回后，过滤框必须拿回焦点：
	// 否则它在过滤态里被 blur，敲字与退格会被静默吞掉。
	m := newTestAddModel(&fakeBackend{})
	m = typeText(m, "新建入口")
	m = press(m, "enter")
	if m.step != stepNewCategory {
		t.Fatalf("应当进新建分类步，当前 step=%v", m.step)
	}

	m = press(m, "esc")
	if m.step != stepCategory {
		t.Fatalf("Esc 应当回到分类步，当前 step=%v", m.step)
	}

	m = typeText(m, "读")
	if got := m.picker.list.FilterValue(); got != "新建入口读" {
		t.Fatalf("返回后敲字应当接在原有过滤串后面（说明没被吞掉），得到 %q", got)
	}
}

func TestAddCategoryStepNewCategoryBecomesSelected(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	m = typeText(m, "新建入口")
	m = press(m, "enter")
	m = typeText(m, "随手记")
	m = press(m, "enter")

	if got := m.picker.list.FilterValue(); got != "" {
		t.Fatalf("新建成功后过滤串应当被清空，得到 %q", got)
	}
	if got := m.picker.list.FilterState(); got != list.Unfiltered {
		t.Fatalf("新建成功后应当回到未过滤态，得到 %v", got)
	}
	entry, ok := m.picker.selected().(categoryEntry)
	if !ok || entry.category.ID != 101 {
		t.Fatalf("光标应当落在新建的分类上，得到 %+v", m.picker.selected())
	}
}

func TestAddCategoryStepWithNoCategoriesShowsCreateRow(t *testing.T) {
	m := newTestAddModel(&fakeBackend{noCategories: true})

	if got := categoryNames(m); len(got) != 1 || got[0] != newCategoryRow {
		t.Fatalf("一个分类都没有时应当只剩占位项，得到 %v", got)
	}
	m = press(m, "enter")
	if m.step != stepNewCategory {
		t.Fatalf("回车应当进新建分类步，当前 step=%v", m.step)
	}
}

func TestCategoryStepViewFitsTerminal(t *testing.T) {
	// 与 note list 同一道门道：delegate 不能多发结尾换行，否则每分类多占一行、整个向导溢出终端。
	m := newTestAddModel(&fakeBackend{})
	view := m.viewCategory()
	if got := lipgloss.Height(view); got > m.height {
		t.Fatalf("分类步视图占 %d 行，超过终端 %d 行：\n%s", got, m.height, view)
	}

	lines := strings.Split(m.picker.view(), "\n")
	first, second := lineOf(lines, "未分类"), lineOf(lines, "工作")
	if first < 0 || second < 0 {
		t.Fatalf("列表里找不到头两行分类：\n%s", m.picker.view())
	}
	if second-first != 1 {
		t.Fatalf("相邻两行分类应当紧挨着（各自一行），得到 %d..%d：\n%s", first, second, m.picker.view())
	}
	// 选择器视图的第一行就是第一个分类：未过滤时的空标题区必须已被剪掉，
	// 否则分类步会多出两行空白
	if first != 0 {
		t.Fatalf("第一行分类应当在视图首行，得到第 %d 行：\n%s", first, m.picker.view())
	}
}

func TestCategoryStepViewShowsCountsAndNoCountForCreateRow(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	view := ansi.Strip(m.viewCategory())

	if !strings.Contains(view, "选择分类") {
		t.Fatalf("分类步应当有向导自己的头部：\n%s", view)
	}
	if !strings.Contains(view, "读书笔记") || !strings.Contains(view, "3 篇") {
		t.Fatalf("分类行应当带篇数：\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, newCategoryRow) && strings.Contains(line, "篇") {
			t.Fatalf("占位行不该有篇数：%q", line)
		}
	}
}

func TestBlinkMessagesReachFocusedInputOnEveryStep(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	// 这条用例专测闪烁链，把 newTestAddModel 关掉的闪烁再打开
	m.picker.list.FilterInput.Cursor.SetMode(cursor.CursorBlink)
	m.title.Cursor.SetMode(cursor.CursorBlink)
	m.body.Cursor.SetMode(cursor.CursorBlink)

	// 分类步的过滤框只在过滤态可见（spec §9.4 的观感），因此只在这一态验证闪烁链没断。
	m = typeText(m, "读")
	if _, cmd := m.Update(textinput.Blink()); cmd == nil {
		t.Fatal("分类步过滤态应把闪烁消息转给过滤框并继续排下一拍")
	}
	m = press(m, "esc")

	title := pickCategory(t, m, "工作")
	if _, cmd := title.Update(textinput.Blink()); cmd == nil {
		t.Fatal("标题步应把闪烁消息转给标题输入框")
	}

	body := press(typeText(title, "会议纪要"), "enter")
	if _, cmd := body.Update(textarea.Blink()); cmd == nil {
		t.Fatal("正文步应把闪烁消息转给 textarea")
	}
}

func TestContentStepHasHeaderAndBorderedBox(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	view := ansi.Strip(press(typeText(pickCategory(t, m, "工作"), "会议纪要"), "enter").View())

	if !strings.Contains(view, "新建笔记") {
		t.Fatalf("正文步顶部应有「新建笔记」header:\n%s", view)
	}
	for _, rune := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(view, rune) {
			t.Fatalf("正文框缺少圆角边框 %q:\n%s", rune, view)
		}
	}
}
