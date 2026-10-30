package cli

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// stubItem 是 picker 用例的最小 item：picker 只要求 list.Item 的三件套。
type stubItem struct {
	title string
	value string
}

func (i stubItem) Title() string       { return i.title }
func (i stubItem) Description() string { return "" }
func (i stubItem) FilterValue() string { return i.value }

func stubItems(titles ...string) []list.Item {
	items := make([]list.Item, 0, len(titles))
	for _, title := range titles {
		items = append(items, stubItem{title: title, value: title})
	}
	return items
}

func newTestPicker(titles ...string) picker {
	p := newPicker(stubItems(titles...), 80, 24, list.NewDefaultDelegate(), nil)
	// 光标闪烁在真实运行时是后台定时器（530ms 一跳）；测试里把模式改成静态，
	// 否则每敲一个键都要白等一个 tick。
	p.list.FilterInput.Cursor.SetMode(cursor.CursorStatic)
	return p
}

// keyMsg 造按键消息；单独的空格在 bubbletea 里是 KeySpace 而不是 KeyRunes。
func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// apply 喂一条消息，只关心「谁处理了它」。
func apply(p picker, msg tea.Msg) (picker, bool) {
	next, handled, _ := p.update(msg)
	return next, handled
}

// applyCmd 喂一条消息并把产生的命令跑到底：bubbles 的过滤结果要等
// filterItems 产出的 FilterMatchesMsg 回来才落地，可见项与选中项都受它影响。
func applyCmd(t *testing.T, p picker, msg tea.Msg) picker {
	t.Helper()
	next, _, cmd := p.update(msg)
	return settle(t, next, cmd)
}

func settle(t *testing.T, p picker, cmd tea.Cmd) picker {
	t.Helper()
	if cmd == nil {
		return p
	}
	msg := cmd()
	switch value := msg.(type) {
	case nil:
		return p
	case tea.BatchMsg:
		for _, sub := range value {
			p = settle(t, p, sub)
		}
		return p
	default:
		next, _, _ := p.update(msg)
		return next
	}
}

func TestPickerTypingEntersFilterModeForEveryLetter(t *testing.T) {
	// 默认绑定里 j/k/h/l/b/u/f/d/g/G 都是导航键，只要漏掉一个，
	// 那个字母就没法当过滤字符输入。
	for _, letter := range []string{"j", "k", "h", "l", "b", "u", "f", "d", "g", "G", " "} {
		p := newTestPicker("会议纪要", "工作笔记", "读书笔记")
		p, handled := apply(p, keyMsg(letter))
		if !handled {
			t.Fatalf("按键 %q 应当被 picker 处理", letter)
		}
		if got := p.list.FilterState(); got != list.Filtering {
			t.Fatalf("按键 %q 后应当进入过滤态，得到 %v", letter, got)
		}
		if !strings.Contains(p.list.FilterValue(), letter) {
			t.Fatalf("按键 %q 应当进过滤串，得到 %q", letter, p.list.FilterValue())
		}
	}
}

func TestPickerArrowsMoveWhileFiltering(t *testing.T) {
	p := newTestPicker("一", "二", "三")

	p, handled := apply(p, keyMsg("/"))
	if !handled {
		t.Fatal("/ 应当被 picker 处理")
	}
	if p.list.FilterState() != list.Filtering {
		t.Fatalf("/ 应当进入过滤态，得到 %v", p.list.FilterState())
	}
	if p.list.FilterValue() != "" {
		t.Fatalf("/ 不该进过滤串，得到 %q", p.list.FilterValue())
	}

	p, handled = apply(p, keyMsg("down"))
	if !handled {
		t.Fatal("过滤态的 down 应当被 picker 处理")
	}
	p, _ = apply(p, keyMsg("down"))
	if got := p.list.Index(); got != 2 {
		t.Fatalf("过滤态按两次 down 后应当停在第 3 项，得到 %d", got)
	}
	p, _ = apply(p, keyMsg("up"))
	if got := p.list.Index(); got != 1 {
		t.Fatalf("再按一次 up 应当回到第 2 项，得到 %d", got)
	}
}

