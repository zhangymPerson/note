package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"note/internal/cli"
	"note/internal/store"
)

// 统计口径直接读 activities 表：CLI 的读写（CreateNote / ViewNote / 搜索）都要算进来，
// 这正是「埋点在 store 层而不是接口层」的意义。
func TestCollectStatsReadsActivityLedger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.db")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ctx := context.Background()
	body := "正文"
	note, err := st.CreateNote(ctx, store.NoteInput{Title: "会议纪要", Content: &body})
	if err != nil {
		t.Fatalf("create note: %v", err)
	}
	if _, err := st.ViewNote(ctx, note.ID); err != nil {
		t.Fatalf("view note: %v", err)
	}
	if _, err := st.ListNotes(ctx, store.NoteFilter{Q: "会议", Page: 1}); err != nil {
		t.Fatalf("search notes: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	data, err := collectStats(path)
	if err != nil {
		t.Fatalf("collectStats: %v", err)
	}
	if !data.DBExists {
		t.Fatal("数据库存在时 DBExists 应为 true")
	}
	if data.TotalEvents != 3 || data.TotalSearches != 1 || data.ActiveDays != 1 {
		t.Fatalf("统计不对：%+v", data)
	}

	want := map[string]int{
		store.ActivityNoteCreate: 1,
		store.ActivityNoteView:   1,
		store.ActivitySearch:     1,
	}
	for _, item := range data.EventsByKind {
		if count, ok := want[item.Kind]; ok && item.Count != count {
			t.Fatalf("%s 事件数 %d，应为 %d（%+v）", item.Kind, item.Count, count, data.EventsByKind)
		}
		delete(want, item.Kind)
	}
	if len(want) != 0 {
		t.Fatalf("缺少事件：%+v（得到 %+v）", want, data.EventsByKind)
	}
	if len(data.TopQueries) != 1 || data.TopQueries[0].Query != "会议" {
		t.Fatalf("Top 查询词不对：%+v", data.TopQueries)
	}

	out := cli.FormatStats(data)
	for _, text := range []string{"事件构成", "查看笔记", "新建笔记", "Top 查询词"} {
		if !strings.Contains(out, text) {
			t.Fatalf("渲染结果缺少 %q：\n%s", text, out)
		}
	}
}

// 只读报告：数据库不存在时不建库、不创建目录，只报「未创建」。
func TestCollectStatsDoesNotCreateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "notes.db")

	data, err := collectStats(path)
	if err != nil {
		t.Fatalf("collectStats: %v", err)
	}
	if data.DBExists || data.TotalEvents != 0 {
		t.Fatalf("数据库不存在时不该有统计：%+v", data)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("不该创建数据目录：%v", err)
	}
	if out := cli.FormatStats(data); !strings.Contains(out, "未创建") {
		t.Fatalf("渲染结果应提示未创建：%q", out)
	}
}
