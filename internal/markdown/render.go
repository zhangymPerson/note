// Package markdown 把 Markdown 渲染成终端文本。
//
// 全项目只有这里 import glamour（glow 的渲染内核；glow 本体是 package main，
// 无法当库引用）。note show 与 note list 的预览都从这里出，样式因此只有一处。
package markdown

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
)

// Options 是渲染参数。Width <= 0 时不调用 glamour 的 WithWordWrap（沿用它的
// 默认 80，避免 WordWrap=0 触发 glamour 内部宽度为 0 的路径）；Style 空串等价 "auto"。
type Options struct {
	Width int
	Style string
}

// Render 把 Markdown 渲染成终端文本；Style 为 "notty" 时输出纯文本（无 ANSI）。
// 风格名直接交给 glamour，未知风格以 error 形式返回而不是静默回退。
// 返回值不带结尾换行，怎么打印由调用方决定。
func Render(src string, opts Options) (string, error) {
	style := opts.Style
	if style == "" {
		style = "auto"
	}

	options := []glamour.TermRendererOption{glamour.WithStandardStyle(style)}
	if opts.Width > 0 {
		options = append(options, glamour.WithWordWrap(opts.Width))
	}

	renderer, err := glamour.NewTermRenderer(options...)
	if err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	out, err := renderer.Render(src)
	if err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return strings.TrimRight(out, "\n"), nil
}