func TestPickerEnterReturnsSelectedImmediately(t *testing.T) {
	p := newTestPicker("会议纪要", "工作笔记", "读书笔记")

	p = applyCmd(t, p, keyMsg("工"))
	if got := p.list.FilterValue(); got != "工" {
		t.Fatalf("过滤串应当是「工」，得到 %q", got)
	}

	p, handled := apply(p, keyMsg("enter"))
	if handled {
		t.Fatal("Enter 应当交回外层模型（由它决定是选中还是预览）")
	}
	if got := p.list.FilterState(); got == list.FilterApplied {
		t.Fatal("Enter 不该把列表推进 FilterApplied 态（那样要再按一次才算确认）")
	}
	item, ok := p.selected().(stubItem)
	if !ok || item.title != "工作笔记" {
		t.Fatalf("选中的应当是「工作笔记」，得到 %+v", p.selected())
	}
}

func TestPickerEscClearsFilterThenReportsUnhandled(t *testing.T) {
	p := newTestPicker("一", "二")

	p, _ = apply(p, keyMsg("一"))
	if p.list.FilterState() != list.Filtering {
		t.Fatalf("敲字后应当进入过滤态，得到 %v", p.list.FilterState())
	}

	p, handled := apply(p, keyMsg("esc"))
	if !handled {
		t.Fatal("过滤态的 Esc 应当被 picker 处理（清空过滤）")
	}
	if got := p.list.FilterState(); got != list.Unfiltered {
		t.Fatalf("过滤态 Esc 后应当回到未过滤态，得到 %v", got)
	}
	if got := p.list.FilterValue(); got != "" {
		t.Fatalf("过滤串应当被清空，得到 %q", got)
	}

	p, handled = apply(p, keyMsg("esc"))
	if handled {
		t.Fatal("未过滤时的 Esc 应当交回外层模型（放弃向导 / 退出列表）")
	}
}

func TestPickerTabIsSwallowed(t *testing.T) {
	// AcceptWhileFiltering 默认也绑着 tab/shift+tab；一旦进了 FilterApplied 态，
	// Esc 就变成「只清过滤」而不是退出，外层的两段式 Esc 就不成立了。
	for _, key := range []string{"tab", "shift+tab"} {
		p := newTestPicker("一", "二")
		p, _ = apply(p, keyMsg("一"))

		p, handled := apply(p, keyMsg(key))
		if !handled {
			t.Fatalf("%s 应当被 picker 吞掉", key)
		}
		if got := p.list.FilterState(); got == list.FilterApplied {
			t.Fatalf("%s 不该把列表推进 FilterApplied 态", key)
		}
	}
}

