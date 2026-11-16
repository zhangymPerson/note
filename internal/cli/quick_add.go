package cli

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"note/internal/markdown"
)

// QuickNoteResult 是「AI 速记」的结果：标题与正文都来自模型，落库前必须先预览。
// 底层调用在 internal/ai（QuickNote），cmd/note 负责适配，cli 不碰网络。
type QuickNoteResult struct {
	Title   string
	Content string
	Model   string
}

// QuickAddDeps 是 note add <正文> 需要的外部能力。
// QuickNote 为 nil 表示 AI 没配置好：界面只提示怎么补配置，绝不静默落库。
type QuickAddDeps struct {
	CreateQuick func(ctx context.Context, content, title string) (NoteRef, error)
	QuickNote   func(ctx context.Context, content string) (QuickNoteResult, error)
	// AIModel / AIHost 只用于界面披露：用哪个模型、正文会发到哪台主机。
	AIModel string
	AIHost  string
}

// RunAddQuick 执行 note add <正文>：ai=false 时直接落速记（标题服务端自动生成）；
// ai=true 时先进 bubbletea 单屏预览（标题只读），Enter 保存 / Esc 放弃。
// 返回 saved=false 表示用户放弃，调用方按既有约定退出 130。
func RunAddQuick(ctx context.Context, deps QuickAddDeps, content string, ai bool) (NoteRef, bool, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return NoteRef{}, false, fmt.Errorf("正文不能为空")
	}
	if n := utf8.RuneCountInString(content); n > maxQuickContentRunes {
		return NoteRef{}, false, fmt.Errorf("正文最多 %d 码点，当前 %d 码点", maxQuickContentRunes, n)
	}

	if !ai {
		ref, err := deps.CreateQuick(ctx, content, "")
		return ref, err == nil, err
	}

	m := newQuickAddModel(deps)
	m.content = content
	program := tea.NewProgram(m)
	final, err := program.Run()
	if err != nil {
		return NoteRef{}, false, err
	}
	model, ok := final.(quickAddModel)
	if !ok {
		return NoteRef{}, false, nil
	}
	if model.saved {
		return model.ref, true, nil
	}
	return NoteRef{}, false, nil // Esc 放弃
}

// quickStep 是 AI 速记单屏的两个阶段：整理中（含未配置提示）与预览确认。
type quickStep int

const (
	stepQuickWorking quickStep = iota
	stepQuickPreview
)

// quickAddModel 是单屏 Elm 模型：Init 发起 AI 整理，完成后切到预览，Enter 才保存。
type quickAddModel struct {
	deps    QuickAddDeps
	content string // 原始速记正文（已 trim）
	width   int

	step   quickStep
	status string // 失败原因 / 保存失败（只在出错时非空）

	title         string // AI 生成的标题（预览时只读）
	resultContent string // AI 整理后的正文
	model         string
	rendered      string
	view          viewport.Model

	ref       NoteRef
	saved     bool
	abandoned bool
}

func newQuickAddModel(deps QuickAddDeps) quickAddModel {
	m := quickAddModel{
		deps:  deps,
		step:  stepQuickWorking,
		width: 80,
	}
	width := m.width - 4
	if width < 20 {
		width = 20
	}
	m.view = viewport.New(width, 20)
	return m
}

// quickNoteFinishedMsg 是 AI 调用的回执：错误只回显摘要，不落到正文。
type quickNoteFinishedMsg struct {
	Title   string
	Content string
	Model   string
	Err     error
}

// startQuickNote 发起一次 AI 整理；未配置时只提示，不发起任何请求。
// 只用于 Ctrl+O 重新整理（Update 会采用返回的模型）；初始整理走 Init。
func (m quickAddModel) startQuickNote() (tea.Model, tea.Cmd) {
	if m.deps.QuickNote == nil {
		m.status = "还没配置 AI：用 note config set ai.base_url=… ai.api_key=… ai.models=…，或在信息页的 AI 配置卡片里填"
		return m, nil
	}

	m.status = ""
	m.title, m.resultContent, m.model = "", "", ""
	content, quickNote := m.content, m.deps.QuickNote
	return m, func() tea.Msg {
		res, err := quickNote(context.Background(), content)
		return quickNoteFinishedMsg{Title: res.Title, Content: res.Content, Model: res.Model, Err: err}
	}
}

