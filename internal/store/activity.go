package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const timeLayoutUTC = "2006-01-02T15:04:05Z"

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

// DayCount 是某一天的活动次数，Date 形如 2026-10-26。
type DayCount struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

// Stats 是首页使用统计的全量数据。
type Stats struct {
	TotalSearches int          `json:"total_searches"`
	ActiveDays    int          `json:"active_days"`
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
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
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

// RecordSearch 写入一条查询活动。
func (s *Store) RecordSearch(ctx context.Context, query string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO activities (kind, query, created_at) VALUES ('search', ?, ?)`,
		query, time.Now().UTC().Format(timeLayoutUTC))
	if err != nil {
		return fmt.Errorf("record search activity: %w", err)
	}
	return nil
}

// GetStats 汇总使用统计；days 覆盖最近 days 天（含今天），无活动的日期省略。
func (s *Store) GetStats(ctx context.Context, lookbackDays int) (Stats, error) {
	stats := Stats{TopQueries: []QueryCount{}, Days: []DayCount{}}

	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COUNT(DISTINCT substr(created_at, 1, 10)) FROM activities WHERE kind = 'search'`,
	).Scan(&stats.TotalSearches, &stats.ActiveDays)
	if err != nil {
		return stats, fmt.Errorf("query activity totals: %w", err)
	}

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
         WHERE kind = 'search' AND substr(created_at, 1, 10) >= ?
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
