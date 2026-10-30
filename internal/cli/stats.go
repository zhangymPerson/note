package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// EventCount 是某一类活动事件的次数；Kind 在分组阶段已被换成中文标签。
type EventCount struct {
	Kind  string
	Count int
}

// QueryCount 是某个查询词的使用次数。
type QueryCount struct {
	Query string
	Count int
}

// StatsData 是 FormatStats 的全部输入，由 cmd/note 从 store.GetStats 采集。
// 与 InfoData 同一套路：cli 只做渲染，不认识持久层类型。
type StatsData struct {
	DBPath        string
	DBExists      bool
	TotalEvents   int
	TotalSearches int
	ActiveDays    int
	EventsByKind  []EventCount // 原始 kind 计数，分组在 GroupEvents 里做
	TopQueries    []QueryCount
}

// eventGroups 是「事件构成」的展示口径，与 web/src/lib/stats.ts 的分组一一对应：
// note_purge 折进「删除笔记」，三个分类事件合成「分类维护」。
var eventGroups = []struct {
	label string
	kinds []string
}{
	{"查看笔记", []string{"note_view"}},
	{"新建笔记", []string{"note_create"}},
	{"编辑笔记", []string{"note_update"}},
	{"删除笔记", []string{"note_delete", "note_purge"}},
	{"恢复笔记", []string{"note_restore"}},
	{"查询", []string{"search"}},
	{"分类维护", []string{"category_create", "category_rename", "category_delete"}},
}

// GroupEvents 把按 kind 的计数折成展示分组：顺序固定，零事件的分组保留
// （「恢复笔记 0 次」本身就是有信息量的读数）。没进 eventGroups 的 kind
// 按名字排序附在末尾，新枚举上线时 CLI 不会悄悄漏掉计数。
func GroupEvents(counts []EventCount) []EventCount {
	byKind := make(map[string]int, len(counts))
	for _, item := range counts {
		byKind[item.Kind] += item.Count
	}

	groups := make([]EventCount, 0, len(eventGroups)+len(byKind))
	known := make(map[string]bool, len(byKind))
	for _, group := range eventGroups {
		total := 0
		for _, kind := range group.kinds {
			known[kind] = true
			total += byKind[kind]
		}
		groups = append(groups, EventCount{Kind: group.label, Count: total})
	}

	extra := make([]string, 0, len(byKind))
	for kind := range byKind {
		if !known[kind] {
			extra = append(extra, kind)
		}
	}
	sort.Strings(extra)
	for _, kind := range extra {
		groups = append(groups, EventCount{Kind: kind, Count: byKind[kind]})
	}
	return groups
}

// FormatStats 把统计渲染成对齐的分区文本（uniseg 计算 CJK 宽度，与 note info 同款）。
// 数据库不存在时只报「未创建」：统计是只读报告，不该顺手建库。
func FormatStats(d StatsData) string {
	if !d.DBExists {
		return "数据库 " + d.DBPath + "（未创建，还没有任何使用记录）\n"
	}

	var b strings.Builder
	b.WriteString("使用统计\n")
	writeStatRows(&b, [][2]string{
		{"累计事件", strconv.Itoa(d.TotalEvents) + " 次"},
		{"活跃天数", strconv.Itoa(d.ActiveDays) + " 天"},
		{"查询", strconv.Itoa(d.TotalSearches) + " 次"},
	})
	b.WriteString("\n")

	b.WriteString("事件构成\n")
	if d.TotalEvents == 0 {
		b.WriteString("  还没有记录到任何事件。\n")
	} else {
		groups := GroupEvents(d.EventsByKind)
		rows := make([][2]string, 0, len(groups))
		for _, group := range groups {
			rows = append(rows, [2]string{group.Kind, strconv.Itoa(group.Count) + " 次"})
		}
		writeStatRows(&b, rows)
	}
	b.WriteString("\n")

	b.WriteString("Top 查询词\n")
	if len(d.TopQueries) == 0 {
		b.WriteString("  还没有查询记录。\n")
	} else {
		rows := make([][2]string, 0, len(d.TopQueries))
		for _, item := range d.TopQueries {
			rows = append(rows, [2]string{item.Query, strconv.Itoa(item.Count) + " 次"})
		}
		writeStatRows(&b, rows)
	}
	return b.String()
}

// writeStatRows 按最宽标签对齐后逐行输出，两列都带两个空格缩进。
func writeStatRows(b *strings.Builder, rows [][2]string) {
	width := 0
	for _, row := range rows {
		if w := displayWidth(row[0]); w > width {
			width = w
		}
	}
	for _, row := range rows {
		fmt.Fprintf(b, "  %s  %s\n", padRight(row[0], width), row[1])
	}
}
