package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func plainDetail() NoteDetail {
	return NoteDetail{
		ID:           "9f2c1a3b-4d5e-4f60-8a71-2b3c4d5e6f70",
		Title:        "会议纪要",
		CategoryName: "工作",
		UpdatedAt:    "2026-10-29T14:03:05Z",
		Content:      "# 会议纪要\n\n- 先说结论\n- 再说细节\n",
	}
}

func detailDeps(detail NoteDetail, gotID *string) ShowDeps {
	return ShowDeps{Note: func(ctx context.Context, id string) (NoteDetail, error) {
		if gotID != nil {
			*gotID = id
		}
		return detail, nil
	}}
}

func TestResolveStyleMatrix(t *testing.T) {
	cases := []struct {
		style string
		tty   bool
		want  string
	}{
		{"", true, "auto"},
		{"", false, "notty"},
		{"dark", false, "dark"},
		{"notty", true, "notty"},
	}
	for _, tc := range cases {
		if got := resolveStyle(tc.style, tc.tty); got != tc.want {
			t.Fatalf("resolveStyle(%q, %v) = %q, want %q", tc.style, tc.tty, got, tc.want)
		}
	}
}

func TestResolveWidthMatrix(t *testing.T) {
	cases := []struct {
		width int
		term  int
		want  int
	}{
		{0, 200, 120},
		{0, 60, 60},
		{0, 10, 20},
		{0, 0, 80},
		{40, 200, 40},
	}
	for _, tc := range cases {
		if got := resolveWidth(tc.width, tc.term); got != tc.want {
			t.Fatalf("resolveWidth(%d, %d) = %d, want %d", tc.width, tc.term, got, tc.want)
		}
	}
}

func TestBuildShowTTYHasHeader(t *testing.T) {
	var gotID string
	res, err := BuildShow(context.Background(), detailDeps(plainDetail(), &gotID), ShowOptions{ID: "abc", Style: "notty"}, true, 80)
	if err != nil {
		t.Fatalf("BuildShow: %v", err)
	}
	if gotID != "abc" {
		t.Fatalf("应当把 id 透传给 deps，得到 %q", gotID)
	}
	for _, want := range []string{"会议纪要", "工作", plainDetail().ID} {
		if !strings.Contains(res.Header, want) {
			t.Fatalf("头部缺少 %q：\n%s", want, res.Header)
		}
	}
	if strings.Count(res.Header, "\n") != 2 {
		t.Fatalf("头部应当三行（标题、元信息、分隔线）：\n%s", res.Header)
	}
	if res.Body == "" {
		t.Fatal("正文不能为空")
	}
	if strings.Contains(res.Body, "\x1b[") {
		t.Fatalf("notty 风格不该带 ANSI：%q", res.Body)
	}
}

func TestBuildShowNonTTYHasNoHeader(t *testing.T) {
	res, err := BuildShow(context.Background(), detailDeps(plainDetail(), nil), ShowOptions{ID: "abc", Style: "notty"}, false, 80)
	if err != nil {
		t.Fatalf("BuildShow: %v", err)
	}
	if res.Header != "" {
		t.Fatalf("非 TTY 下不该打印头部，得到 %q", res.Header)
	}
	if !strings.Contains(res.Body, "先说结论") {
		t.Fatalf("正文应当渲染出来，得到 %q", res.Body)
	}
}

func TestBuildShowTrashedMarksHeader(t *testing.T) {
	detail := plainDetail()
	detail.Trashed = true
	res, err := BuildShow(context.Background(), detailDeps(detail, nil), ShowOptions{ID: "abc", Style: "notty"}, true, 80)
	if err != nil {
		t.Fatalf("BuildShow: %v", err)
	}
	if !strings.Contains(res.Header, "回收站") {
		t.Fatalf("回收站里的笔记应当在头部标注：\n%s", res.Header)
	}
}

func TestBuildShowRawSkipsRendering(t *testing.T) {
	detail := plainDetail()
	res, err := BuildShow(context.Background(), detailDeps(detail, nil), ShowOptions{ID: "abc", Raw: true}, true, 80)
	if err != nil {
		t.Fatalf("BuildShow: %v", err)
	}
	if res.Header != "" {
		t.Fatalf("--raw 不打印头部，得到 %q", res.Header)
	}
	if res.Body != detail.Content {
		t.Fatalf("--raw 应当原样输出 Markdown：\n%q\nwant\n%q", res.Body, detail.Content)
	}
}

func TestBuildShowEncryptedReturnsHintAndError(t *testing.T) {
	detail := plainDetail()
	detail.Encrypted = true
	detail.Content = ""

	res, err := BuildShow(context.Background(), detailDeps(detail, nil), ShowOptions{ID: "abc"}, true, 80)
	if !errors.Is(err, ErrNoteEncrypted) {
		t.Fatalf("加密笔记应当返回 ErrNoteEncrypted，得到 %v", err)
	}
	if res.Header == "" {
		t.Fatal("TTY 下加密笔记仍要给出头部元信息")
	}
	if !strings.Contains(res.Body, "加密") {
		t.Fatalf("正文位置应当是解密提示，得到 %q", res.Body)
	}
	if strings.Contains(res.Body, "先说结论") {
		t.Fatalf("加密笔记不该漏出正文：%q", res.Body)
	}

	// 非 TTY 下头部为空，提示行照给
	res, err = BuildShow(context.Background(), detailDeps(detail, nil), ShowOptions{ID: "abc"}, false, 80)
	if !errors.Is(err, ErrNoteEncrypted) {
		t.Fatalf("非 TTY 下也应当是 ErrNoteEncrypted，得到 %v", err)
	}
	if res.Header != "" {
		t.Fatalf("非 TTY 下头部应当为空，得到 %q", res.Header)
	}
	if !strings.Contains(res.Body, "加密") {
		t.Fatalf("非 TTY 下也要给提示行，得到 %q", res.Body)
	}
}

func TestBuildShowPropagatesDepsError(t *testing.T) {
	want := errors.New("找不到笔记")
	deps := ShowDeps{Note: func(ctx context.Context, id string) (NoteDetail, error) {
		return NoteDetail{}, want
	}}
	if _, err := BuildShow(context.Background(), deps, ShowOptions{ID: "abc"}, true, 80); !errors.Is(err, want) {
		t.Fatalf("deps 的错误应当原样返回，得到 %v", err)
	}
}
