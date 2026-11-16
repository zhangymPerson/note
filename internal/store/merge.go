package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// maxMergeNotes 单次合并最多接受的笔记条数（防病态请求）。
const maxMergeNotes = 100

// MergeNotes 按 ids 顺序把各明文笔记的正文直接拼接（不做去重/截断/格式化），
// 落成一条「未分类」（category_id NULL）的新笔记；原文一行不写。
// title 为空 → 自动生成 合并-<本地时间>（同秒撞名顺延下一秒重试 ≤5 次）；
// title 非空 → 单次创建，撞名原样返回。正文拼接后必须 ≤maxContentRunes 码点。
//
// 错误：ids <2、>maxMergeNotes、id 空或重复 → ErrInvalidNote；id 不存在 → ErrNoteNotFound；
// 选中笔记在回收站 → ErrNoteInTrash；选中笔记已加密 → ErrNoteEncrypted。
func (s *Store) MergeNotes(ctx context.Context, ids []string, title string) (Note, error) {
	return s.mergeNotesAt(ctx, ids, title, time.Now())
}

// mergeNotesAt 是 MergeNotes 的可注入时间版本（同 createQuickNoteAt 的测试缝）。
func (s *Store) mergeNotesAt(ctx context.Context, ids []string, title string, now time.Time) (Note, error) {
	title = strings.TrimSpace(title)
	if title != "" && utf8.RuneCountInString(title) > maxTitleRunes {
		return Note{}, fmt.Errorf("merge title exceeds %d characters: %w", maxTitleRunes, ErrInvalidNote)
	}
	if len(ids) < 2 {
		return Note{}, fmt.Errorf("merge requires at least 2 notes: %w", ErrInvalidNote)
	}
	if len(ids) > maxMergeNotes {
		return Note{}, fmt.Errorf("merge accepts at most %d notes: %w", maxMergeNotes, ErrInvalidNote)
	}

	seen := make(map[string]bool, len(ids))
	contents := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return Note{}, fmt.Errorf("duplicate or empty note id: %w", ErrInvalidNote)
		}
		seen[id] = true

		var content *string
		var encrypted int
		var deletedAt sql.NullString
		if err := s.db.QueryRowContext(ctx,
			`SELECT content, is_encrypted, deleted_at FROM notes WHERE id = ?`, id,
		).Scan(&content, &encrypted, &deletedAt); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return Note{}, ErrNoteNotFound
			}
			return Note{}, fmt.Errorf("load note %q for merge: %w", id, err)
		}
		if deletedAt.Valid {
			return Note{}, ErrNoteInTrash
		}
		if encrypted == 1 || content == nil {
			// 服务端读不到加密正文；明文笔记必有正文，content 为 nil 同样不可合并。
			return Note{}, ErrNoteEncrypted
		}
		contents = append(contents, *content)
	}

	joined := strings.Join(contents, "")
	if utf8.RuneCountInString(joined) > maxContentRunes {
		return Note{}, fmt.Errorf("merged content exceeds %d characters: %w", maxContentRunes, ErrInvalidNote)
	}

	if title != "" {
		return s.CreateNote(ctx, NoteInput{Title: title, Content: &joined})
	}
	return s.createWithAutoTitleAt(ctx, "合并", joined, nil, now)
}
