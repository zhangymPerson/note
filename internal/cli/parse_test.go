package cli

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestParseNoArgsOpensMenu(t *testing.T) {
	cmd, opts, err := Parse(nil)
	if err != nil || cmd != CommandMenu {
		t.Fatalf("no args: cmd=%v opts=%+v err=%v", cmd, opts, err)
	}
}

func TestParseSubcommands(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Command
	}{
		{"web", []string{"web"}, CommandWeb},
		{"info", []string{"info"}, CommandInfo},
		{"web with flags", []string{"web", "-p", "9000", "--remote"}, CommandWeb},
		{"info with config", []string{"info", "-c", "x.yaml"}, CommandInfo},
	}
	for _, tc := range cases {
		cmd, opts, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cmd != tc.want {
			t.Fatalf("%s: cmd = %v, want %v", tc.name, cmd, tc.want)
		}
		if tc.name == "web with flags" && !reflect.DeepEqual(opts.Args, []string{"-p", "9000", "--remote"}) {
			t.Fatalf("%s: args = %v", tc.name, opts.Args)
		}
	}
}

func TestParseGlobalConfigBeforeSubcommand(t *testing.T) {
	cmd, opts, err := Parse([]string{"-c", "a.yaml", "web", "--port", "9000"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandWeb || opts.ConfigPath != "a.yaml" {
		t.Fatalf("cmd=%v opts=%+v", cmd, opts)
	}
	if !reflect.DeepEqual(opts.Args, []string{"--port", "9000"}) {
		t.Fatalf("args = %v", opts.Args)
	}
}

func TestParseHelpVariants(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want []string // 期望的帮助主题
	}{
		{"-h", []string{"-h"}, nil},
		{"--help", []string{"--help"}, nil},
		{"help", []string{"help"}, nil},
		{"help web", []string{"help", "web"}, []string{"web"}},
		{"web -h", []string{"web", "-h"}, []string{"web"}},
		{"web --help", []string{"web", "--help"}, []string{"web"}},
		{"info -h", []string{"info", "-h"}, []string{"info"}},
		{"help 后带 -c", []string{"help", "web", "-c", "x"}, []string{"web", "-c", "x"}},
	}
	for _, tc := range cases {
		cmd, opts, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cmd != CommandHelp {
			t.Fatalf("%s: cmd = %v", tc.name, cmd)
		}
		if tc.want != nil && !reflect.DeepEqual(opts.Args, tc.want) {
			t.Fatalf("%s: args = %v, want %v", tc.name, opts.Args, tc.want)
		}
	}
}

func TestParseHelpFlagSkipsFlagValues(t *testing.T) {
	// --config 的值 "-h" 不是帮助请求
	cmd, opts, err := Parse([]string{"web", "--config", "-h"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandWeb {
		t.Fatalf("cmd = %v, want CommandWeb", cmd)
	}
	if !reflect.DeepEqual(opts.Args, []string{"--config", "-h"}) {
		t.Fatalf("args = %v", opts.Args)
	}

	// --port 的值 "--help" 同理
	cmd, _, err = Parse([]string{"info", "--port", "--help"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandInfo {
		t.Fatalf("cmd = %v, want CommandInfo", cmd)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"未知命令", []string{"foo"}},
		{"未知参数", []string{"--addr", "1.2.3.4:80"}},
		{"裸 -c 缺值", []string{"-c"}},
		{"裸 --config 缺值", []string{"--config"}},
	}
	for _, tc := range cases {
		_, _, err := Parse(tc.args)
		if err == nil {
			t.Fatalf("%s: 期望报错", tc.name)
		}
	}
}

func TestParseGlobalConfigAloneStillMenu(t *testing.T) {
	cmd, opts, err := Parse([]string{"-c", "a.yaml"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandMenu || opts.ConfigPath != "a.yaml" {
		t.Fatalf("cmd=%v opts=%+v", cmd, opts)
	}
}

func TestParseWebActions(t *testing.T) {
	cases := []struct {
		args []string
		want WebAction
		rest []string
	}{
		{[]string{"web"}, WebForeground, nil},
		{[]string{"web", "start", "-p", "9000"}, WebStart, []string{"-p", "9000"}},
		{[]string{"web", "stop"}, WebStop, nil},
		{[]string{"web", "status"}, WebStatus, nil},
		{[]string{"web", "--addr", "0.0.0.0:8080", "--remote"}, WebForeground, []string{"--addr", "0.0.0.0:8080", "--remote"}},
	}
	for _, tc := range cases {
		cmd, opts, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if cmd != CommandWeb || opts.WebAction != tc.want {
			t.Fatalf("%v: cmd=%v action=%v, want %v", tc.args, cmd, opts.WebAction, tc.want)
		}
		if !slices.Equal(opts.Args, tc.rest) {
			t.Fatalf("%v: args = %v, want %v", tc.args, opts.Args, tc.rest)
		}
	}
}

func TestParseWebRejectsUnknownAction(t *testing.T) {
	_, _, err := Parse([]string{"web", "serve"})
	if err == nil || !strings.Contains(err.Error(), "无法识别的 web 子命令 serve") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseWebStatusHelp(t *testing.T) {
	cmd, opts, err := Parse([]string{"web", "status", "-h"})
	if err != nil || cmd != CommandHelp || !reflect.DeepEqual(opts.Args, []string{"web"}) {
		t.Fatalf("cmd=%v opts=%+v err=%v", cmd, opts, err)
	}
}

func TestParseAddCommand(t *testing.T) {
	cmd, opts, err := Parse([]string{"add"})
	if err != nil || cmd != CommandAdd {
		t.Fatalf("Parse(add) = %v, %v; want CommandAdd", cmd, err)
	}
	if len(opts.Args) != 0 {
		t.Fatalf("add 不带参数，Args 应为空，得到 %v", opts.Args)
	}
}

func TestParseAddHelpFlag(t *testing.T) {
	cmd, opts, err := Parse([]string{"add", "-h"})
	if err != nil || cmd != CommandHelp {
		t.Fatalf("Parse(add -h) = %v, %v; want CommandHelp", cmd, err)
	}
	if len(opts.Args) != 1 || opts.Args[0] != "add" {
		t.Fatalf("帮助主题应为 [add]，得到 %v", opts.Args)
	}
}

func TestParseAddRejectsExtraArgs(t *testing.T) {
	if _, _, err := Parse([]string{"add", "foo"}); err == nil {
		t.Fatal("note add 不接受多余参数，应报错")
	}
}

func TestParseStatusCommand(t *testing.T) {
	cmd, opts, err := Parse([]string{"status", "--db", "x.db"})
	if err != nil || cmd != CommandStatus {
		t.Fatalf("Parse(status) = %v, %v; want CommandStatus", cmd, err)
	}
	if !reflect.DeepEqual(opts.Args, []string{"--db", "x.db"}) {
		t.Fatalf("配置参数应原样透传，得到 %v", opts.Args)
	}
}

func TestParseStatusHelp(t *testing.T) {
	cmd, opts, err := Parse([]string{"status", "--help"})
	if err != nil || cmd != CommandHelp {
		t.Fatalf("Parse(status -h) = %v, %v; want CommandHelp", cmd, err)
	}
	if len(opts.Args) != 1 || opts.Args[0] != "status" {
		t.Fatalf("帮助主题应为 [status]，得到 %v", opts.Args)
	}
}

func TestParseStatusIsNotWebStatus(t *testing.T) {
	// note web status 仍是后台实例状态，不能被顶层 status 吃掉
	cmd, opts, err := Parse([]string{"web", "status"})
	if err != nil || cmd != CommandWeb || opts.WebAction != WebStatus {
		t.Fatalf("cmd=%v opts=%+v err=%v", cmd, opts, err)
	}
}

func TestParseStatusRejectsExtraArgs(t *testing.T) {
	for _, args := range [][]string{{"status", "--days", "7"}, {"status", "extra"}, {"status", "--db"}} {
		if _, _, err := Parse(args); err == nil {
			t.Fatalf("note status 不接受 %v，应报错", args)
		}
	}
}

func TestParseListFlags(t *testing.T) {
	cmd, opts, err := Parse([]string{"list", "--trash", "--plain", "--limit", "5", "--db", "x.db"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandList {
		t.Fatalf("cmd = %v, want CommandList", cmd)
	}
	if opts.List != (ListOptions{Trash: true, Plain: true, Limit: 5}) {
		t.Fatalf("ListOptions = %+v", opts.List)
	}
	// 配置参数要留给 config.Load，自己消费的开关不能留在 Args 里
	if !slices.Equal(opts.Args, []string{"--db", "x.db"}) {
		t.Fatalf("Args = %v", opts.Args)
	}
}

func TestParseListDefaults(t *testing.T) {
	cmd, opts, err := Parse([]string{"list"})
	if err != nil || cmd != CommandList {
		t.Fatalf("cmd=%v err=%v", cmd, err)
	}
	if opts.List != (ListOptions{}) {
		t.Fatalf("默认 ListOptions 应当是全零，得到 %+v", opts.List)
	}
	if len(opts.Args) != 0 {
		t.Fatalf("Args 应当为空，得到 %v", opts.Args)
	}
}

func TestParseShowFlags(t *testing.T) {
	cmd, opts, err := Parse([]string{"show", "9f2c1a3b", "--style", "dark", "--width", "100", "--raw", "-c", "x.yaml"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandShow {
		t.Fatalf("cmd = %v, want CommandShow", cmd)
	}
	want := ShowOptions{ID: "9f2c1a3b", Style: "dark", Width: 100, Raw: true}
	if opts.Show != want {
		t.Fatalf("ShowOptions = %+v, want %+v", opts.Show, want)
	}
	if !slices.Equal(opts.Args, []string{"-c", "x.yaml"}) {
		t.Fatalf("Args = %v", opts.Args)
	}
}

func TestParseShowDefaults(t *testing.T) {
	cmd, opts, err := Parse([]string{"show", "abc"})
	if err != nil || cmd != CommandShow {
		t.Fatalf("cmd=%v err=%v", cmd, err)
	}
	if opts.Show != (ShowOptions{ID: "abc"}) {
		t.Fatalf("ShowOptions = %+v", opts.Show)
	}
}

func TestParseListAndShowHelp(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"list", "-h"}, "list"},
		{[]string{"list", "--help"}, "list"},
		{[]string{"show", "-h"}, "show"},
		{[]string{"show", "--help"}, "show"},
	}
	for _, tc := range cases {
		cmd, opts, err := Parse(tc.args)
		if err != nil || cmd != CommandHelp {
			t.Fatalf("%v: cmd=%v err=%v", tc.args, cmd, err)
		}
		if !slices.Equal(opts.Args, []string{tc.want}) {
			t.Fatalf("%v: 帮助主题 = %v, want [%s]", tc.args, opts.Args, tc.want)
		}
	}
}

func TestParseListAndShowRejectBadArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"list 未知参数", []string{"list", "--nope"}},
		{"list limit 非数字", []string{"list", "--limit", "abc"}},
		{"list limit 负数", []string{"list", "--limit", "-1"}},
		{"list limit 缺值", []string{"list", "--limit"}},
		{"list 多给了位置参数", []string{"list", "abc"}},
		{"show 多个 id", []string{"show", "a", "b"}},
		{"show 未知参数", []string{"show", "a", "--nope"}},
		{"show style 非法", []string{"show", "a", "--style", "nope"}},
		{"show width 太小", []string{"show", "a", "--width", "5"}},
		{"show width 太大", []string{"show", "a", "--width", "1001"}},
		{"show width 非数字", []string{"show", "a", "--width", "abc"}},
		{"show width 缺值", []string{"show", "a", "--width"}},
	}
	for _, tc := range cases {
		if _, _, err := Parse(tc.args); err == nil {
			t.Fatalf("%s：期望报错，实际通过", tc.name)
		}
	}
}

// 不给 id 不是错误：note show 会进选择器（是不是终端由 cmd/note 判定）。
func TestParseShowWithoutIDIsPicker(t *testing.T) {
	cmd, opts, err := Parse([]string{"show"})
	if err != nil || cmd != CommandShow {
		t.Fatalf("cmd=%v err=%v", cmd, err)
	}
	if opts.Show != (ShowOptions{}) {
		t.Fatalf("ShowOptions 应当是全零（无 id、无开关），得到 %+v", opts.Show)
	}

	// 配置参数仍然可以透传，且不会因此变成「有 id」
	cmd, opts, err = Parse([]string{"show", "--db", "x.db"})
	if err != nil || cmd != CommandShow || opts.Show.ID != "" {
		t.Fatalf("cmd=%v opts=%+v err=%v", cmd, opts, err)
	}
	if !slices.Equal(opts.Args, []string{"--db", "x.db"}) {
		t.Fatalf("Args = %v", opts.Args)
	}
}

func TestParseAddAcceptsConfigFlags(t *testing.T) {
	cmd, opts, err := Parse([]string{"add", "--db", "x.db"})
	if err != nil || cmd != CommandAdd {
		t.Fatalf("note add --db 应被接受，得到 cmd=%v err=%v", cmd, err)
	}
	if !slices.Equal(opts.Args, []string{"--db", "x.db"}) {
		t.Fatalf("args = %v", opts.Args)
	}
	if _, _, err := Parse([]string{"add", "--db"}); err == nil {
		t.Fatal("--db 缺值应报错")
	}
	if _, _, err := Parse([]string{"add", "--addr", "0.0.0.0:1"}); err == nil {
		t.Fatal("note add 只接受 -c/--config/--db，--addr 应被拒绝")
	}
}
