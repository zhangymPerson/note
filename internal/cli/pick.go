package cli

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// picker 是 note list 与 note add 分类步共用的选择器：包一层 bubbles/list。
//
// bubbles/list 的默认交互是 vim 式模态的：浏览态敲字不会进过滤态（要按 /），
// 而过滤态里除了 Esc/Enter 之外所有按键都被转发给过滤输入框，方向键因此不会移动光标。
// gum filter 的习惯是「一直能打字、方向键一直能选」，所以这里接管四条键位规则：
//
//  1. 可见字符即进过滤态（空格在 bubbletea 里是 KeySpace，要单独认）；
//  2. 过滤态里方向键自己调 CursorUp/CursorDown；
//  3. Enter 立即确认（不进 FilterApplied 态，否则要按两次）；
//  4. Esc 两段式：过滤态先清空过滤，未过滤时交回外层模型。
//
// 另外还要重绑默认绑定里会抢字母的导航键，并禁掉 list 自己的退出键（退出归外层）。
type picker struct {
	list list.Model
}

// newPicker 造一个选择器。filter 为 nil 时用 bubbles 的 fuzzy 默认实现。
func newPicker(items []list.Item, width, height int, delegate list.ItemDelegate, filter list.FilterFunc) picker {
	model := list.New(items, delegate, width, height)
	model.SetShowTitle(false)
	model.SetShowStatusBar(false)
	model.SetShowHelp(false)
	model.SetShowPagination(false)
	model.SetFilteringEnabled(true)
	model.SetShowFilter(true)
	if filter != nil {
		model.Filter = filter
	}
	rebindPickerKeys(&model)
	return picker{list: model}
}

// rebindPickerKeys 去掉默认绑定里会抢字母的导航键（j/k/h/l/b/u/f/d/g/G），
// 否则那些字母没法当过滤字符输入。
// bubbles 的 updateKeybindings 只改绑定的 enabled、不覆盖键位，所以重绑一次就够。
func rebindPickerKeys(model *list.Model) {
	model.KeyMap.CursorUp = key.NewBinding(key.WithKeys("up", "ctrl+k"))
	model.KeyMap.CursorDown = key.NewBinding(key.WithKeys("down", "ctrl+j"))
	model.KeyMap.PrevPage = key.NewBinding(key.WithKeys("pgup"))
	model.KeyMap.NextPage = key.NewBinding(key.WithKeys("pgdown"))
	model.KeyMap.GoToStart = key.NewBinding(key.WithKeys("home"))
	model.KeyMap.GoToEnd = key.NewBinding(key.WithKeys("end"))
	// 空键位绑定永远匹配不上：退出这件事由外层模型决定
	model.KeyMap.Quit = key.NewBinding()
	model.KeyMap.ForceQuit = key.NewBinding()
}

// pickerFilterStartKey 是「进过滤态」那条按键：借用 list 自己的 / 处理。
var pickerFilterStartKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}

// update 处理一条消息；handled 为 false 表示这条消息要交回外层模型
// （只有「Enter 确认」与「未过滤时的 Esc」两种）。
func (p picker) update(msg tea.Msg) (picker, bool, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "tab", "shift+tab":
			// AcceptWhileFiltering 默认也绑着它们；一旦进了 FilterApplied 态，
			// Esc 就变成「只清过滤」而不是退出，规则 4 的不变式就破了。
			return p, true, nil
		case "enter":
			return p, false, nil
		case "esc":
			if p.list.FilterState() != list.Filtering {
				return p, false, nil
			}
		case "ctrl+c":
			// 退出是外层模型的职责：picker 把它吃掉的话外层永远收不到。
			return p, false, nil
		case "/":
			if p.list.FilterState() != list.Filtering {
				// 交回 list 自己的 / ：它进过滤态时不会把斜杠写进过滤串
				return p.forward(keyMsg)
			}
		case "up", "ctrl+k":
			if p.list.FilterState() == list.Filtering {
				p.list.CursorUp()
				return p, true, nil
			}
		case "down", "ctrl+j":
			if p.list.FilterState() == list.Filtering {
				p.list.CursorDown()
				return p, true, nil
			}
		default:
			if p.list.FilterState() != list.Filtering && isFilterRune(keyMsg) {
				enterCmd := p.enterFiltering()
				next, _, cmd := p.forward(keyMsg)
				return next, true, tea.Batch(enterCmd, cmd)
			}
		}
	}
	return p.forward(msg)
}

// forward 把消息交给内层 list。
func (p picker) forward(msg tea.Msg) (picker, bool, tea.Cmd) {	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, true, cmd
}

// enterFiltering 借 list 自己的 / 进过滤态。不能直接调 SetFilterState：
// 它不会像 / 那样把「空过滤 = 全部项」填进 filteredItems，
// 结果就是过滤态里列表是空的、方向键点了也不动。
func (p *picker) enterFiltering() tea.Cmd {
	var cmd tea.Cmd
	p.list, cmd = p.list.Update(pickerFilterStartKey)
	return cmd
}

// isFilterRune 判断这条按键是不是可打印字符。单独的空格在 bubbletea 里
// 投的是 KeySpace 而不是 KeyRunes，漏了它空格就会被静默吃掉。
func isFilterRune(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace
}

// selected 返回当前高亮项；没有可选项时为 nil。
func (p picker) selected() list.Item {
	return p.list.SelectedItem()
}

func (p picker) view() string {
	return p.list.View()
}

// pinnedLastFilter 让第 pinnedIndex 项不受过滤影响、永远钉在最后一行。
// note add 的「+ 新建分类…」靠它保证任何过滤串下都可见（旧设计文档的硬要求）。
func pinnedLastFilter(pinnedIndex int) list.FilterFunc {
	return func(term string, targets []string) []list.Rank {
		others := make([]string, 0, len(targets))
		indexes := make([]int, 0, len(targets))
		for i, target := range targets {
			if i == pinnedIndex {
				continue
			}
			others = append(others, target)
			indexes = append(indexes, i)
		}

		ranks := list.DefaultFilter(term, others)
		out := make([]list.Rank, 0, len(ranks)+1)
		for _, rank := range ranks {
			// DefaultFilter 给的是 others 里的下标，得映射回原始下标
			rank.Index = indexes[rank.Index]
			out = append(out, rank)
		}
		return append(out, list.Rank{Index: pinnedIndex})
	}
}

// highlightMatches 把命中下标处的字符染成强调色，其余原样。
// bubbles 的 fuzzy 过滤给的正是这样一组下标，比子串高亮更贴近实际命中。
func highlightMatches(name string, matched []int) string {
	if len(matched) == 0 {
		return name
	}
	hits := make(map[int]bool, len(matched))
	for _, index := range matched {
		hits[index] = true
	}

	var b strings.Builder
	for i, r := range []rune(name) {
		if hits[i] {
			b.WriteString(styleMatch.Render(string(r)))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
