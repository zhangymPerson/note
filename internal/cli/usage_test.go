package cli

import (
	"strings"
	"testing"
)

func TestUsageCoversCommandsAndDefaults(t *testing.T) {
	out := Usage()
	for _, want := range []string{
		"web", "info", "help",
		"note web", "note info",
		"-c, --config",
		"配置优先级",
		"配置文件默认位置",
		"数据库默认位置",
		"note.yaml",
		"notes.db",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("Usage 缺少 %q:\n%s", want, out)
		}
	}
}

func TestUsageForTopics(t *testing.T) {
	web := UsageFor("web")
	for _, want := range []string{
		"note web", "--addr", "-p, --port", "--db", "--remote, --allow-remote",
		"web start", "web stop", "web status", "note.log",
	} {
		if !strings.Contains(web, want) {
			t.Fatalf("web 用法缺少 %q:\n%s", want, web)
		}
	}

	info := UsageFor("info")
	for _, want := range []string{"note info", "-c, --config", "不输出任何密文、盐、口令或令牌"} {
		if !strings.Contains(info, want) {
			t.Fatalf("info 用法缺少 %q:\n%s", want, info)
		}
	}

	if UsageFor("unknown-topic") != Usage() {
		t.Fatal("未知主题应回退为总帮助")
	}
}

func TestUsageListsAddCommand(t *testing.T) {
	if !strings.Contains(Usage(), "add") {
		t.Fatal("总帮助的命令表里缺少 add")
	}
}

func TestUsageForAddDescribesThreeSteps(t *testing.T) {
	text := UsageFor("add")
	for _, want := range []string{"分类", "标题", "正文", "Ctrl+D"} {
		if !strings.Contains(text, want) {
			t.Fatalf("addUsage 缺少 %q", want)
		}
	}
}
