package cli

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeQuickBackend 是 QuickAddDeps 的假实现：记录调用，按需注入错误。
type fakeQuickBackend struct {
	quickNoteErr error
	createErr    error

	quickNoteCalls int
	created        []struct{ content, title string }
	quickResult    QuickNoteResult
}

func (b *fakeQuickBackend) deps() QuickAddDeps {
	return QuickAddDeps{
		CreateQuick: func(_ context.Context, content, title string) (NoteRef, error) {
			if b.createErr != nil {
				return NoteRef{}, b.createErr
			}
			b.created = append(b.created, struct{ content, title string }{content, title})
			return NoteRef{ID: "q-" + strconv.Itoa(len(b.created)), Title: title, CategoryName: "速记"}, nil
		},
		QuickNote: func(_ context.Context, content string) (QuickNoteResult, error) {
			b.quickNoteCalls++
			if b.quickNoteErr != nil {
				return QuickNoteResult{}, b.quickNoteErr
			}
			return b.quickResult, nil
		},
		AIModel: "m1",
		AIHost:  "api.example.com",
	}
}

func newTestQuickAddModel(b *fakeQuickBackend) quickAddModel {
	m := newQuickAddModel(b.deps())
	m.width = 100
	m.view.Width, m.view.Height = m.width-4, 20
	return m
}

// finishQuick 执行 Init 返回的 AI 命令并把它喂回模型（模拟整理完成）。
func finishQuick(m quickAddModel, cmd tea.Cmd) quickAddModel {
	if cmd == nil {
		return m
	}
	msg := cmd()
	updated, _ := m.Update(msg)
	next, ok := updated.(quickAddModel)
	if !ok {
		return m
	}
	return next
}

// pressQuick 喂一个按键并把命令原样返回。
func pressQuick(m quickAddModel, name string) (quickAddModel, tea.Cmd) {
	updated, cmd := m.Update(keyMsgFor(name))
	next, ok := updated.(quickAddModel)
	if !ok {
		return m, cmd
	}
	return next, cmd
}

func TestRunAddQuickWithoutAI(t *testing.T) {
	var gotTitle string
	deps := QuickAddDeps{
		CreateQuick: func(_ context.Context, content, title string) (NoteRef, error) {
			gotTitle = title
			return NoteRef{ID: "id1", Title: "速记-x", CategoryName: "速记"}, nil
		},
	}
	ref, saved, err := RunAddQuick(context.Background(), deps, "买牛奶", false)
	if err != nil || !saved {
		t.Fatalf("saved=%v err=%v", saved, err)
	}
	if gotTitle != "" {
		t.Fatalf("非 AI 速记标题应为空（服务端自动生成），得到 %q", gotTitle)
	}
	if ref.ID != "id1" {
		t.Fatalf("ref = %+v", ref)
	}
}

func TestRunAddQuickWithoutAIEmptyContent(t *testing.T) {
	deps := QuickAddDeps{
		CreateQuick: func(_ context.Context, content, title string) (NoteRef, error) {
			return NoteRef{}, nil
		},
	}
	if _, _, err := RunAddQuick(context.Background(), deps, "   ", false); err == nil {
		t.Fatal("空正文应报错")
	}
	if _, _, err := RunAddQuick(context.Background(), deps, strings.Repeat("😀", 2001), false); err == nil {
		t.Fatal("超长正文应报错")
	}
}

func TestQuickAddAIWithoutConfigShowsHint(t *testing.T) {
	b := &fakeQuickBackend{}
	deps := b.deps()
	deps.QuickNote = nil
	deps.AIModel, deps.AIHost = "", ""
	m := newTestQuickAddModel(b)
	m.deps = deps

	if cmd := m.Init(); cmd != nil {
		t.Fatal("未配置时 Init 不应发起请求")
	}
	if !strings.Contains(m.View(), "还没配置 AI") {
		t.Fatalf("未配置时 View 应提示补齐，得到:\n%s", m.View())
	}
	// Esc 放弃
	m, cmd := pressQuick(m, "esc")
	if cmd == nil {
		t.Fatal("Esc 应产生退出命令")
	}
	if !m.abandoned {
		t.Fatal("Esc 后应标记 abandoned")
	}
}

func TestQuickAddAIAdoptsPreview(t *testing.T) {
	b := &fakeQuickBackend{}
	b.quickResult = QuickNoteResult{Title: "待办", Content: "- 买牛奶", Model: "m1"}
	m := newTestQuickAddModel(b)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("配置齐了 Init 应发起整理")
	}
	// 整理中必须先披露：用哪个模型、正文发到哪台主机
	if !strings.Contains(m.View(), "正在用 m1 整理") || !strings.Contains(m.View(), "api.example.com") {
		t.Fatalf("整理中应披露模型与主机:\n%s", m.View())
	}
	m = finishQuick(m, cmd)
	if m.step != stepQuickPreview {
		t.Fatalf("step = %d, want preview", m.step)
	}
	if m.title != "待办" || m.resultContent != "- 买牛奶" || m.model != "m1" {
		t.Fatalf("m = %+v", m)
	}
	if !strings.Contains(m.View(), "AI 速记预览") || !strings.Contains(m.View(), "待办") || !strings.Contains(m.View(), "买牛奶") {
		t.Fatalf("预览屏缺关键信息:\n%s", m.View())
	}

	// Enter 保存：用预览锁定的内容与标题
	updated, cmd := m.Update(keyMsgFor("enter"))
	if cmd == nil {
		t.Fatal("保存应产生退出命令")
	}
	m = updated.(quickAddModel)
	if !m.saved {
		t.Fatal("saved 应为 true")
	}
	if len(b.created) != 1 || b.created[0].content != "- 买牛奶" || b.created[0].title != "待办" {
		t.Fatalf("CreateQuick 调用不符: %+v", b.created)
	}
}

func TestQuickAddAIEscAbandons(t *testing.T) {
	b := &fakeQuickBackend{}
	b.quickResult = QuickNoteResult{Title: "待办", Content: "- 买牛奶", Model: "m1"}
	m := newTestQuickAddModel(b)
	m = finishQuick(m, m.Init())

	m, cmd := pressQuick(m, "esc")
	if cmd == nil {
		t.Fatal("Esc 应产生退出命令")
	}
	if m.saved {
		t.Fatal("Esc 不应保存")
	}
	if len(b.created) != 0 {
		t.Fatalf("CreateQuick 不应被调用: %+v", b.created)
	}
}

func TestQuickAddAIErrorShowsAndRetries(t *testing.T) {
	b := &fakeQuickBackend{}
	b.quickNoteErr = errors.New("上游 500")
	m := newTestQuickAddModel(b)

	m = finishQuick(m, m.Init())
	if !strings.Contains(m.View(), "AI 整理失败") || !strings.Contains(m.View(), "上游 500") {
		t.Fatalf("失败原因应回显:\n%s", m.View())
	}
	if m.step != stepQuickWorking {
		t.Fatalf("失败后应留在工作步，step = %d", m.step)
	}

	// Ctrl+O 重试：这次成功
	b.quickNoteErr = nil
	m = finishQuick(m, m.Init())
	if m.step != stepQuickPreview {
		t.Fatalf("重试成功后应进预览，step = %d", m.step)
	}
}
