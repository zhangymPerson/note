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
	b.WriteString("  web      启动 Web 服务（start/stop/status 管理后台实例，不带动作则前台运行）\n")
	b.WriteString("  info     输出软件与运行信息\n")
	b.WriteString("  add      新建一条笔记（交互向导：选分类 → 标题 → 正文）\n  set      新建或覆盖一条笔记（note set <标题[@分类]> <正文>，默认落「速记」）\n  get      读正文或列清单（get <标题[@分类]> · get @<分类> · get --list）\n  del      把笔记移入回收站（软删除；del @<分类> 需 --yes）\n")
	b.WriteString("  list     列出笔记并筛选（输入即筛选标题/分类/正文；Enter 预览；非终端时输出纯文本）\n")
	b.WriteString("  show     渲染一条笔记并把原文复制到剪贴板（不给 id 时先选后看）\n")
	b.WriteString("  status   输出使用统计（事件构成、累计事件、活跃天数、Top 查询词）\n")
	b.WriteString("  config   读写配置文件里的 AI 四项（list 查看、set <键>=<值> 写入）\n")
	b.WriteString("  help     显示帮助；help <命令> 显示该命令的参数\n\n")
	b.WriteString("全局参数:\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  -h, --help             显示帮助\n\n")
	b.WriteString("不带命令运行 note 等价于 note help（打印本帮助）。\n")
	b.WriteString("配置优先级: 命令行参数 > 配置文件 > 内置默认值。\n\n")
	b.WriteString("示例:\n")
	b.WriteString("  note web                    前台启动 Web 服务（Ctrl+C 退出）\n")
	b.WriteString("  note web start              后台启动，日志写入数据库同目录的 note.log\n")
	b.WriteString("  note web status             查看后台实例运行情况\n")
	b.WriteString("  note web stop               停止后台实例\n")
	b.WriteString("  note web start -p 9000      指定端口后台启动\n")
	b.WriteString("  note web --remote --addr 0.0.0.0:8080    允许局域网访问\n")
	b.WriteString("  note info                   查看软件与运行信息\n")
	b.WriteString("  note set 部署命令 \"make deploy\"     记一条速记（同名覆盖）\n  note get 部署命令           读回速记正文\n  note get @工作              列出「工作」分类下的笔记\n  note get --list             列出所有分类\n  note del 部署命令           移入回收站（软删除）\n  note list                   筛选并预览笔记\n")
	b.WriteString("  note show                   交互选一条，渲染并复制原文\n")
	b.WriteString("  note show 9f2c1a3b-…       按 id 渲染并复制原文\n")
	b.WriteString("  note status                 查看使用统计（与网页概览页同源）\n")
	b.WriteString("  note config list            查看 AI 四项（密钥只显示掩码）\n")
	b.WriteString("  note config set ai.api_key=sk-…    写入密钥（值留空就是清空）\n\n")
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

// UsageFor 返回指定主题（web/info/add/set/get/del/list/show/status/config）的帮助文本；\n// 未知主题回退为总帮助。
func UsageFor(topic string) string {
	switch strings.TrimSpace(topic) {
	case "web":
		return webUsage()
	case "info":
		return infoUsage()
	case "add":
		return addUsage()
	case "list":
		return listUsage()
	case "show":
		return showUsage()
	case "status":
		return statusUsage()
	case "config":
		return configUsage()
	case "set":
		return setUsage()
	case "get":
		return getUsage()
	case "del":
		return delUsage()
	default:
		return Usage()
	}
}

