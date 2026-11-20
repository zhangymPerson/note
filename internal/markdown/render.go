// Package markdown 把 Markdown 渲染成终端文本。
//
// 全项目只有这里 import glamour（glow 的渲染内核；glow 本体是 package main，
// 无法当库引用）。note show 与 note list 的预览都从这里出，样式因此只有一处。
package markdown

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// autoStyle 缓存「auto」风格的一次性解析结果。glamour 的 "auto" 风格会调用
// termenv 向终端发 OSC 11 查询（`\x1b]11;?\x07`）并阻塞读 stdin 等回复——
// 在 bubbletea 的 TUI 循环里调用会与 bubbletea 自己的 stdin 读取器抢字节：
// 终端的回复（`\x1b]11;rgb:…`）会被当作用户输入，轻则显示乱码、误触发按键，
// 重则让程序报错直接退出。因此「auto」只能在进入 raw mode（tea 启动）之前
// 解析一次并永久缓存，之后所有渲染都拿显式的 "dark"/"light"/"notty"，
// 不再碰终端。
var (
	autoStyleOnce sync.Once
	autoStyle     string
)

// AutoStyle 解析一次「auto」风格并返回显式风格名，与 glamour 的 auto 同口径：
// stdout 不是终端（管道/重定向/测试）→ "notty"（纯文本、无 ANSI）；否则按终端
// 背景亮暗给 "dark"/"light"。任何要跑 bubbletea TUI 的入口都应在构造模型时
// （tea 尚未进入 raw mode）先调用一次，避免渲染阶段再去查终端。
func AutoStyle() string {
	autoStyleOnce.Do(func() {
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			autoStyle = "notty"
			return
		}
		if termenv.HasDarkBackground() {
			autoStyle = "dark"
		} else {
			autoStyle = "light"
		}
	})
	return autoStyle
}

// Options 是渲染参数。Width <= 0 时不调用 glamour 的 WithWordWrap（沿用它的
// 默认 80，避免 WordWrap=0 触发 glamour 内部宽度为 0 的路径）；Style 空串等价
// "auto"，都按 AutoStyle 的一次性解析结果渲染，不会在渲染时查询终端。
type Options struct {
	Width int
	Style string
}

// Render 把 Markdown 渲染成终端文本；Style 为 "notty" 时输出纯文本（无 ANSI）。
// 风格名直接交给 glamour，未知风格以 error 形式返回而不是静默回退。
// 返回值不带结尾换行，怎么打印由调用方决定。
func Render(src string, opts Options) (string, error) {
	style := opts.Style
	if style == "" || style == "auto" {
		style = AutoStyle()
	}

	options := []glamour.TermRendererOption{glamour.WithStandardStyle(style)}
	if opts.Width > 0 {
		options = append(options, glamour.WithWordWrap(opts.Width))
	}

	renderer, err := glamour.NewTermRenderer(options...)
	if err != nil {
		return "", fmt.Errorf("创建 Markdown 渲染器失败：%w", err)
	}
	out, err := renderer.Render(src)
	if err != nil {
		return "", fmt.Errorf("渲染 Markdown 失败：%w", err)
	}
	return strings.TrimRight(out, "\n"), nil
}
