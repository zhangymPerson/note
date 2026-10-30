// Package cli 提供 note 命令行界面：子命令解析、交互菜单与系统信息输出。
// 服务装配与生命周期不属于本包——cmd/note 负责，cli 只做可测试的纯逻辑与交互。
package cli

import (
	"fmt"
	"strconv"
	"strings"

	"note/internal/config"
)

// Command 是一次命令行解析得到的子命令。
type Command int

const (
	// CommandMenu 表示未给出子命令：进入交互菜单。
	CommandMenu Command = iota
	// CommandWeb 启动 Web 服务，是唯一会启动前端页面的命令。
	CommandWeb
	// CommandInfo 输出软件与运行信息。
	CommandInfo
	// CommandAdd 走交互向导新建一条明文笔记。
	CommandAdd
	// CommandList 列出笔记并支持模糊筛选。
	CommandList
	// CommandShow 在终端里渲染一条笔记。
	CommandShow
	// CommandStatus 输出使用统计（事件构成、累计事件、活跃天数）。
	CommandStatus
	// CommandConfig 读写配置文件里的 AI 三项（list / set）。
	CommandConfig
	// CommandHelp 显示帮助。
	CommandHelp
)

// WebAction 是 `note web` 之后的动作：不带动作为前台运行（零值）。
type WebAction int

const (
	// WebForeground 前台运行 Web 服务（默认，Ctrl+C 退出）。
	WebForeground WebAction = iota
	// WebStart 后台启动一个脱离终端的实例。
	WebStart
	// WebStop 通过关停端点优雅停止后台实例。
	WebStop
	// WebStatus 输出后台实例的运行情况。
	WebStatus
)

// webActions 把子命令名映射为动作；未列出的名字不是合法动作。
var webActions = map[string]WebAction{
	"start":  WebStart,
	"stop":   WebStop,
	"status": WebStatus,
}

// Options 携带全局参数与子命令自己的参数列表。
type Options struct {
	// ConfigPath 是全局 -c/--config 指定的配置文件路径；空串表示未显式指定。
	ConfigPath string
	// Args 是子命令名之后的参数原样列表；帮助场景下携带帮助主题（如 "web"）。
	// list/show 只把 -c/--config 与 --db 留在这里交给 config.Load。
	Args []string
	// WebAction 仅对 CommandWeb 有意义：start/stop/status 或前台运行。
	WebAction WebAction
	// List 仅对 CommandList 有意义。
	List ListOptions
	// Show 仅对 CommandShow 有意义。
	Show ShowOptions
	// Config 仅对 CommandConfig 有意义。
	Config ConfigOptions
}

// ListOptions 是 note list 自己的开关。
type ListOptions struct {
	// Trash 只列回收站里的笔记。
	Trash bool
	// Plain 强制纯文本输出，不进交互列表。
	Plain bool
	// Limit 限制非交互输出的条数；0 表示全部。
	Limit int
}

// ShowOptions 是 note show 自己的开关。
type ShowOptions struct {
	// ID 是要渲染的笔记 id；空串表示先进交互选择器。
	ID string
	// Style 是 glamour 风格名；空串表示 auto。
	Style string
	// Width 是渲染宽度；0 表示按终端宽度自动。
	Width int
	// Raw 表示只输出原始 Markdown，不渲染。
	Raw bool
}

// ConfigOptions 是 note config 自己的参数。
type ConfigOptions struct {
	// List 为 true 表示 note config list；否则是 note config set。
	List bool
	// Key/Value 是 set 的键与值（已按第一个 = 拆开，值里可以再含 =）。
	Key   string
	Value string
	// ConfigPath 是子命令里出现的 -c/--config，比全局 -c 优先。
	ConfigPath string
}

// showStyles 是 note show 允许的风格名（glamour 的内置风格）。
var showStyles = map[string]bool{
	"auto": true, "dark": true, "light": true, "notty": true,
	"ascii": true, "pink": true, "dracula": true, "tokyo-night": true,
}

// showWidthMin/showWidthMax 是 --width 的合法区间：比 20 更窄没法读，
// 比 1000 更宽没有意义（自动宽度本来就封顶 120）。
const (
	showWidthMin = 20
	showWidthMax = 1000
)

// valueFlags 是会单独消耗一个值的参数名；预扫描 -h 时必须跳过它们的值，
// 否则 `note web --config -h` 会把 "-h" 误判成帮助请求。
var valueFlags = map[string]bool{
	"-c":       true,
	"--config": true,
	"--addr":   true,
	"--db":     true,
	"-p":       true,
	"--port":   true,
}