// finishQuickNote 落地整理结果：成功渲染进预览屏，失败留在工作步并回显原因。
func (m quickAddModel) finishQuickNote(msg quickNoteFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.status = "AI 整理失败: " + msg.Err.Error()
		return m, nil
	}

	m.title, m.resultContent, m.model = msg.Title, msg.Content, msg.Model
	rendered, err := markdown.Render(msg.Content, markdown.Options{Width: m.view.Width})
	if err != nil {
		// 渲染失败不拦着保存：正文本身就是合法输入，预览退化成原文
		rendered = msg.Content
	}
	m.rendered = rendered
	m.view.SetContent(m.rendered)
	m.view.GotoTop()
	m.step = stepQuickPreview
	m.status = ""
	return m, nil
}

func (m quickAddModel) Init() tea.Cmd {
	if m.deps.QuickNote == nil {
		return nil
	}
	content, quickNote := m.content, m.deps.QuickNote
	return func() tea.Msg {
		res, err := quickNote(context.Background(), content)
		return quickNoteFinishedMsg{Title: res.Title, Content: res.Content, Model: res.Model, Err: err}
	}
}

func (m quickAddModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case quickNoteFinishedMsg:
		return m.finishQuickNote(msg)
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc:
			m.abandoned = true
			return m, tea.Quit
		case tea.KeyCtrlO:
			// 整理中（step 未进预览）不重复发起；未配置提示按 Ctrl+O 只会再提示一次
			if m.step != stepQuickPreview {
				return m, nil
			}
			m.step = stepQuickWorking
			return m.startQuickNote()
		case tea.KeyEnter:
			if m.step != stepQuickPreview {
				return m, nil
			}
			ref, err := m.deps.CreateQuick(context.Background(), m.resultContent, m.title)
			if err != nil {
				m.status = "保存失败: " + err.Error()
				return m, nil
			}
			m.ref, m.saved = ref, true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m quickAddModel) View() string {
	var b strings.Builder
	b.WriteString(header("速记", ""))
	b.WriteString("\n\n")
	if m.step == stepQuickPreview {
		b.WriteString(stylePrompt.Render("AI 速记预览") + styleMuted.Render(" · "+m.model) + "\n\n")
		b.WriteString(styleMuted.Render("标题  ") + styleBrand.Render(m.title) + "\n\n")
		b.WriteString(m.view.View())
		b.WriteString("\n")
		if m.status != "" {
			b.WriteString(styleWarn.Render("! "+m.status) + "\n")
		}
		b.WriteString(help([][2]string{{"Enter", "保存"}, {"Esc", "放弃"}, {"Ctrl+O", "重新整理"}}) + "\n")
		return b.String()
	}

	// 工作步：未配置提示 / 失败原因 / 整理中状态三选一。
	// 「正在整理」由状态推导而不是存字段：Init 是值接收者，改不到模型上。
	switch {
	case m.deps.QuickNote == nil:
		b.WriteString(styleWarn.Render("! 还没配置 AI：用 note config set ai.base_url=… ai.api_key=… ai.models=…，或在信息页的 AI 配置卡片里填") + "\n")
	case m.status != "":
		b.WriteString(styleWarn.Render("! "+m.status) + "\n")
	default:
		b.WriteString(styleMuted.Render(fmt.Sprintf("正在用 %s 整理…（正文会发送到 %s）", m.deps.AIModel, m.deps.AIHost)) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(help([][2]string{{"Esc", "放弃"}}) + "\n")
	return b.String()
}
