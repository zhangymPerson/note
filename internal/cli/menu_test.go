package cli

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newMenuModel() menuModel {
	return menuModel{items: defaultMenuItems}
}

// testKey 把字符串映射成对应的 tea.KeyMsg；菜单只依赖这几个键。
func testKey(s string) tea.KeyMsg {
	switch s {
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// update 是测试辅助：以字符串触发一次按键，返回新模型。
func update(m menuModel, key string) menuModel {
	next, _ := m.Update(testKey(key))
	if model, ok := next.(menuModel); ok {
		return model
	}
	return m
}

func TestMenuViewListsAllItems(t *testing.T) {
	view := newMenuModel().View()
	for _, label := range []string{"启动 Web 服务（后台）", "系统信息", "退出"} {
		if !strings.Contains(view, label) {
			t.Fatalf("view 缺少菜单项 %q:\n%s", label, view)
		}
	}
	if !strings.Contains(view, "> 启动 Web 服务（后台）") {
		t.Fatalf("view 未在第一项显示光标:\n%s", view)
	}
}

func TestMenuCursorMoves(t *testing.T) {
	m := update(newMenuModel(), "down")
	if !strings.Contains(m.View(), "> 系统信息") {
		t.Fatalf("down 后光标应在第二项:\n%s", m.View())
	}

	// 顶部再按 up 不越界
	m = update(m, "up")
	if !strings.Contains(m.View(), "> 启动 Web 服务") {
		t.Fatalf("up 后光标应回到第一项:\n%s", m.View())
	}
	m = update(m, "up")
	if !strings.Contains(m.View(), "> 启动 Web 服务") {
		t.Fatalf("顶部 up 不应越界:\n%s", m.View())
	}

	// j/k 与方向键等价
	m = update(newMenuModel(), "j")
	if !strings.Contains(m.View(), "> 系统信息") {
		t.Fatalf("j 应下移光标:\n%s", m.View())
	}
	m = update(m, "k")
	if !strings.Contains(m.View(), "> 启动 Web 服务") {
		t.Fatalf("k 应上移光标:\n%s", m.View())
	}

	// 底部再按 down 不越界
	bottom := newMenuModel()
	bottom = update(bottom, "down")
	bottom = update(bottom, "down")
	if !strings.Contains(bottom.View(), "> 退出") {
		t.Fatalf("两次 down 后光标应在第三项:\n%s", bottom.View())
	}
	bottom = update(bottom, "down")
	if !strings.Contains(bottom.View(), "> 退出") {
		t.Fatalf("底部 down 不应越界:\n%s", bottom.View())
	}
}

func TestMenuEnterSelectsCurrentItem(t *testing.T) {
	m := update(newMenuModel(), "enter")
	if m.chosen != MenuWeb {
		t.Fatalf("第一项回车应选中 MenuWeb, got %v", m.chosen)
	}

	m = update(update(newMenuModel(), "down"), "enter")
	if m.chosen != MenuInfo {
		t.Fatalf("第二项回车应选中 MenuInfo, got %v", m.chosen)
	}

	m = update(update(update(newMenuModel(), "down"), "down"), "enter")
	if m.chosen != MenuQuit {
		t.Fatalf("第三项回车应选中 MenuQuit, got %v", m.chosen)
	}
}

func TestMenuQuitKeysKeepZeroAction(t *testing.T) {
	for _, key := range []string{"q", "esc"} {
		m := update(newMenuModel(), key)
		if m.chosen != MenuQuit {
			t.Fatalf("按 %s 后动作应为 MenuQuit, got %v", key, m.chosen)
		}
	}
}

func TestRunMenuFallsBackWithoutTerminal(t *testing.T) {
	if isTerminal(os.Stdin) {
		t.Skip("stdin 是终端（交互运行），跳过非终端分支")
	}
	action, err := RunMenu()
	if err != nil {
		t.Fatalf("run menu: %v", err)
	}
	if action != MenuFallback {
		t.Fatalf("非终端环境应返回 MenuFallback, got %v", action)
	}
}
