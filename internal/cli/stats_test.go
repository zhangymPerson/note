package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestGroupEventsMergesKindsLikeFrontend(t *testing.T) {
	groups := GroupEvents([]EventCount{
		{Kind: "note_purge", Count: 2},
		{Kind: "note_delete", Count: 3},
		{Kind: "category_delete", Count: 1},
		{Kind: "category_rename", Count: 1},
		{Kind: "category_create", Count: 2},
		{Kind: "search", Count: 4},
		{Kind: "note_view", Count: 16},
		{Kind: "mystery", Count: 5},
	})

	want := []EventCount{
		{Kind: "查看笔记", Count: 16},
		{Kind: "新建笔记", Count: 0},
		{Kind: "编辑笔记", Count: 0},
		{Kind: "删除笔记", Count: 5},
		{Kind: "恢复笔记", Count: 0},
		{Kind: "查询", Count: 4},
		{Kind: "分类维护", Count: 4},
		{Kind: "mystery", Count: 5},
	}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("groups = %+v, want %+v", groups, want)
	}
}

func TestGroupEventsKeepsZeroGroupsOnEmptyLedger(t *testing.T) {
	groups := GroupEvents(nil)
	if len(groups) != len(eventGroups) {
		t.Fatalf("expected %d groups, got %+v", len(eventGroups), groups)
	}
	for _, group := range groups {
		if group.Count != 0 {
			t.Fatalf("expected every group zero, got %+v", groups)
		}
	}
}

func TestFormatStatsRendersAlignedSections(t *testing.T) {
	data := StatsData{
		DBPath:        "notes.db",
		DBExists:      true,
		TotalEvents:   46,
		TotalSearches: 4,
		ActiveDays:    12,
		EventsByKind: []EventCount{
			{Kind: "note_view", Count: 16},
			{Kind: "note_create", Count: 5},
			{Kind: "note_update", Count: 1},
			{Kind: "note_delete", Count: 6},
			{Kind: "note_restore", Count: 3},
			{Kind: "search", Count: 4},
			{Kind: "category_create", Count: 5},
		},
		TopQueries: []QueryCount{{Query: "周报", Count: 3}},
	}

	want := "使用统计\n" +
		"  累计事件  46 次\n" +
		"  活跃天数  12 天\n" +
		"  查询      4 次\n" +
		"\n" +
		"事件构成\n" +
		"  查看笔记  16 次\n" +
		"  新建笔记  5 次\n" +
		"  编辑笔记  1 次\n" +
		"  删除笔记  6 次\n" +
		"  恢复笔记  3 次\n" +
		"  查询      4 次\n" +
		"  分类维护  5 次\n" +
		"\n" +
		"Top 查询词\n" +
		"  周报  3 次\n"
	if got := FormatStats(data); got != want {
		t.Fatalf("FormatStats mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFormatStatsOnEmptyLedger(t *testing.T) {
	got := FormatStats(StatsData{DBPath: "notes.db", DBExists: true})
	for _, want := range []string{"累计事件", "事件构成", "还没有记录到任何事件", "还没有查询记录"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output should mention %q:\n%s", want, got)
		}
	}
	// 空账簿不列全零分组：那一屏没有信息量
	if strings.Contains(got, "查看笔记") {
		t.Fatalf("empty ledger should not list zero groups:\n%s", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Fatalf("output should not end with a blank line: %q", got)
	}
}

func TestFormatStatsMissingDatabase(t *testing.T) {
	got := FormatStats(StatsData{DBPath: "C:/data/notes.db"})
	if !strings.Contains(got, "C:/data/notes.db") || !strings.Contains(got, "未创建") {
		t.Fatalf("missing database should say 未创建: %q", got)
	}
	if strings.Contains(got, "事件构成") {
		t.Fatalf("missing database has no ledger to render: %q", got)
	}
}