func TestPickerPinnedItemSurvivesFiltering(t *testing.T) {
	items := stubItems("未分类", "工作", "+ 新建分类…")
	p := newPicker(items, 80, 24, list.NewDefaultDelegate(), pinnedLastFilter(2))
	p.list.FilterInput.Cursor.SetMode(cursor.CursorStatic)

	p = applyCmd(t, p, keyMsg("工"))
	visible := p.list.VisibleItems()
	if len(visible) != 2 {
		t.Fatalf("应当只剩命中项与常驻占位项，得到 %d 项", len(visible))
	}
	if got := visible[0].(stubItem).title; got != "工作" {
		t.Fatalf("第一项应当是「工作」，得到 %q", got)
	}
	if got := visible[1].(stubItem).title; got != "+ 新建分类…" {
		t.Fatalf("占位项必须留在最后一行，得到 %q", got)
	}

	// 谁都匹配不上时，占位项仍然可见（否则用户没法在零结果时就地新建分类）
	p2 := newPicker(items, 80, 24, list.NewDefaultDelegate(), pinnedLastFilter(2))
	p2.list.FilterInput.Cursor.SetMode(cursor.CursorStatic)
	p2 = applyCmd(t, p2, keyMsg("zzz"))
	visible = p2.list.VisibleItems()
	if len(visible) != 1 || visible[0].(stubItem).title != "+ 新建分类…" {
		t.Fatalf("零命中时应当只剩占位项，得到 %+v", visible)
	}

	// 占位项不在末尾时，命中项的下标必须映射回原始下标（DefaultFilter 是在
	// 去掉占位项后的切片上给下标的，不映射就会指错项）
	mid := stubItems("甲", "+ 新建分类…", "丙")
	p3 := newPicker(mid, 80, 24, list.NewDefaultDelegate(), pinnedLastFilter(1))
	p3.list.FilterInput.Cursor.SetMode(cursor.CursorStatic)
	p3 = applyCmd(t, p3, keyMsg("丙"))
	visible = p3.list.VisibleItems()
	if len(visible) != 2 {
		t.Fatalf("应当只剩命中项与占位项，得到 %d 项", len(visible))
	}
	if got := visible[0].(stubItem).title; got != "丙" {
		t.Fatalf("命中项应当是「丙」（原始下标 2），得到 %q", got)
	}
	if got := visible[1].(stubItem).title; got != "+ 新建分类…" {
		t.Fatalf("占位项必须留在最后一行，得到 %q", got)
	}
}

func TestPickerFilterPromptIsChinese(t *testing.T) {
	// bubbles 默认给过滤框的提示是英文「Filter: 」，它会直接出现在 note add 与 note list 的界面上
	p := newTestPicker("一")
	if strings.Contains(p.list.FilterInput.Prompt, "Filter") {
		t.Fatalf("过滤框提示应当是中文，得到 %q", p.list.FilterInput.Prompt)
	}
}

func TestPickerQuitKeysAreDisabled(t *testing.T) {
	p := newTestPicker("一", "二")

	if keys := p.list.KeyMap.Quit.Keys(); len(keys) != 0 {
		t.Fatalf("list 自己的退出键必须禁用，得到 %v", keys)
	}
	if keys := p.list.KeyMap.ForceQuit.Keys(); len(keys) != 0 {
		t.Fatalf("list 自己的强制退出键必须禁用，得到 %v", keys)
	}

	// 未过滤时敲 q 是过滤字符，不是退出
	p, handled := apply(p, keyMsg("q"))
	if !handled {
		t.Fatal("q 应当被 picker 当可见字符处理")
	}
	if got := p.list.FilterState(); got != list.Filtering {
		t.Fatalf("q 应当进过滤态，得到 %v", got)
	}

	// ctrl+c 必须交回外层模型：picker 吃掉它的话外层永远收不到，退出就没了
	next := newTestPicker("一", "二")
	_, handled, cmd := next.update(keyMsg("ctrl+c"))
	if handled {
		t.Fatal("ctrl+c 应当交回外层模型（退出由它决定）")
	}
	if cmd != nil {
		t.Fatal("ctrl+c 不该由 picker 产生命令")
	}
}

func TestHighlightMatchesMarksOnlyHitRunes(t *testing.T) {
	// lipgloss 在非终端环境里默认不输出颜色，不强制 profile 的话这条用例
	// 删掉染色逻辑也照样绿（即它根本没钉住高亮）。
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(previous)

	if plain := highlightMatches("工作笔记", nil); plain != "工作笔记" {
		t.Fatalf("没有命中时应当原样返回，得到 %q", plain)
	}

	got := highlightMatches("工作笔记", []int{0, 3})
	if got == "工作笔记" {
		t.Fatal("命中字符应当被染色")
	}
	if stripped := ansi.Strip(got); stripped != "工作笔记" {
		t.Fatalf("染色不该改变文字内容，得到 %q", stripped)
	}
	if n := strings.Count(got, "38;2;"); n != 2 {
		t.Fatalf("应当恰好染两个命中字符（工、记），得到 %d 个颜色序列：%q", n, got)
	}
}