func webUsage() string {
	var b strings.Builder
	b.WriteString("用法: note web [start|stop|status] [参数]\n\n")
	b.WriteString("启动 Web 服务并内嵌前端页面。不带动作时前台运行，Ctrl+C 优雅退出。\n\n")
	b.WriteString("动作:\n")
	b.WriteString("  (无)                       前台运行，日志输出到终端\n")
	b.WriteString("  start                      后台启动；就绪后打印访问地址、pid 与日志路径\n")
	b.WriteString("  stop                       优雅停止后台实例（走关停接口，不是杀进程）\n")
	b.WriteString("  status                     输出运行情况；未运行时退出码 3\n\n")
	b.WriteString("参数:\n")
	b.WriteString("  -c, --config <路径>        配置文件路径\n")
	b.WriteString("  --addr <host:port>         监听地址（默认 127.0.0.1:8080）\n")
	b.WriteString("  -p, --port <端口>          只覆盖监听端口，host 沿用 --addr 或配置文件\n")
	b.WriteString("  --db <路径>                SQLite 数据库路径\n")
	b.WriteString("  --remote, --allow-remote   允许绑定非回环地址（有安全风险，应置于 TLS 与认证之后）\n")
	b.WriteString("  -h, --help                 显示本帮助\n\n")
	b.WriteString("后台实例的日志与状态文件写在数据库同目录：note.log / note.json。\n")
	b.WriteString("命令行参数优先于配置文件；未提供的项沿用配置文件或内置默认值。\n\n")
	b.WriteString("示例:\n")
	b.WriteString("  note web start              后台启动\n")
	b.WriteString("  note web status             查看运行情况\n")
	b.WriteString("  note web stop               停止后台实例\n")
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

// statusUsage 是 note status 的主题帮助；分区与 GroupEvents 的展示口径必须一致。
func statusUsage() string {
	var b strings.Builder
	b.WriteString("用法: note status [参数]\n\n")
	b.WriteString("输出使用统计：累计事件、活跃天数、查询次数、事件构成与 Top 查询词。\n")
	b.WriteString("数据直接读 activities 表，与网页概览页同源同口径：网页与 CLI 的操作都计入\n")
	b.WriteString("（查看笔记、新建、编辑、删除、恢复、查询、分类维护均算事件）。\n\n")
	b.WriteString("参数:\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  -h, --help             显示本帮助\n\n")
	b.WriteString("本命令只读：数据库文件不存在时只提示未创建，不建库也不写文件。\n\n")
	b.WriteString("退出码:\n")
	b.WriteString("  0  正常\n")
	b.WriteString("  1  读取失败\n")
	b.WriteString("  2  参数错误\n\n")
	b.WriteString("示例:\n")
	b.WriteString("  note status                       看累计事件与事件构成\n")
	b.WriteString("  note status --db ./data/notes.db  指定数据库\n")
	return b.String()
}

// listUsage 是 note list 的主题帮助；键位与实现（pick.go 的 picker）必须保持一致。
func listUsage() string {
	var b strings.Builder
	b.WriteString("用法: note list [参数]\n\n")
	b.WriteString("列出笔记：在交互式终端里进一个可模糊筛选的列表，Enter 预览选中的笔记；\n")
	b.WriteString("stdin 或 stdout 不是终端时（管道、重定向、CI）改为输出纯文本一行一条。\n\n")
	b.WriteString("参数:\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  --trash                只列回收站里的笔记\n")
	b.WriteString("  --plain                强制纯文本输出（真终端下也不进列表）\n")
	b.WriteString("  --limit <N>            非交互输出最多 N 条（0 = 全部，默认）\n")
	b.WriteString("  -h, --help             显示本帮助\n\n")
	b.WriteString("键位:\n")
	b.WriteString("  输入即筛选 · ↑/↓ 选择 · Enter 预览并复制原文 · Esc 退出\n\n")
	b.WriteString("筛选同时看标题与分类（fuzzy）和正文（子串）：标题/分类命中的排在前面，\n")
	b.WriteString("只命中正文的会在第二行显示命中处附近的一小段。\n\n")
	b.WriteString("非交互输出的列: id  标题  分类  updated_at（UTC 秒级文本，原样输出）。\n")
	b.WriteString("结果为空时不输出任何内容，退出码 0。\n\n")
	b.WriteString("退出码:\n")
	b.WriteString("  0  正常（包含没选就退出）\n")
	b.WriteString("  1  运行时失败\n")
	b.WriteString("  2  参数错误\n")
	b.WriteString("  4  选中的是加密笔记（CLI 没有主密码，只能看元信息）\n\n")
	b.WriteString("示例:\n")
	b.WriteString("  note list                       交互式筛选列表\n")
	b.WriteString("  note list --trash               看回收站\n")
	b.WriteString("  note list --plain --limit 20    取最近 20 条的纯文本\n")
	b.WriteString("  note show 9f2c1a3b-…           按 id 直接渲染一条\n")
	return b.String()
}

// showUsage 是 note show 的主题帮助。
func showUsage() string {
	var b strings.Builder
	b.WriteString("用法: note show [<id>] [参数]\n\n")
	b.WriteString("在终端里把一条笔记渲染成 Markdown（渲染内核是 glow 的 glamour）。\n")
	b.WriteString("标题与正文在库里是两个字段，因此真终端下先打印三行头部（标题、分类·时间·id、分隔线）。\n")
	b.WriteString("打开后会把原文（库里的 Markdown，不是渲染结果）复制到剪贴板；复制失败只提示，不影响查看。\n")
	b.WriteString("不给 id 时先弹选择器（与 note list 同一套筛选与键位），选中再渲染+复制；\n")
	b.WriteString("这条交互路径要求 stdin/stdout 都是终端。\n\n")
	b.WriteString("参数:\n")
	b.WriteString("  <id>                   笔记 id（可省略：省略时进选择器）\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  --style <名>           auto（默认）/ dark / light / notty / ascii / pink / dracula / tokyo-night\n")
	b.WriteString("  --width <N>            渲染宽度 20-1000；默认按终端宽度（上限 120）\n")
	b.WriteString("  --raw                  只输出原始 Markdown，不渲染\n")
	b.WriteString("  -h, --help             显示本帮助\n\n")
	b.WriteString("非终端（管道/重定向）下不打印头部，并默认用 notty 风格（纯文本、无 ANSI）；\n")
	b.WriteString("显式给了 --style 就按显式值走。非终端下也不动剪贴板：脚本里不该有看不见的副作用。\n\n")
	b.WriteString("加密笔记无法在 CLI 解密（主密码只在浏览器里）：打印元信息与提示，退出码 4。\n\n")
	b.WriteString("退出码:\n")
	b.WriteString("  0  正常\n")
	b.WriteString("  1  找不到笔记、渲染失败，或库里还没有笔记\n")
	b.WriteString("  2  参数错误\n")
	b.WriteString("  4  这条笔记是加密的（CLI 没有主密码，看不了也就没有可复制的原文）\n")
	b.WriteString("  130 选择器里没选就退出\n\n")
	b.WriteString("示例:\n")
	b.WriteString("  note show                            交互选一条，渲染并复制原文\n")
	b.WriteString("  note show 9f2c1a3b-…                 终端渲染并复制原文\n")
	b.WriteString("  note show 9f2c1a3b-… --raw            只输出原始 Markdown（不碰剪贴板）\n")
	b.WriteString("  note show 9f2c1a3b-… --style dark     指定风格\n")
	return b.String()
}

// addUsage 是 note add 的主题帮助；键位与向导实现（add.go）必须保持一致。
func addUsage() string {
	var b strings.Builder
	b.WriteString("用法: note add [全局参数]\n\n")
	b.WriteString("在交互式终端里依次完成三步，保存为一条明文笔记：\n")
	b.WriteString("  1. 选择分类（输入即模糊筛选；列表末尾可当场新建分类）\n")
	b.WriteString("  2. 输入标题（1-200 字符）\n")
	b.WriteString("  3. 输入正文（0-2000 码点，支持换行）\n\n")
	b.WriteString("键位:\n")
	b.WriteString("  分类步    ↑/↓ 选择 · 输入过滤 · Enter 确认 · Esc 放弃\n")
	b.WriteString("  标题步    Enter 下一步 · Esc 放弃\n")
	b.WriteString("  正文步    Enter 换行 · Ctrl+D 完成 · Ctrl+E 用 $EDITOR 编辑 · Esc 放弃\n\n")
	b.WriteString("全局参数:\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  -h, --help             显示本帮助\n\n")
	b.WriteString("note add 不接受其它参数；脚本化创建笔记请用 POST /api/notes。\n")
	b.WriteString("stdin 不是终端时报错退出，不会挂起等待输入。\n")
	return b.String()
}

// setUsage 是 note set 的主题帮助；寻址语法与 parse/ref 层必须一致。
func setUsage() string {
	var b strings.Builder
	b.WriteString("用法: note set <标题[@分类]> <正文> [参数]\n")
	b.WriteString("\n")
	b.WriteString("新建或覆盖一条明文笔记；不指定分类时落在内置分类「速记」。\n")
	b.WriteString("同一个分类里标题唯一（大小写不敏感）：同名就是覆盖那一条，不会新建第二条。\n")
	b.WriteString("分类不存在时自动创建（note set 周报@工作 正文）。\n")
	b.WriteString("\n")
	b.WriteString("标题寻址:\n")
	b.WriteString("  <标题>          速记分类里的这条\n")
	b.WriteString("  <标题>@<分类>   指定分类里的这条\n")
	b.WriteString("  --type <分类>   显式分类（标题里要写 @ 时用）\n")
	b.WriteString("\n")
	b.WriteString("参数:\n")
	b.WriteString("  <标题[@分类]>          标题 1-200 字符\n")
	b.WriteString("  <正文>                 0-2000 字符（传空字符串即清空正文）\n")
	b.WriteString("  --type <分类>          分类名（≤30 字符）\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  -h, --help             显示本帮助\n")
	b.WriteString("\n")
	b.WriteString("stdout 一个字节都不打印：成功提示写 stderr（「已保存「标题」@速记」/「已更新「标题」@速记」），\n")
	b.WriteString("这样 note set … && note get … 这类脚本管道不会被提示文案污染。\n")
	b.WriteString("同名笔记在回收站里时拒绝覆盖（先去网页恢复或彻底删除）；加密笔记也不能在这里覆盖。\n")
	b.WriteString("\n")
	b.WriteString("退出码:\n")
	b.WriteString("  0  正常\n")
	b.WriteString("  1  同名笔记在回收站里、命中的是加密笔记，或数据层失败\n")
	b.WriteString("  2  参数错误（缺正文、标题/分类名不合法、正文超长）\n")
	b.WriteString("\n")
	b.WriteString("示例:\n")
	b.WriteString("  note set 部署命令 \"make deploy\"        记一条速记\n")
	b.WriteString("  note set 周报@工作 \"本周结论\"         写进「工作」分类（分类不存在时自动建）\n")
	b.WriteString("  note set --type 工作 \"a@b\" \"正文\"   标题里要写 @ 时用 --type\n")
	return b.String()
}

// getUsage 是 note get 的主题帮助。
func getUsage() string {
	var b strings.Builder
	b.WriteString("用法: note get <标题[@分类]> | note get @<分类> [--detail] | note get --list [参数]\n")
	b.WriteString("\n")
	b.WriteString("note get <标题[@分类]>   把正文原样写到 stdout（缺结尾换行时补一个换行，便于终端与 wc -l）\n")
	b.WriteString("note get @<分类>         列出分类下未删除笔记：每行一个标题，按更新时间倒序\n")
	b.WriteString("note get @<分类> --detail   改成表格：标题 / 更新时间 / 正文（折成一行、最多 60 字）\n")
	b.WriteString("                            加密笔记的正文列写「（加密）」，空正文写「（空）」\n")
	b.WriteString("note get --list          列出所有分类：ID / 分类 / 笔记数 / 备注（固定分类备注列写「固定」）\n")
	b.WriteString("\n")
	b.WriteString("--list 与引用互斥；@<分类> 与标题也不能混写；--detail 只用于 @<分类>。\n")
	b.WriteString("\n")
	b.WriteString("参数:\n")
	b.WriteString("  --type <分类>          显式分类（标题里要写 @ 时用）\n")
	b.WriteString("  --detail               只对 @<分类> 有效：表格里带上时间与正文摘要\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  -h, --help             显示本帮助\n")
	b.WriteString("\n")
	b.WriteString("加密笔记在 CLI 里解不开（主密码只在浏览器里），退出码 4。\n")
	b.WriteString("回收站里的笔记不算「找到」：提示后退出码 1。\n")
	b.WriteString("\n")
	b.WriteString("退出码:\n")
	b.WriteString("  0  正常（含分类下没有笔记的空结果）\n")
	b.WriteString("  1  找不到笔记或分类\n")
	b.WriteString("  2  参数错误\n")
	b.WriteString("  4  命中的是加密笔记\n")
	b.WriteString("\n")
	b.WriteString("示例:\n")
	b.WriteString("  note get 部署命令            读回正文\n")
	b.WriteString("  note get 周报@工作           读指定分类里的那条\n")
	b.WriteString("  note get @工作               列出「工作」分类下的笔记（只有标题）\n")
	b.WriteString("  note get @工作 --detail      表格：标题 / 更新时间 / 正文摘要\n")
	b.WriteString("  note get --list              列出所有分类\n")
	return b.String()
}

// delUsage 是 note del 的主题帮助。
func delUsage() string {
	var b strings.Builder
	b.WriteString("用法: note del <标题[@分类]> | note del @<分类> --yes [参数]\n")
	b.WriteString("\n")
	b.WriteString("删除是软删除：只写 deleted_at，正文原样留在库里，可在网页回收站里恢复或彻底删除。\n")
	b.WriteString("删整个分类必须显式加 --yes：一次删一整类是危险操作，脚本里也该看得见。\n")
	b.WriteString("\n")
	b.WriteString("参数:\n")
	b.WriteString("  --type <分类>          显式分类（标题里要写 @ 时用）\n")
	b.WriteString("  --yes                  确认删除整个分类\n")
	b.WriteString("  -c, --config <路径>    配置文件路径\n")
	b.WriteString("  --db <路径>            SQLite 数据库路径\n")
	b.WriteString("  -h, --help             显示本帮助\n")
	b.WriteString("\n")
	b.WriteString("成功提示写 stderr，stdout 为空。加密笔记照常可删（软删除不碰正文/密文）。\n")
	b.WriteString("\n")
	b.WriteString("退出码:\n")
	b.WriteString("  0  正常\n")
	b.WriteString("  1  找不到、已在回收站里，或整分类删除没删到任何笔记\n")
	b.WriteString("  2  参数错误（含 del @<分类> 缺 --yes）\n")
	b.WriteString("\n")
	b.WriteString("示例:\n")
	b.WriteString("  note del 部署命令            移入回收站\n")
	b.WriteString("  note del 周报@工作           删指定分类里的那条\n")
	b.WriteString("  note del @工作 --yes         把「工作」分类下的笔记全部移入回收站\n")
	return b.String()
}
