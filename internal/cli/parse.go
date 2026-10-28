// Package cli 提供 note 命令行界面：子命令解析、交互菜单与系统信息输出。
// 服务装配与生命周期不属于本包——cmd/note 负责，cli 只做可测试的纯逻辑与交互。
package cli

import (
	"fmt"
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
	// CommandHelp 显示帮助。
	CommandHelp
)

// Options 携带全局参数与子命令自己的参数列表。
type Options struct {
	// ConfigPath 是全局 -c/--config 指定的配置文件路径；空串表示未显式指定。
	ConfigPath string
	// Args 是子命令名之后的参数原样列表；帮助场景下携带帮助主题（如 "web"）。
	Args []string
}

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
//	note                    → CommandMenu
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
		case arg == "web" || arg == "info":
			rest := args[i+1:]
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
			return CommandMenu, Options{}, fmt.Errorf("无法识别的命令 %s（可用命令: web, info, help）", arg)
		}
	}
	return CommandMenu, opts, nil
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
