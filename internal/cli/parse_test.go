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

func TestParseConfigList(t *testing.T) {
	cases := []struct {
		name string
		args []string
		path string
	}{
		{"plain", []string{"config", "list"}, ""},
		{"with config flag", []string{"config", "list", "-c", "x.yaml"}, "x.yaml"},
		{"with --config flag", []string{"config", "list", "--config", "x.yaml"}, "x.yaml"},
	}
	for _, tc := range cases {
		cmd, opts, err := Parse(tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if cmd != CommandConfig {
			t.Fatalf("%s: cmd = %v, want CommandConfig", tc.name, cmd)
		}
		if !opts.Config.List || opts.Config.Key != "" {
			t.Fatalf("%s: 应当解析成 list，得到 %+v", tc.name, opts.Config)
		}
		if opts.Config.ConfigPath != tc.path {
			t.Fatalf("%s: config path = %q, want %q", tc.name, opts.Config.ConfigPath, tc.path)
		}
	}
}

func TestParseConfigSet(t *testing.T) {
	cmd, opts, err := Parse([]string{"config", "set", "ai.api_key=sk-abc=def"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandConfig {
		t.Fatalf("cmd = %v, want CommandConfig", cmd)
	}
	if opts.Config.List {
		t.Fatal("set 不该被解析成 list")
	}
	// 只按第一个 = 拆：密钥里带 = 时不该被截断
	if opts.Config.Key != "ai.api_key" || opts.Config.Value != "sk-abc=def" {
		t.Fatalf("key/value = %q/%q", opts.Config.Key, opts.Config.Value)
	}

	// 空值表示清空
	_, opts, err = Parse([]string{"config", "set", "ai.base_url="})
	if err != nil {
		t.Fatalf("parse empty value: %v", err)
	}
	if opts.Config.Value != "" {
		t.Fatalf("空值应当保留为空串，得到 %q", opts.Config.Value)
	}
}

func TestParseConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"没有子命令", []string{"config"}, "需要一个子命令"},
		{"未知子命令", []string{"config", "show"}, "无法识别的 config 子命令 show"},
		{"list 多给参数", []string{"config", "list", "extra"}, "不接受额外参数"},
		{"set 缺键值", []string{"config", "set"}, "需要一个 <键>=<值>"},
		{"set 少等号", []string{"config", "set", "ai.api_key"}, "<键>=<值>"},
		{"set 未知键", []string{"config", "set", "ai.model=gpt"}, "未知配置项"},
		{"set 值没加引号", []string{"config", "set", "ai.prompt=hello", "world"}, "引号"},
		{"未知开关", []string{"config", "list", "--all"}, "无法识别的参数"},
	}
	for _, tc := range cases {
		_, _, err := Parse(tc.args)
		if err == nil {
			t.Fatalf("%s：应当报错", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s：错误信息应当含 %q，得到 %v", tc.name, tc.want, err)
		}
	}
}

func TestParseConfigHelp(t *testing.T) {
	cmd, opts, err := Parse([]string{"config", "--help"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cmd != CommandHelp || !reflect.DeepEqual(opts.Args, []string{"config"}) {
		t.Fatalf("cmd=%v opts=%+v", cmd, opts)
	}
}

func TestParseSetArgs(t *testing.T) {
	cmd, opts, err := Parse([]string{"set", "周报", "正文内容"})
	if err != nil {
		t.Fatalf("parse set: %v", err)
	}
	if cmd != CommandSet {
		t.Fatalf("cmd = %v, want CommandSet", cmd)
	}
	want := Ref{Title: "周报", Category: QuickCategoryName}
	if opts.Set.Ref != want || opts.Set.Content != "正文内容" {
		t.Fatalf("unexpected set options: %+v", opts.Set)
	}
}

func TestParseSetRejectsExtraArgs(t *testing.T) {
	if _, _, err := Parse([]string{"set", "周报", "正文", "多余"}); err == nil {
		t.Fatal("expected an error for extra positional arguments")
	}
	if _, _, err := Parse([]string{"set", "只有标题"}); err == nil {
		t.Fatal("expected an error when the content is missing")
	}
}

func TestParseSetAcceptsTypeFlag(t *testing.T) {
	cmd, opts, err := Parse([]string{"set", "--type", "工作", "周报", "正文"})
	if err != nil {
		t.Fatalf("parse set --type: %v", err)
	}
	if cmd != CommandSet {
		t.Fatalf("cmd = %v, want CommandSet", cmd)
	}
	if opts.Set.Ref != (Ref{Title: "周报", Category: "工作"}) {
		t.Fatalf("unexpected ref: %+v", opts.Set.Ref)
	}
}

func TestParseGetList(t *testing.T) {
	cmd, opts, err := Parse([]string{"get", "--list"})
	if err != nil {
		t.Fatalf("parse get --list: %v", err)
	}
	if cmd != CommandGet || !opts.Get.List {
		t.Fatalf("cmd = %v opts = %+v, want CommandGet with List", cmd, opts.Get)
	}
	if opts.Get.Ref != (Ref{}) {
		t.Fatalf("expected an empty ref with --list, got %+v", opts.Get.Ref)
	}
}

func TestParseGetRejectsListWithTarget(t *testing.T) {
	if _, _, err := Parse([]string{"get", "--list", "@工作"}); err == nil {
		t.Fatal("expected --list and a target to be mutually exclusive")
	}
}

func TestParseGetCategoryRef(t *testing.T) {
	cmd, opts, err := Parse([]string{"get", "@工作"})
	if err != nil {
		t.Fatalf("parse get @工作: %v", err)
	}
	if cmd != CommandGet || opts.Get.Ref != (Ref{Category: "工作", CategoryOnly: true}) {
		t.Fatalf("unexpected options: %v %+v", cmd, opts.Get)
	}
}

func TestParseGetDetail(t *testing.T) {
	cmd, opts, err := Parse([]string{"get", "@工作", "--detail"})
	if err != nil {
		t.Fatalf("parse get @工作 --detail: %v", err)
	}
	if cmd != CommandGet || !opts.Get.Detail || !opts.Get.Ref.CategoryOnly {
		t.Fatalf("unexpected options: %v %+v", cmd, opts.Get)
	}

	// --detail 只对「列一个分类」有意义：配上标题或 --list 都是用法错误。
	if _, _, err := Parse([]string{"get", "周报", "--detail"}); err == nil {
		t.Fatal("expected --detail with a title to be rejected")
	}
	if _, _, err := Parse([]string{"get", "--list", "--detail"}); err == nil {
		t.Fatal("expected --detail with --list to be rejected")
	}
}

func TestParseDelRequiresYesForCategory(t *testing.T) {
	if _, _, err := Parse([]string{"del", "@工作"}); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected a --yes hint, got %v", err)
	}
	cmd, opts, err := Parse([]string{"del", "@工作", "--yes"})
	if err != nil {
		t.Fatalf("parse del @工作 --yes: %v", err)
	}
	if cmd != CommandDel || !opts.Del.Yes || !opts.Del.Ref.CategoryOnly {
		t.Fatalf("unexpected del options: %v %+v", cmd, opts.Del)
	}
}

func TestParseQuickCommandsAcceptConfigFlags(t *testing.T) {
	cmd, opts, err := Parse([]string{"get", "周报", "-c", "x.yaml"})
	if err != nil {
		t.Fatalf("parse with config flag: %v", err)
	}
	if cmd != CommandGet || !reflect.DeepEqual(opts.Args, []string{"-c", "x.yaml"}) {
		t.Fatalf("cmd = %v args = %v", cmd, opts.Args)
	}
}

func TestParseQuickCommandHelp(t *testing.T) {
	for _, name := range []string{"set", "get", "del"} {
		cmd, opts, err := Parse([]string{name, "--help"})
		if err != nil {
			t.Fatalf("parse %s --help: %v", name, err)
		}
		if cmd != CommandHelp || !reflect.DeepEqual(opts.Args, []string{name}) {
			t.Fatalf("%s: cmd = %v args = %v", name, cmd, opts.Args)
		}
	}
}
