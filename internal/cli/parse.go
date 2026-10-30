// Package cli 提供 note 命令行界面：子命令解析、交互菜单与系统信息输出。
// 服务装配与生命周期不属于本包——cmd/note 负责，cli 只做可测试的纯逻辑与交互。
package cli

import (
	"fmt"
	"strconv"
	"strings"
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
	// ID 是要渲染的笔记 id，必填。
	ID string
	// Style 是 glamour 风格名；空串表示 auto。
	Style string
	// Width 是渲染宽度；0 表示按终端宽度自动。
	Width int
	// Raw 表示只输出原始 Markdown，不渲染。
	Raw bool
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
//	note info / note help / note -h … → 其余命令
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
			if err := validateAddArgs(rest); err != nil {
				return CommandMenu, Options{}, err
			}
			opts.Args = rest
			return CommandAdd, opts, nil
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
			return CommandMenu, Options{}, fmt.Errorf("无法识别的命令 %s（可用命令: add, list, show, web, info, help）", arg)
		}
	}
	return CommandMenu, opts, nil
}

// addConfigFlags 是允许透传给配置加载的参数（只影响读哪个库/配置，不改变交互形态）。
// note add / list / show 共用它：这三条命令只把配置参数留给 config.Load，
// 自己的开关在各自的分支里消费掉。
var addConfigFlags = map[string]bool{
	"-c":       true,
	"--config": true,
	"--db":     true,
}

// validateAddArgs 校验 note add 的参数：只允许「带一个值的配置参数」，
// 交互形态本身不带任何开关，多余参数直接报错而不是被静默忽略。
func validateAddArgs(args []string) error {
	for i := 0; i < len(args); i++ {
		if !addConfigFlags[args[i]] {
			return fmt.Errorf("note add 只接受 -c/--config 与 --db 参数（用 note add --help 查看用法）")
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

// parseShowArgs 拆 note show 的参数：第一个位置参数是笔记 id，其余开关自己消费。
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
	if opts.ID == "" {
		return ShowOptions{}, nil, fmt.Errorf("note show 需要一个笔记 id（先用 note list 筛选，见 note show --help）")
	}
	return opts, configArgs, nil
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
