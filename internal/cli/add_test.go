package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeBackend 是 AddDeps 的假实现：记录调用、按需返回错误，让模型逻辑可以脱离 TTY 与数据库测试。
type fakeBackend struct {
	createCategoryErr error
	createNoteErr     error

	createdCategories []string
	createdNotes      []AddNote
}

func (b *fakeBackend) categories(context.Context) ([]Category, error) {
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
	m.all, m.filtered = cats, cats
	m.width, m.height = 100, 30
	m.layout()
	return m
}

// press 把一个键名喂给模型；返回新模型（忽略命令，测试只关心状态）。
func press(m addModel, name string) addModel {
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
	next, _ := m.Update(msg)
	return next.(addModel)
}

// typeText 逐个码点喂给模型，模拟用户敲字。
func typeText(m addModel, text string) addModel {
	for _, r := range text {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(addModel)
	}
	return m
}

// pickCategory 在分类步选中指定名字的分类（跳过过滤逻辑）。
func pickCategory(t *testing.T, m addModel, name string) addModel {
	t.Helper()
	for i, c := range m.filtered {
		if c.Name == name {
			m.cursor = i
			m = press(m, "enter")
			if m.step != stepTitle {
				t.Fatalf("选分类后应进入标题步，当前 step=%v status=%q", m.step, m.status)
			}
			return m
		}
	}
	t.Fatalf("分类列表里没有 %q", name)
	return m
}

func TestFilterCategoriesMatchesIgnoringCase(t *testing.T) {
	all := []Category{{ID: 1, Name: "未分类", IsDefault: true}, {ID: 2, Name: "工作"}, {ID: 3, Name: "读书笔记"}}
	if got := filterCategories(all, "读"); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("过滤「读」应只剩读书笔记，得到 %+v", got)
	}
	if got := filterCategories(all, "工作"); len(got) != 1 || got[0].ID != 2 {
		t.Fatalf("过滤「工作」应只剩工作，得到 %+v", got)
	}
	if got := filterCategories(all, ""); len(got) != 3 {
		t.Fatalf("空过滤应返回全部，得到 %d 条", len(got))
	}
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

	// ↓ 到最后一行（+ 新建分类…）再回车
	for i := 0; i < len(m.filtered); i++ {
		m = press(m, "down")
	}
	if m.cursor != len(m.filtered) {
		t.Fatalf("↓ 到底应停在「+ 新建分类…」行，cursor=%d", m.cursor)
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
	m.cursor = len(m.filtered)
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

func TestEmptyFilterResultFallsOnNewCategoryRow(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	m = typeText(m, "不存在")
	m = press(m, "down") // 已经只剩「+ 新建分类…」一行，↓ 应被夹住

	if len(m.filtered) != 0 {
		t.Fatalf("无匹配时应过滤到空，得到 %+v", m.filtered)
	}
	if m.cursor != len(m.filtered) {
		t.Fatalf("cursor 应停在「+ 新建分类…」行，得到 %d", m.cursor)
	}
	m = press(m, "enter")
	if m.step != stepNewCategory {
		t.Fatalf("过滤到空时回车应进入新建分类步，当前 step=%v", m.step)
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

func TestMatchRangeFindsCaseInsensitiveSubstring(t *testing.T) {
	start, end, ok := matchRange("读书笔记", "笔")
	if !ok || start != 6 || end != 9 {
		t.Fatalf("应在「笔」处命中（字节 6..9），得到 %d,%d,%v", start, end, ok)
	}
	if _, _, ok := matchRange("工作", "work"); ok {
		t.Fatal("不匹配时不该命中")
	}
	if _, _, ok := matchRange("工作", "  "); ok {
		t.Fatal("空查询不该命中整行")
	}
}

func TestCategoryStepRoutesTypingToFilterInput(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	if !m.filterInput.Focused() {
		t.Fatal("分类步的过滤框应处于聚焦状态（游标才会闪烁）")
	}

	m = typeText(m, "读")
	if m.filter != "读" {
		t.Fatalf("过滤框应收到输入，filter=%q", m.filter)
	}
	if len(m.filtered) != 1 || m.filtered[0].Name != "读书笔记" {
		t.Fatalf("应过滤到读书笔记，得到 %+v", m.filtered)
	}

	m = press(m, "backspace")
	if m.filter != "" || len(m.filtered) != 3 {
		t.Fatalf("退格应清空过滤并复原列表，filter=%q len=%d", m.filter, len(m.filtered))
	}
}

func TestCategoryStepKeepsJAndKForNavigationWhenFilterEmpty(t *testing.T) {
	m := newTestAddModel(&fakeBackend{})
	m = typeText(m, "j")
	if m.cursor != 1 || m.filter != "" {
		t.Fatalf("过滤为空时 j 应向下移动，cursor=%d filter=%q", m.cursor, m.filter)
	}
	m = typeText(m, "工")
	m = typeText(m, "j")
	if m.filter != "工j" {
		t.Fatalf("过滤非空时 j 应进入过滤串，filter=%q", m.filter)
	}
}
