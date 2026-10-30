package cli

import (
	"context"
	"errors"
	"strings"

	"note/internal/markdown"
)

// NoteDetail 是 note show 的输入：一条笔记的元信息与明文正文。
// 加密笔记的 Content 为空——CLI 没有主密码，解不开密文。
type NoteDetail struct {
	ID           string
	Title        string
	CategoryName string
	Encrypted    bool
	Trashed      bool
	UpdatedAt    string
	Content      string
}

// ErrNoteEncrypted 表示这条笔记是加密的：CLI 只能给出元信息与提示。
// 它不是故障，调用方据此把退出码映射成 4。
var ErrNoteEncrypted = errors.New("note is encrypted")

// ShowDeps 让 cli 不必依赖持久层。
type ShowDeps struct {
	Note func(ctx context.Context, id string) (NoteDetail, error)
}

// ShowResult 是 note show 的纯计算结果，由 cmd/note 打印。
type ShowResult struct {
	// Header 是真终端下的三行元信息；非 TTY 下恒为空。
	Header string
	// Body 是渲染后的正文；--raw 时为原始 Markdown；加密笔记为提示行。
	Body string
	// Source 是库里的原始 Markdown（加密笔记为空）。「复制原文」复制的是它，
	// 而不是渲染后的终端文本：带着色与折行的渲染结果粘到别处就不可用了。
	Source string
}

// encryptedHint 是加密笔记在正文位置打印的提示：说清为什么看不了、去哪儿看。
const encryptedHint = "这条笔记是加密的，CLI 没有主密码，无法解密；请在 note web 里解锁查看。"

// resolveStyle 决定实际使用的 glamour 风格：显式指定优先；
// 否则真终端用 auto（按终端背景在 dark/light 间选），非终端用 notty（纯文本、无 ANSI）。
func resolveStyle(style string, tty bool) string {
	if style != "" {
		return style
	}
	if tty {
		return "auto"
	}
	return "notty"
}

// resolveWidth 决定渲染宽度：显式宽度优先（参数层已保证 20..1000）；
// 否则取终端宽度并封顶 120，取不到终端宽度时用 80。
func resolveWidth(width, termWidth int) int {
	if width > 0 {
		return width
	}
	if termWidth <= 0 {
		return 80
	}
	return max(20, min(termWidth, 120))
}

// BuildShow 读一条笔记并算出要打印的内容；tty 与终端宽度由调用方探测后传入。
//
// 加密笔记不是失败路径：Header 与提示行照常填好，同时返回 ErrNoteEncrypted，
// 调用方先打印 ShowResult 再把该错误映射成退出码 4。
func BuildShow(ctx context.Context, deps ShowDeps, opts ShowOptions, tty bool, termWidth int) (ShowResult, error) {
	detail, err := deps.Note(ctx, opts.ID)
	if err != nil {
		return ShowResult{}, err
	}

	width := resolveWidth(opts.Width, termWidth)
	result := ShowResult{Source: detail.Content}
	if tty {
		result.Header = showHeader(detail, width)
	}

	if detail.Encrypted {
		result.Body = encryptedHint
		return result, ErrNoteEncrypted
	}

	if opts.Raw {
		// --raw 只给原始 Markdown：不渲染、也不加头部，便于直接喂给别的渲染器
		result.Header = ""
		result.Body = detail.Content
		return result, nil
	}

	body, err := markdown.Render(detail.Content, markdown.Options{
		Width: width,
		Style: resolveStyle(opts.Style, tty),
	})
	if err != nil {
		return ShowResult{}, err
	}
	result.Body = body
	return result, nil
}

// showHeader 拼真终端下的三行头部：标题、元信息、分隔线。
// 标题与正文在库里是两个字段，不打印头部就永远看不到标题。
func showHeader(detail NoteDetail, width int) string {
	category := detail.CategoryName
	if category == "" {
		category = "未分类"
	}
	parts := []string{category, localStamp(detail.UpdatedAt), detail.ID}
	if detail.Trashed {
		parts = append(parts, "回收站")
	}

	rule := strings.Repeat("─", min(60, max(20, width)))
	return styleBrand.Render(detail.Title) + "\n" +
		styleMuted.Render(strings.Join(parts, " · ")) + "\n" +
		styleMuted.Render(rule)
}