// Parse 解析全局参数与子命令分发。args 不含程序名。
//
// 形态：
//
//	note                    → CommandMenu（不带子命令：打印帮助）
//	note add                → CommandAdd（交互向导）
//	note -c <路径> web …    → CommandWeb（全局 -c 记入 Options.ConfigPath）
//	note web -p 9000 …      → CommandWeb（参数留在 Options.Args）
//	note config set a.b=c   → CommandConfig（key/value 已拆好）
//	note info / note status / note help / note -h … → 其余命令
func Parse(args []string) (Command, Options, error) {
	var opts Options
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			return CommandHelp, opts, nil
		case arg == "-c" || arg == "--config":
			if i+1 >= len(args) {
				return CommandMenu, Options{}, fmt.Errorf("%s 需要一个配置文件路径参数", arg)
			}
			i++
			opts.ConfigPath = args[i]
		case arg == "help":
			opts.Args = args[i+1:]
			return CommandHelp, opts, nil
		case arg == "add":
			rest := args[i+1:]
			if containsHelpFlag(rest) {
				opts.Args = []string{"add"}
				return CommandHelp, opts, nil
			}
			if err := validateConfigOnlyArgs("add", rest); err != nil {
				return CommandMenu, Options{}, err
			}
			opts.Args = rest
			return CommandAdd, opts, nil
		case arg == "status":
			rest := args[i+1:]
			if containsHelpFlag(rest) {
				opts.Args = []string{"status"}
				return CommandHelp, opts, nil
			}
			if err := validateConfigOnlyArgs("status", rest); err != nil {
				return CommandMenu, Options{}, err
			}
			opts.Args = rest
			return CommandStatus, opts, nil
		case arg == "list":
			rest := args[i+1:]
			if containsHelpFlag(rest) {
				opts.Args = []string{"list"}
				return CommandHelp, opts, nil
			}
			listOpts, configArgs, err := parseListArgs(rest)
			if err != nil {
				return CommandMenu, Options{}, err
			}
			opts.List = listOpts
			opts.Args = configArgs
			return CommandList, opts, nil
		case arg == "show":
			rest := args[i+1:]
			if containsHelpFlag(rest) {
				opts.Args = []string{"show"}
				return CommandHelp, opts, nil
			}
			showOpts, configArgs, err := parseShowArgs(rest)
			if err != nil {
				return CommandMenu, Options{}, err
			}
			opts.Show = showOpts
			opts.Args = configArgs
			return CommandShow, opts, nil
		case arg == "config":
			rest := args[i+1:]
			if containsHelpFlag(rest) {
				opts.Args = []string{"config"}
				return CommandHelp, opts, nil
			}
			configOpts, err := parseConfigArgs(rest)
			if err != nil {
				return CommandMenu, Options{}, err
			}
			opts.Config = configOpts
			return CommandConfig, opts, nil
		case arg == "web" || arg == "info":
			rest := args[i+1:]
			if arg == "web" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
				action, ok := webActions[rest[0]]
				if !ok {
					return CommandMenu, Options{}, fmt.Errorf("无法识别的 web 子命令 %s（可用: start, stop, status）", rest[0])
				}
				opts.WebAction = action
				rest = rest[1:]
			}
			if containsHelpFlag(rest) {
				opts.Args = []string{arg}
				return CommandHelp, opts, nil
			}
			opts.Args = rest
			if arg == "web" {
				return CommandWeb, opts, nil
			}
			return CommandInfo, opts, nil
		case strings.HasPrefix(arg, "-"):
			return CommandMenu, Options{}, fmt.Errorf("无法识别的参数 %s（启动 Web 服务请用 note web）", arg)
		default:
			return CommandMenu, Options{}, fmt.Errorf("无法识别的命令 %s（可用命令: add, list, show, status, web, info, help）", arg)
		}
	}
	return CommandMenu, opts, nil
}

// addConfigFlags 是允许透传给配置加载的参数（只影响读哪个库/配置，不改变交互形态）。
// note add / status / list / show 共用它：add 与 status 只把配置参数留给 config.Load，
// list / show 的开关在各自的分支里消费掉。
var addConfigFlags = map[string]bool{
	"-c":       true,
	"--config": true,
	"--db":     true,
}

// validateConfigOnlyArgs 校验只带配置参数的只读子命令（note add / note status）：
// 交互形态本身不带任何开关，多余参数直接报错而不是被静默忽略。
func validateConfigOnlyArgs(command string, args []string) error {
	for i := 0; i < len(args); i++ {
		if !addConfigFlags[args[i]] {
			return fmt.Errorf("note %s 只接受 -c/--config 与 --db 参数（用 note %s --help 查看用法）", command, command)
		}
		if i+1 >= len(args) {
			return fmt.Errorf("%s 需要一个值", args[i])
		}
		i++ // 跳过值
	}
	return nil
}

// parseListArgs 拆 note list 的参数：自己消费开关，把配置参数原样留在第二个返回值里。
func parseListArgs(args []string) (ListOptions, []string, error) {
	var opts ListOptions
	var configArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case addConfigFlags[arg]:
			if i+1 >= len(args) {
				return ListOptions{}, nil, fmt.Errorf("%s 需要一个值", arg)
			}
			configArgs = append(configArgs, arg, args[i+1])
			i++
		case arg == "--trash":
			opts.Trash = true
		case arg == "--plain":
			opts.Plain = true
		case arg == "--limit":
			if i+1 >= len(args) {
				return ListOptions{}, nil, fmt.Errorf("--limit 需要一个值")
			}
			limit, err := strconv.Atoi(args[i+1])
			if err != nil || limit < 0 {
				return ListOptions{}, nil, fmt.Errorf("--limit 需要 0 或正整数，得到 %q", args[i+1])
			}
			opts.Limit = limit
			i++
		default:
			return ListOptions{}, nil, fmt.Errorf("note list 无法识别的参数 %s（用 note list --help 查看用法）", arg)
		}
	}
	return opts, configArgs, nil
}

