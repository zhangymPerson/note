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

func TestRenderNotePrintsRenderedBody(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	body := "# 小标题\n\n先说结论，再说细节。\n"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "会议纪要", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var buf bytes.Buffer
	if err := renderNote(ctx, st, note.ID, cli.ShowOptions{Style: "notty"}, false, &buf); err != nil {
		t.Fatalf("renderNote: %v", err)
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

func TestRenderNoteEncryptedMapsToExitCode4(t *testing.T) {
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
	err = renderNote(ctx, st, note.ID, cli.ShowOptions{}, false, &buf)
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

func TestRenderNoteMissingNoteIsError1(t *testing.T) {
	st := openTestStore(t)

	var buf bytes.Buffer
	err := renderNote(context.Background(), st, "不存在的-id", cli.ShowOptions{}, false, &buf)
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

func TestRenderNoteTTYPrintsHeader(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	body := "先说结论。\n"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "会议纪要", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}

	var buf bytes.Buffer
	if err := renderNote(ctx, st, note.ID, cli.ShowOptions{Style: "notty"}, true, &buf); err != nil {
		t.Fatalf("renderNote: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"会议纪要", note.ID, "先说结论"} {
		if !strings.Contains(out, want) {
			t.Fatalf("TTY 输出缺少 %q：%q", want, out)
		}
	}
}
