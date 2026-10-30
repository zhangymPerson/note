package cli

import (
	"fmt"
	"strings"
)

// ConfigItem 是 note config list 的一行。Value 是拿来展示的字符串——
// 密钥由调用方先过 config.MaskSecret，这里不再判断哪个键是敏感键；
// 空串表示该项未设置。
type ConfigItem struct {
	Key   string
	Value string
}

// configValueWidth 是单行值的展示宽度：提示词可能很长，列表里只看一眼，
// 想确认全文用 note config list 之外的途径（配置文件本身）。
const configValueWidth = 60

// FormatConfigList 渲染 note config list：先配置文件路径，再逐项列值。
// found 为 false 表示文件还没创建（首次 note config set 会自动建）。
func FormatConfigList(path string, found bool, items []ConfigItem) string {
	var b strings.Builder
	b.WriteString("配置文件  " + path + "\n")
	if !found {
		b.WriteString("          （还没创建；note config set 会自动建，权限 0600）\n")
	}
	b.WriteString("\n")

	width := 0
	for _, item := range items {
		if n := len(item.Key); n > width {
			width = n
		}
	}
	for _, item := range items {
		b.WriteString("  " + item.Key + strings.Repeat(" ", width-len(item.Key)+2) + displayConfigValue(item.Value) + "\n")
	}

	b.WriteString("\n用 note config set <键>=<值> 写入；值留空表示清空该项。\n")
	b.WriteString("ai.prompt 里用 {{content}} 代表笔记正文；留空表示使用内置默认提示词。\n")
	b.WriteString("上面四项供 OpenAI 兼容接口使用，密钥在命令行与网页里都只显示掩码。\n")
	return b.String()
}

// FormatConfigSet 渲染 note config set 的成功回显；密钥同样只回显掩码，
// 免得刚写进去的明文又被终端记进 scrollback。
func FormatConfigSet(path string, item ConfigItem) string {
	return fmt.Sprintf("已写入 %s\n  %s = %s\n", path, item.Key, displayConfigValue(item.Value))
}

// displayConfigValue 把值压成单行：提示词里的换行折成空格，超宽截断。
func displayConfigValue(value string) string {
	collapsed := strings.Join(strings.Fields(value), " ")
	if collapsed == "" {
		return "（未设置）"
	}
	return truncateDisplay(collapsed, configValueWidth)
}

// configUsage 是 note config 的主题帮助。
func configUsage() string {
	return `用法: note config list|set [参数]

读写 note.yaml 里的 AI 四项（OpenAI 兼容接口）：

  ai.base_url  接口地址，必须是 http(s) 绝对地址，例如 https://api.openai.com/v1
  ai.api_key   API 密钥；命令行与网页里都只显示掩码
  ai.models    逗号分隔的模型列表，第一个为默认模型；最多 10 个
  ai.prompt    提示词模板，最多 2000 字，可以多行；
               用 {{content}} 代表用户的笔记正文，留空表示使用内置默认提示词

子命令:
  list              输出配置文件路径与四项当前值（密钥掩码）
  set <键>=<值>     写入一项；值留空表示清空该项

参数:
  -c, --config <路径>    配置文件路径（不给则用用户配置目录下的 note.yaml）
  -h, --help             显示本帮助

写入是原子的（同目录临时文件 + rename），文件权限 0600、目录 0700；
写回后再按严格规则解析一遍，未知键或非法值会拒绝写入。
命令行参数优先于配置文件；远程访问相关设置请直接改配置文件或 note web 参数。

退出码:
  0  正常
  1  读取或写入失败
  2  参数错误（如未知键、缺 <键>=<值>）

示例:
  note config list
  note config set ai.base_url=https://api.openai.com/v1
  note config set ai.api_key=sk-…
  note config set ai.models=gpt-4o-mini,deepseek-chat
  note config set "ai.prompt=把下面这段整理成 Markdown 笔记：{{content}}"
  note config set ai.prompt=             恢复默认提示词
  note config set ai.api_key=            清空密钥
`
}
