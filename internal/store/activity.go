package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

const timeLayoutUTC = "2006-01-02T15:04:05Z"

// 活动类型：与 sql/0005_activity_events.sql 的 CHECK 枚举同源维护。
// 事件只落 kind 与 created_at（搜索额外落查询词），不记录标题、正文或分类名。
const (
	ActivitySearch         = "search"
	ActivityNoteView       = "note_view"
	ActivityNoteCreate     = "note_create"
	ActivityNoteUpdate     = "note_update"
	ActivityNoteDelete     = "note_delete"
	ActivityNoteRestore    = "note_restore" // 预留给软删除恢复，当前无写入点
	ActivityNotePurge      = "note_purge"   // 预留给回收站彻底删除，当前无写入点
	ActivityCategoryCreate = "category_create"
	ActivityCategoryRename = "category_rename"
	ActivityCategoryDelete = "category_delete"
)

// NoteHit 是搜索命中的笔记摘要，不包含正文。
type NoteHit struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UpdatedAt string `json:"updated_at"`
}

// QueryCount 是某个查询词的使用次数。
type QueryCount struct {
	Query string `json:"query"`
	Count int    `json:"count"`
}

// KindCount 是某一类活动事件的次数，Kind 对应 Activity* 常量。
type KindCount struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// DayCount 是某一天的活动次数，Date 形如 2026-10-26。
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// Stats 是首页使用统计的全量数据。
// TotalEvents / ActiveDays / Days 覆盖全部事件；TotalSearches 与 TopQueries
// 口径仍限 kind='search'，让「查询」这件事单独可读。
type Stats struct {
	TotalEvents   int          `json:"total_events"`
	TotalSearches int          `json:"total_searches"`
	ActiveDays    int          `json:"active_days"`
	EventsByKind  []KindCount  `json:"events_by_kind"`
	TopQueries    []QueryCount `json:"top_queries"`
	Days          []DayCount   `json:"days"`
}

// SearchNotes 按标题与明文正文做子串模糊查询，并记录一次查询活动。
func (s *Store) SearchNotes(ctx context.Context, query string) ([]NoteHit, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return []NoteHit{}, nil
	}

	if err := s.RecordSearch(ctx, q); err != nil {
		return nil, err
	}

	// LIKE 通配符需要转义，防止用户输入 % 或 _ 扩大匹配范围
	escaped := escapeLike(q)
	like := "%" + escaped + "%"

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, title, updated_at FROM notes
         WHERE title LIKE ? ESCAPE '\' OR content LIKE ? ESCAPE '\'
         ORDER BY updated_at DESC LIMIT 20`,
		like, like)
	if err != nil {
		return nil, fmt.Errorf("search notes: %w", err)
	}
	defer rows.Close()

	hits := []NoteHit{}
	for rows.Next() {
		var hit NoteHit
		if err := rows.Scan(&hit.ID, &hit.Title, &hit.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan note hit: %w", err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate note hits: %w", err)
	}
	return hits, nil
}

// activityWriter 同时被 *sql.DB 与 *sql.Tx 满足，写事件因此能挂进业务事务。
type activityWriter interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const insertActivitySQL = `INSERT INTO activities (kind, query, created_at) VALUES (?, ?, ?)`

// insertActivity 写入一条活动事件；query 对非搜索事件传空串。
func insertActivity(ctx context.Context, w activityWriter, kind, query, at string) error {
	if _, err := w.ExecContext(ctx, insertActivitySQL, kind, query, at); err != nil {
		return fmt.Errorf("record %s activity: %w", kind, err)
	}
	return nil
}

// recordActivityTx 在业务事务内记录一条不带查询词的事件，成功与埋点同生共死。
func recordActivityTx(ctx context.Context, tx *sql.Tx, kind string) error {
	return insertActivity(ctx, tx, kind, "", nowUTC())
}

// RecordSearch 写入一条查询活动。
func (s *Store) RecordSearch(ctx context.Context, query string) error {
	return insertActivity(ctx, s.db, ActivitySearch, query, nowUTC())
}

// GetStats 汇总使用统计；days 覆盖最近 days 天（含今天），无活动的日期省略。
func (s *Store) GetStats(ctx context.Context, lookbackDays int) (Stats, error) {
	stats := Stats{
		EventsByKind: []KindCount{},
		TopQueries:   []QueryCount{},
		Days:         []DayCount{},
	}

	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN kind = 'search' THEN 1 ELSE 0 END), 0),
		        COUNT(DISTINCT substr(created_at, 1, 10))
         FROM activities`,
	).Scan(&stats.TotalEvents, &stats.TotalSearches, &stats.ActiveDays)
	if err != nil {
		return stats, fmt.Errorf("query activity totals: %w", err)
	}

	kindRows, err := s.db.QueryContext(ctx,
		`SELECT kind, COUNT(*) AS c FROM activities GROUP BY kind ORDER BY c DESC, kind`,
	)
	if err != nil {
		return stats, fmt.Errorf("query activity kinds: %w", err)
	}
	for kindRows.Next() {
		var item KindCount
		if err := kindRows.Scan(&item.Kind, &item.Count); err != nil {
			kindRows.Close()
			return stats, fmt.Errorf("scan activity kind: %w", err)
		}
		stats.EventsByKind = append(stats.EventsByKind, item)
	}
	if err := kindRows.Err(); err != nil {
		kindRows.Close()
		return stats, fmt.Errorf("iterate activity kinds: %w", err)
	}
	kindRows.Close()

	rows, err := s.db.QueryContext(ctx,
		`SELECT query, COUNT(*) AS c FROM activities
         WHERE kind = 'search' AND query <> ''
         GROUP BY query ORDER BY c DESC, MAX(created_at) DESC LIMIT 10`,
	)
	if err != nil {
		return stats, fmt.Errorf("query top queries: %w", err)
	}
	for rows.Next() {
		var item QueryCount
		if err := rows.Scan(&item.Query, &item.Count); err != nil {
			rows.Close()
			return stats, fmt.Errorf("scan top query: %w", err)
		}
		stats.TopQueries = append(stats.TopQueries, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return stats, fmt.Errorf("iterate top queries: %w", err)
	}
	rows.Close()

	since := time.Now().UTC().AddDate(0, 0, -(lookbackDays - 1)).Format("2006-01-02")
	dayRows, err := s.db.QueryContext(ctx,
		`SELECT substr(created_at, 1, 10) AS day, COUNT(*) FROM activities
         WHERE substr(created_at, 1, 10) >= ?
         GROUP BY day ORDER BY day`,
		since)
	if err != nil {
		return stats, fmt.Errorf("query day counts: %w", err)
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var day DayCount
		if err := dayRows.Scan(&day.Date, &day.Count); err != nil {
			return stats, fmt.Errorf("scan day count: %w", err)
		}
		stats.Days = append(stats.Days, day)
	}
	if err := dayRows.Err(); err != nil {
		return stats, fmt.Errorf("iterate day counts: %w", err)
	}

	return stats, nil
}
