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
