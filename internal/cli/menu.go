package cli

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"
)

// MenuAction 是交互菜单的选择结果。
type MenuAction int

const (
	// MenuQuit 表示退出（零值：q / Esc / Ctrl+C / 选「退出」都落在这里）。
	MenuQuit MenuAction = iota
	// MenuWeb 启动 Web 服务。
	MenuWeb
	// MenuInfo 输出系统信息。
	MenuInfo
	// MenuFallback 表示菜单无法运行（stdin 不是终端等），调用方应退化为打印帮助。
	MenuFallback
)

type menuItem struct {
	label  string
	action MenuAction
}

var defaultMenuItems = []menuItem{
	{label: "启动 Web 服务（后台）", action: MenuWeb},
	{label: "系统信息", action: MenuInfo},
	{label: "退出", action: MenuQuit},
}

// menuModel 是 bubbletea 的 Elm 架构模型：只维护光标与选择结果，
// 不持有任何服务资源——启动 Web 服务是菜单退出后由 cmd/note 接手的事。
type menuModel struct {
	items  []menuItem
	cursor int
	chosen MenuAction
}

func (m menuModel) Init() tea.Cmd { return nil }

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			m.chosen = m.items[m.cursor].action
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	var b strings.Builder
	b.WriteString("note — 本地单用户笔记应用\n\n")
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&b, "%s%s\n", cursor, item.label)
	}
	b.WriteString("\n↑/↓ 或 j/k 选择，Enter 确认，q 退出\n")
	return b.String()
}

// RunMenu 启动交互菜单，返回用户选择的动作。
// stdin 不是终端（脚本/管道环境）或 TUI 运行失败时返回 MenuFallback，
// 由调用方决定退化行为（打印帮助），绝不在无终端环境里挂住进程。
func RunMenu() (MenuAction, error) {
	if !isTerminal(os.Stdin) {
		return MenuFallback, nil
	}
	program := tea.NewProgram(menuModel{items: defaultMenuItems})
	final, err := program.Run()
	if err != nil {
		return MenuFallback, err
	}
	if model, ok := final.(menuModel); ok {
		return model.chosen, nil
	}
	return MenuQuit, nil
}

// isTerminal 判断文件描述符是否连接着终端。
func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}
