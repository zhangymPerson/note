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

func TestUsageForStatusDescribesLedger(t *testing.T) {
	if !strings.Contains(Usage(), "note status") {
		t.Fatalf("总帮助应把 note status 摆出来：\n%s", Usage())
	}
	text := UsageFor("status")
	for _, want := range []string{"note status", "事件构成", "累计事件", "只读", "-c, --config", "--db"} {
		if !strings.Contains(text, want) {
			t.Fatalf("statusUsage 缺少 %q:\n%s", want, text)
		}
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

func TestUsageListsListAndShowCommands(t *testing.T) {
	out := Usage()
	for _, want := range []string{"  list ", "  show ", "note list", "note show"} {
		if !strings.Contains(out, want) {
			t.Fatalf("总帮助缺少 %q:\n%s", want, out)
		}
	}
}

func TestUsageForListDescribesFilterAndPlainOutput(t *testing.T) {
	text := UsageFor("list")
	for _, want := range []string{
		"note list", "--trash", "--plain", "--limit",
		"输入即筛选", "note show", "退出码", "updated_at",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("listUsage 缺少 %q:\n%s", want, text)
		}
	}
}

func TestUsageForShowDescribesStylesAndEncryptedExit(t *testing.T) {
	text := UsageFor("show")
	for _, want := range []string{
		"note show", "--style", "--width", "--raw", "notty", "加密", "4",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("showUsage 缺少 %q:\n%s", want, text)
		}
	}
}

func TestUsageListsConfigCommand(t *testing.T) {
	if !strings.Contains(Usage(), "note config") {
		t.Fatalf("总帮助应把 note config 摆出来：\n%s", Usage())
	}
}

func TestUsageForConfigDescribesKeysAndMasking(t *testing.T) {
	text := UsageFor("config")
	for _, want := range []string{
		"note config list", "note config set",
		"ai.base_url", "ai.api_key", "ai.models", "ai.prompt",
		"{{content}}", "默认提示词",
		"掩码", "0600", "原子", "<键>=<值>",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("configUsage 缺少 %q:\n%s", want, text)
		}
	}
}
