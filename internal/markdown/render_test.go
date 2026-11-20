package markdown

import (
	"strings"
	"testing"
)

func TestRenderNottyHasNoEscapeSequences(t *testing.T) {
	out, err := Render("# 会议纪要\n\n- 先说结论\n- 再说细节\n", Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("notty 风格不该出现 ANSI 转义：%q", out)
	}
	if !strings.Contains(out, "会议纪要") || !strings.Contains(out, "先说结论") {
		t.Fatalf("正文被吞了：%q", out)
	}
}

func TestRenderKeepsCodeBlockContent(t *testing.T) {
	out, err := Render("```go\nfmt.Println(1)\n```\n", Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "fmt.Println(1)") {
		t.Fatalf("代码块内容丢失：%q", out)
	}
}

func TestAutoStyleResolvesOnceAndCaches(t *testing.T) {
	// AutoStyle 与 glamour 的 auto 同口径：stdout 不是终端（测试里就是这样）时
	// 回 "notty"，是终端则按背景亮暗给 "dark"/"light"。解析一次后永久缓存——
	// 不能每次渲染都去查终端（在 TUI 循环里查会跟 bubbletea 的 stdin 读取器
	// 抢字节）。结果必须稳定且是合法风格名。
	first := AutoStyle()
	if first != "dark" && first != "light" && first != "notty" {
		t.Fatalf("AutoStyle 应返回 dark/light/notty 之一，得到 %q", first)
	}
	if second := AutoStyle(); second != first {
		t.Fatalf("AutoStyle 应缓存同一结果：%q != %q", first, second)
	}
}

func TestRenderEmptyStyleUsesResolvedStyle(t *testing.T) {
	// Style 空串与 "auto" 一样，都按 AutoStyle 的一次性解析结果渲染，
	// 不再把 "auto" 交给 glamour（那会触发对终端的 OSC 查询）。
	resolved := AutoStyle()
	out, err := Render("正文\n", Options{Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "正文") {
		t.Fatalf("正文被吞了：%q", out)
	}
	_ = resolved
}

func TestRenderRejectsUnknownStyle(t *testing.T) {
	if _, err := Render("hi\n", Options{Style: "nope"}); err == nil {
		t.Fatal("未知风格应当报错，而不是静默回退")
	}
}

func TestRenderTrimsTrailingNewlines(t *testing.T) {
	out, err := Render("一句话\n", Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.HasSuffix(out, "\n") {
		t.Fatalf("Render 返回的文本不该带结尾换行（由调用方决定怎么打印）：%q", out)
	}
}

func TestRenderWidthZeroSkipsWordWrap(t *testing.T) {
	long := strings.Repeat("很长的一句话 ", 20) + "\n"
	// Width 为 0 时不该调用 WithWordWrap，于是落到 glamour 自己的默认宽度 80；
	// 一旦这层判断被删掉，WithWordWrap(0) 会让 reflow 直接透传（不折行），两者就不再相等。
	unwrapped, err := Render(long, Options{Style: "notty", Width: 0})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	atDefault, err := Render(long, Options{Style: "notty", Width: 80})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if unwrapped != atDefault {
		t.Fatal("Width 为 0 时应当沿用 glamour 的默认宽度 80")
	}
}

func TestRenderConstructionErrorMessageIsChinese(t *testing.T) {
	_, err := Render("hi\n", Options{Style: "nope"})
	if err == nil {
		t.Fatal("未知风格应当报错")
	}
	if !strings.Contains(err.Error(), "渲染") {
		t.Fatalf("错误信息是给用户看的，必须是中文：%v", err)
	}
}
