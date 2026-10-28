package cli

import (
	"strings"

	"note/internal/config"
)

// Usage 返回总帮助文本；本机默认配置文件路径动态带出，解析失败时省略该行。
func Usage() string {
	var b strings.Builder
	b.WriteString("note — 本地单用户笔记应用\n\n")
	b.WriteString("用法:\n")
	b.WriteString("  note [全局参数] <命令> [命令参数]\n\n")
	b.WriteString("命令:\n")
	b.WriteString("  web      启动 Web 服务（唯一会启动前端页面的命令）\n")
	b.WriteString("  info     输出软件与运行信息\n")
	b.WriteString("  help     显示帮助；help <命令> 显示该命令的参数\n\n")
	b.WriteString("全局参数:\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  -h, --help             显示帮助\n\n")
	b.WriteString("不带命令运行 note 进入交互菜单。\n")
	b.WriteString("配置优先级: 命令行参数 > 配置文件 > 内置默认值。\n\n")
	b.WriteString("示例:\n")
	b.WriteString("  note web                    启动 Web 服务\n")
	b.WriteString("  note web -p 9000            指定端口启动\n")
	b.WriteString("  note web --remote --addr 0.0.0.0:8080    允许局域网访问\n")
	b.WriteString("  note info                   查看软件与运行信息\n\n")
	b.WriteString("配置文件默认位置（与其他常见开源项目一致，跟随用户配置目录）:\n")
	b.WriteString("  Windows  %AppData%\\note\\note.yaml\n")
	b.WriteString("  macOS    ~/Library/Application Support/note/note.yaml\n")
	b.WriteString("  Linux    ~/.config/note/note.yaml\n")
	if defConfig, err := config.DefaultConfigFile(); err == nil {
		b.WriteString("  本机默认: " + defConfig + "\n")
	}
	b.WriteString("\n数据库默认位置（跟随用户数据目录）:\n")
	b.WriteString("  Windows  %AppData%\\note\\notes.db\n")
	b.WriteString("  macOS    ~/Library/Application Support/note/notes.db\n")
	b.WriteString("  Linux    ~/.local/share/note/notes.db\n")
	if defDB, err := config.DefaultDBFile(); err == nil {
		b.WriteString("  本机默认: " + defDB + "\n")
	}
	return b.String()
}

// UsageFor 返回指定主题（web/info）的帮助文本；未知主题回退为总帮助。
func UsageFor(topic string) string {
	switch strings.TrimSpace(topic) {
	case "web":
		return webUsage()
	case "info":
		return infoUsage()
	default:
		return Usage()
	}
}

func webUsage() string {
	var b strings.Builder
	b.WriteString("用法: note web [参数]\n\n")
	b.WriteString("启动 Web 服务并内嵌前端页面；Ctrl+C 优雅退出。\n\n")
	b.WriteString("参数:\n")
	b.WriteString("  -c, --config <路径>        配置文件路径\n")
	b.WriteString("  --addr <host:port>         监听地址（默认 127.0.0.1:8080）\n")
	b.WriteString("  -p, --port <端口>          只覆盖监听端口，host 沿用 --addr 或配置文件\n")
	b.WriteString("  --db <路径>                SQLite 数据库路径\n")
	b.WriteString("  --remote, --allow-remote   允许绑定非回环地址（有安全风险，应置于 TLS 与认证之后）\n")
	b.WriteString("  -h, --help                 显示本帮助\n\n")
	b.WriteString("命令行参数优先于配置文件；未提供的项沿用配置文件或内置默认值。\n")
	return b.String()
}

func infoUsage() string {
	var b strings.Builder
	b.WriteString("用法: note info [参数]\n\n")
	b.WriteString("输出软件版本、构建信息、运行环境与生效配置；\n")
	b.WriteString("不输出任何密文、盐、口令或令牌。\n\n")
	b.WriteString("参数:\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  -h, --help             显示本帮助\n")
	return b.String()
}
