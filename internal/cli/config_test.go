package cli

import (
	"strings"
	"testing"
)

func TestFormatConfigListShowsValuesAndMaskedKey(t *testing.T) {
	items := []ConfigItem{
		{Key: "ai.base_url", Value: "https://api.openai.com/v1"},
		{Key: "ai.api_key", Value: "sk-…abcd"},
		{Key: "ai.prompt", Value: ""},
	}
	out := FormatConfigList("/home/u/.config/note/note.yaml", true, items)

	for _, want := range []string{
		"/home/u/.config/note/note.yaml",
		"ai.base_url", "https://api.openai.com/v1",
		"ai.api_key", "sk-…abcd",
		"ai.prompt", "（未设置）",
		"note config set",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("列表缺少 %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "还没创建") {
		t.Fatalf("文件存在时不该提示未创建:\n%s", out)
	}
}

func TestFormatConfigListMarksMissingFile(t *testing.T) {
	out := FormatConfigList("/tmp/note.yaml", false, []ConfigItem{{Key: "ai.base_url"}})
	if !strings.Contains(out, "还没创建") {
		t.Fatalf("文件不存在时应当提示:\n%s", out)
	}
}

// 三行的值必须从同一列开始，否则终端里对不齐。
func TestFormatConfigListAlignsColumns(t *testing.T) {
	out := FormatConfigList("p", true, []ConfigItem{
		{Key: "ai.base_url", Value: "a"},
		{Key: "ai.prompt", Value: "b"},
	})

	var columns []int
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ai.") {
			continue
		}
		// 值前面是补齐的空格，值的起点就是列位置
		columns = append(columns, len(strings.TrimRight(line, "ab")))
	}
	if len(columns) != 2 || columns[0] != columns[1] {
		t.Fatalf("列没有对齐: %v\n%s", columns, out)
	}
}

func TestFormatConfigListCollapsesAndTruncatesPrompt(t *testing.T) {
	prompt := "第一行\n\n第二行\t有制表符 " + strings.Repeat("填充", 40)
	out := FormatConfigList("p", true, []ConfigItem{{Key: "ai.prompt", Value: prompt}})

	if strings.Contains(out, "\n\n第二行") {
		t.Fatalf("多行值应当折成一行:\n%s", out)
	}
	if !strings.Contains(out, "第一行 第二行 有制表符") {
		t.Fatalf("折行后应当用空格连接:\n%s", out)
	}

	line := ""
	for _, candidate := range strings.Split(out, "\n") {
		if strings.HasPrefix(candidate, "  ai.prompt") {
			line = candidate
		}
	}
	if !strings.HasSuffix(line, "…") {
		t.Fatalf("超宽的值应当截断并加省略号，得到 %q", line)
	}
	runeCount := len([]rune(line))
	if runeCount > len("  ai.prompt  ")+configValueWidth+len("…") {
		t.Fatalf("截断后仍然过宽（%d 字符）：%q", runeCount, line)
	}
}

func TestFormatConfigSetMasksValue(t *testing.T) {
	out := FormatConfigSet("/tmp/note.yaml", ConfigItem{Key: "ai.api_key", Value: "sk-…efgh"})
	for _, want := range []string{"已写入 /tmp/note.yaml", "ai.api_key = sk-…efgh"} {
		if !strings.Contains(out, want) {
			t.Fatalf("回显缺少 %q:\n%s", want, out)
		}
	}

	cleared := FormatConfigSet("/tmp/note.yaml", ConfigItem{Key: "ai.api_key", Value: ""})
	if !strings.Contains(cleared, "（未设置）") {
		t.Fatalf("清空后的回显应当说明未设置:\n%s", cleared)
	}
}