// parseShowArgs 拆 note show 的参数：第一个位置参数是笔记 id（可省略，省略时进选择器），
// 其余开关自己消费。
func parseShowArgs(args []string) (ShowOptions, []string, error) {
	var opts ShowOptions
	var configArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case addConfigFlags[arg]:
			if i+1 >= len(args) {
				return ShowOptions{}, nil, fmt.Errorf("%s 需要一个值", arg)
			}
			configArgs = append(configArgs, arg, args[i+1])
			i++
		case arg == "--raw":
			opts.Raw = true
		case arg == "--style":
			if i+1 >= len(args) {
				return ShowOptions{}, nil, fmt.Errorf("--style 需要一个值")
			}
			if !showStyles[args[i+1]] {
				return ShowOptions{}, nil, fmt.Errorf("--style 只接受 auto/dark/light/notty/ascii/pink/dracula/tokyo-night，得到 %q", args[i+1])
			}
			opts.Style = args[i+1]
			i++
		case arg == "--width":
			if i+1 >= len(args) {
				return ShowOptions{}, nil, fmt.Errorf("--width 需要一个值")
			}
			width, err := strconv.Atoi(args[i+1])
			if err != nil || width < showWidthMin || width > showWidthMax {
				return ShowOptions{}, nil, fmt.Errorf("--width 需要 %d 到 %d 之间的整数，得到 %q", showWidthMin, showWidthMax, args[i+1])
			}
			opts.Width = width
			i++
		case strings.HasPrefix(arg, "-"):
			return ShowOptions{}, nil, fmt.Errorf("note show 无法识别的参数 %s（用 note show --help 查看用法）", arg)
		default:
			if opts.ID != "" {
				return ShowOptions{}, nil, fmt.Errorf("note show 只接受一个笔记 id，多给了 %s", arg)
			}
			opts.ID = arg
		}
	}
	// 没给 id 不是错误：交互终端里进选择器（gum filter 那套）。
	// 是不是终端由 cmd/note 判定并给出可读错误，解析层不探测。
	return opts, configArgs, nil
}

// parseConfigArgs 拆 note config 的参数：list 只带配置参数，set 另需 key=value。
// 键必须在 config.KnownKeys() 白名单里，值合不合法由 config.SetValue 判定
// （解析层没必要重复一套 URL/长度规则）。
func parseConfigArgs(args []string) (ConfigOptions, error) {
	var opts ConfigOptions
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-c" || arg == "--config":
			if i+1 >= len(args) {
				return ConfigOptions{}, fmt.Errorf("%s 需要一个值", arg)
			}
			opts.ConfigPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "-"):
			return ConfigOptions{}, fmt.Errorf("note config 无法识别的参数 %s（用 note config --help 查看用法）", arg)
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) == 0 {
		return ConfigOptions{}, fmt.Errorf("note config 需要一个子命令：note config list 或 note config set <键>=<值>")
	}

	switch positional[0] {
	case "list":
		if len(positional) > 1 {
			return ConfigOptions{}, fmt.Errorf("note config list 不接受额外参数 %s", positional[1])
		}
		opts.List = true
		return opts, nil
	case "set":
		if len(positional) < 2 {
			return ConfigOptions{}, fmt.Errorf("note config set 需要一个 <键>=<值> 参数（可用键: %s）", strings.Join(config.KnownKeys(), ", "))
		}
		if len(positional) > 2 {
			// 值里有空格时 shell 会拆成多个参数，这里直接点明要加引号
			return ConfigOptions{}, fmt.Errorf("note config set 只接受一个 <键>=<值> 参数，值里有空格请用引号括起来")
		}
		key, value, ok := strings.Cut(positional[1], "=")
		if !ok {
			return ConfigOptions{}, fmt.Errorf("note config set 的参数要写成 <键>=<值>，得到 %q", positional[1])
		}
		key = strings.TrimSpace(key)
		if !config.IsKnownKey(key) {
			return ConfigOptions{}, fmt.Errorf("未知配置项 %q（可用: %s）", key, strings.Join(config.KnownKeys(), ", "))
		}
		opts.Key = key
		opts.Value = value
		return opts, nil
	default:
		return ConfigOptions{}, fmt.Errorf("无法识别的 config 子命令 %s（可用: list, set）", positional[0])
	}
}

// containsHelpFlag 判断参数列表里是否出现独立的 -h/--help。
// 带值参数（如 --config）的下一个 token 是值，不算 flag。
func containsHelpFlag(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-h" || arg == "--help" {
			return true
		}
		if valueFlags[arg] {
			i++
		}
	}
	return false
}
