package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"note/internal/cli"
	"note/internal/store"
)

// testKDFParams 是加密笔记入库时必须带的 KDF 参数（与 Web 端写入的形状一致）。
const testKDFParams = `{"algo":"pbkdf2","prf":"HMAC-SHA256","iter":150000,"keylen":32}`

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "notes.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOpenNotePrintsRenderedBody(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	body := "# 小标题\n\n先说结论，再说细节。\n"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "会议纪要", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var buf bytes.Buffer
	if err := openNote(ctx, st, note.ID, cli.ShowOptions{Style: "notty"}, false, &buf); err != nil {
		t.Fatalf("openNote: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "先说结论") {
		t.Fatalf("输出应当含正文：%q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("非 TTY 下不该有 ANSI：%q", out)
	}
	if strings.Contains(out, "会议纪要") {
		t.Fatalf("非 TTY 下不该打印头部（标题只出现在头部）：%q", out)
	}
}

func TestOpenNoteEncryptedMapsToExitCode4(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.SetMasterPassword(ctx, bytes.Repeat([]byte{1}, 16), "母亲的名字？"); err != nil {
		t.Fatalf("set master password: %v", err)
	}
	note, err := st.CreateNote(ctx, store.NoteInput{
		Title:     "加密的",
		CipherB64: base64.StdEncoding.EncodeToString(make([]byte, 29)),
		KDFParams: testKDFParams,
	})
	if err != nil {
		t.Fatalf("create encrypted note: %v", err)
	}

	var buf bytes.Buffer
	err = openNote(ctx, st, note.ID, cli.ShowOptions{}, false, &buf)
	var code exitError
	if !errors.As(err, &code) {
		t.Fatalf("加密笔记应当映射成 exitError，得到 %v", err)
	}
	if int(code) != 4 {
		t.Fatalf("加密笔记的退出码应当是 4，得到 %d", int(code))
	}
	if !strings.Contains(buf.String(), "加密") {
		t.Fatalf("应当打印解密提示，得到 %q", buf.String())
	}
}

func TestOpenNoteMissingNoteIsError1(t *testing.T) {
	st := openTestStore(t)

	var buf bytes.Buffer
	err := openNote(context.Background(), st, "不存在的-id", cli.ShowOptions{}, false, &buf)
	if err == nil {
		t.Fatal("找不到笔记应当报错")
	}
	var code exitError
	if errors.As(err, &code) {
		t.Fatalf("找不到笔记不是「加密笔记」，不该走退出码 4：%v", err)
	}
	if !strings.Contains(err.Error(), "找不到笔记") {
		t.Fatalf("错误信息应当含「找不到笔记」，得到 %v", err)
	}
}

func TestOpenNoteTTYPrintsHeader(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	body := "先说结论。\n"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "会议纪要", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var buf bytes.Buffer
	if err := openNote(ctx, st, note.ID, cli.ShowOptions{Style: "notty"}, true, &buf); err != nil {
		t.Fatalf("openNote: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"会议纪要", note.ID, "先说结论"} {
		if !strings.Contains(out, want) {
			t.Fatalf("TTY 输出缺少 %q：%q", want, out)
		}
	}
}

func TestOpenNoteMissingNoteWritesNothing(t *testing.T) {
	st := openTestStore(t)

	var buf bytes.Buffer
	if err := openNote(context.Background(), st, "不存在的-id", cli.ShowOptions{}, false, &buf); err == nil {
		t.Fatal("找不到笔记应当报错")
	}
	if buf.Len() != 0 {
		t.Fatalf("失败时不该往 stdout 写任何字节，得到 %q", buf.String())
	}
}

func TestPrintShowKeepsRawBytesExact(t *testing.T) {
	raw := "# 标题\n\n正文\n"
	var buf bytes.Buffer
	if err := printShow(cli.ShowResult{Body: raw}, &buf); err != nil {
		t.Fatalf("printShow: %v", err)
	}
	if buf.String() != raw {
		t.Fatalf("--raw 应当逐字节原样输出：%q != %q", buf.String(), raw)
	}

	buf.Reset()
	if err := printShow(cli.ShowResult{}, &buf); err != nil {
		t.Fatalf("printShow: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("空结果不该写任何字节，得到 %q", buf.String())
	}
}

// stubClipboard 换掉真正的剪贴板写入，返回一个记录被复制内容的指针。
func stubClipboard(t *testing.T) *[]string {
	t.Helper()
	copied := []string{}
	previous := copyToClipboard
	t.Cleanup(func() { copyToClipboard = previous })
	copyToClipboard = func(text string) error {
		copied = append(copied, text)
		return nil
	}
	return &copied
}

// note show 复制的是库里的原始 Markdown，不是渲染后的终端文本。
func TestOpenNoteCopiesRawMarkdown(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	body := "# 小标题\n\n先说结论。\n"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "会议纪要", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	copied := stubClipboard(t)
	var out bytes.Buffer
	if err := openNote(ctx, st, note.ID, cli.ShowOptions{Style: "notty"}, true, &out); err != nil {
		t.Fatalf("openNote: %v", err)
	}

	if len(*copied) != 1 {
		t.Fatalf("应当只复制一次，实际 %d 次：%v", len(*copied), *copied)
	}
	if (*copied)[0] != body {
		t.Fatalf("复制的应当是原始 Markdown：\n got %q\nwant %q", (*copied)[0], body)
	}
	// 打印出去的仍是渲染结果（带标题层级被消化掉的纯文本）
	if out.String() == body {
		t.Fatalf("stdout 应当是渲染后的文本，而不是原文：%q", out.String())
	}
}

func TestOpenNoteDoesNotCopyEncryptedOrMissing(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if _, err := st.SetMasterPassword(ctx, bytes.Repeat([]byte{1}, 16), "问题"); err != nil {
		t.Fatalf("set master password: %v", err)
	}
	note, err := st.CreateNote(ctx, store.NoteInput{
		Title:     "加密的",
		CipherB64: base64.StdEncoding.EncodeToString(make([]byte, 29)),
		KDFParams: testKDFParams,
	})
	if err != nil {
		t.Fatalf("create encrypted note: %v", err)
	}

	copied := stubClipboard(t)
	var out bytes.Buffer
	if err := openNote(ctx, st, note.ID, cli.ShowOptions{}, false, &out); err == nil {
		t.Fatal("加密笔记应当返回 exitError(4)")
	}
	if err := openNote(ctx, st, "不存在的-id", cli.ShowOptions{}, true, &out); err == nil {
		t.Fatal("找不到的笔记应当报错")
	}
	if len(*copied) != 0 {
		t.Fatalf("看不成的时候不该碰剪贴板：%v", *copied)
	}
}

// 管道/脚本里（stdout 不是终端）不改用户的剪贴板：那是个看不见的副作用。
func TestOpenNoteSkipsClipboardWithoutTerminal(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	body := "正文\n"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "标题", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	copied := stubClipboard(t)
	var out bytes.Buffer
	if err := openNote(ctx, st, note.ID, cli.ShowOptions{Raw: true}, false, &out); err != nil {
		t.Fatalf("openNote: %v", err)
	}
	if len(*copied) != 0 {
		t.Fatalf("非终端下不该复制：%v", *copied)
	}
	if out.String() != body {
		t.Fatalf("--raw 应当逐字节输出原文，得到 %q", out.String())
	}
}

// 没有 id 又没有终端时不能挂着等输入，也不能只把列表当文本吐出来。
func TestShowPickedRequiresTerminal(t *testing.T) {
	st := openTestStore(t)
	err := showPicked(context.Background(), st, cli.ShowOptions{}, false)
	if err == nil {
		t.Fatal("非终端下 note show 无 id 应当报错")
	}
	if !strings.Contains(err.Error(), "note show 需要一个笔记 id") {
		t.Fatalf("错误信息应当指出缺 id，得到 %v", err)
	}
}
