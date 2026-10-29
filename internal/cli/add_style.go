package cli

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 配色跟随终端亮/暗主题（AdaptiveColor），观感对齐 gum：洋红游标与提示、灰色辅助信息、红色错误。
var (
	accentColor = lipgloss.AdaptiveColor{Light: "#D1006F", Dark: "#FF5FAF"}
	mutedColor  = lipgloss.AdaptiveColor{Light: "#8A8A8A", Dark: "#6C6C6C"}
	warnColor   = lipgloss.AdaptiveColor{Light: "#C92A2A", Dark: "#FF5F5F"}
)

var (
	styleBrand    = lipgloss.NewStyle().Bold(true)
	styleStep     = lipgloss.NewStyle().Foreground(mutedColor)
	styleCursor   = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	stylePrompt   = lipgloss.NewStyle().Foreground(accentColor)
	styleSelected = lipgloss.NewStyle().Bold(true)
	styleMuted    = lipgloss.NewStyle().Foreground(mutedColor)
	styleWarn     = lipgloss.NewStyle().Foreground(warnColor)
	styleMatch    = lipgloss.NewStyle().Foreground(accentColor).Bold(true)
	// styleBox 是正文框（gum write 那种细边框）：圆角、灰色描边、左右各留一列内边距。
	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor).
			Padding(0, 1)
)

// help 把 [(键, 说明)] 渲染成一行：键洋红、说明压暗、用 · 分隔。
func help(pairs [][2]string) string {
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, stylePrompt.Render(p[0])+" "+styleMuted.Render(p[1]))
	}
	return strings.Join(parts, styleMuted.Render(" · "))
}

// header 渲染顶部标题行：品牌加粗、步骤压暗。
func header(text, step string) string {
	out := styleBrand.Render("note add")
	if text != "" {
		out += styleMuted.Render(" · ") + text
	}
	if step != "" {
		out += styleMuted.Render(" · " + step)
	}
	return out
}

// matchRange 返回 name 中第一处命中 query 的字节区间（不区分大小写），没有命中时 ok=false。
// 空 query 不算命中——否则每行都会被整段高亮。
func matchRange(name, query string) (start, end int, ok bool) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, 0, false
	}
	lowerName, lowerQuery := strings.ToLower(name), strings.ToLower(query)
	idx := strings.Index(lowerName, lowerQuery)
	if idx < 0 {
		return 0, 0, false
	}
	return idx, idx + len(lowerQuery), true
}

// highlight 把命中的子串染成强调色，其余原样——gum filter 的 match 样式。
func highlight(name, query string) string {
	start, end, ok := matchRange(name, query)
	if !ok {
		return name
	}
	return name[:start] + styleMatch.Render(name[start:end]) + name[end:]
}
