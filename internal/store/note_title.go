package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"
)

var (
	// ErrNoteTitleExists 表示同分类下已有同名笔记（撞唯一索引）。
	ErrNoteTitleExists = errors.New("note title already exists in this category")
	// ErrNoteTitleInTrash 表示同名笔记在回收站里：set/get/del 都不碰回收站，
	// 用户要先去网页恢复或彻底删除。
	ErrNoteTitleInTrash = errors.New("note title is in trash")
)

// categoryPredicate 返回按分类过滤的 SQL 片段与参数；nil 表示未分类。
// 未分类在库里是 category_id IS NULL（见 sql/0004 头注释），不能用 = 比较。
func categoryPredicate(categoryID *int64) (string, []any) {
	if categoryID == nil {
		return "category_id IS NULL", nil
	}
	return "category_id = ?", []any{*categoryID}
}

// titleLookupArgs 把标题与分类谓词的参数拼成一次查询的参数列表。
func titleLookupArgs(title string, categoryID *int64) []any {
	_, args := categoryPredicate(categoryID)
	return append([]any{title}, args...)
}

// FindNoteByTitle 按 (分类, 标题 NOCASE) 取那唯一一条；回收站里的也返回
// （DeletedAt 非空），由调用方决定怎么提示。找不到返回 ErrNoteNotFound。
//
// 标题列在 0001 里没有声明 COLLATE（列默认 BINARY），NOCASE 只写在唯一索引上，
// 因此这里必须显式 COLLATE NOCASE，否则查询口径与唯一约束会分叉。
func (s *Store) FindNoteByTitle(ctx context.Context, categoryID *int64, title string) (Note, error) {
	title, err := normalizeTitle(title)
	if err != nil {
		return Note{}, err
	}

	predicate, _ := categoryPredicate(categoryID)
	var id string
	err = s.db.QueryRowContext(ctx,
		`SELECT id FROM notes WHERE title = ? COLLATE NOCASE AND `+predicate+` LIMIT 1`,
		titleLookupArgs(title, categoryID)...,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Note{}, ErrNoteNotFound
	}
	if err != nil {
		return Note{}, fmt.Errorf("find note by title: %w", err)
	}
	return s.GetNote(ctx, id)
}

// UpsertNoteByTitle 是 note set 的落库：命中活的 → 覆盖正文（rev+1、updated_at 刷新）；
// 命中回收站 → ErrNoteTitleInTrash（不复活、不覆盖）；命中加密笔记 → ErrEncryptedImmutable；
// 都没有 → 新建。created 表示本次是新建。
func (s *Store) UpsertNoteByTitle(ctx context.Context, categoryID *int64, title, content string) (Note, bool, error) {
	title, err := normalizeTitle(title)
	if err != nil {
		return Note{}, false, err
	}
	if utf8.RuneCountInString(content) > maxContentRunes {
		return Note{}, false, fmt.Errorf("content exceeds %d characters: %w", maxContentRunes, ErrInvalidNote)
	}

	var (
		id      string
		created bool
	)
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		predicate, _ := categoryPredicate(categoryID)
		var (
			foundID   string
			encrypted int
			deletedAt sql.NullString
		)
		probeErr := tx.QueryRowContext(ctx,
			`SELECT id, is_encrypted, deleted_at FROM notes
             WHERE title = ? COLLATE NOCASE AND `+predicate+` LIMIT 1`,
			titleLookupArgs(title, categoryID)...,
		).Scan(&foundID, &encrypted, &deletedAt)

		if errors.Is(probeErr, sql.ErrNoRows) {
			id = NewNoteID()
			created = true
			now := nowUTC()
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO notes (id, title, content, is_encrypted, cipher, kdf_salt, kdf_params, category_id, kind, remark, rev, created_at, updated_at)
                 VALUES (?, ?, ?, 0, NULL, NULL, NULL, ?, 'note', '', 1, ?, ?)`,
				id, title, content, categoryID, now, now); err != nil {
				return fmt.Errorf("create note by title: %w", err)
			}
			return recordActivityTx(ctx, tx, ActivityNoteCreate)
		}
		if probeErr != nil {
			return fmt.Errorf("load note for upsert: %w", probeErr)
		}

		id = foundID
		if deletedAt.Valid {
			return ErrNoteTitleInTrash
		}
		if encrypted == 1 {
			return ErrEncryptedImmutable
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE notes SET content = ?, rev = rev + 1, updated_at = ? WHERE id = ?`,
			content, nowUTC(), foundID); err != nil {
			return fmt.Errorf("overwrite note by title: %w", err)
		}
		return recordActivityTx(ctx, tx, ActivityNoteUpdate)
	})
	if err != nil {
		return Note{}, false, err
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return Note{}, false, err
	}
	return note, created, nil
}

// SoftDeleteNoteByTitle 软删除那份标题对应的笔记：只写 deleted_at。
// 已在回收站 → ErrNoteTitleInTrash；不存在 → ErrNoteNotFound。
func (s *Store) SoftDeleteNoteByTitle(ctx context.Context, categoryID *int64, title string) (Note, error) {
	title, err := normalizeTitle(title)
	if err != nil {
		return Note{}, err
	}

	var id string
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		predicate, _ := categoryPredicate(categoryID)
		var (
			foundID   string
			deletedAt sql.NullString
		)
		probeErr := tx.QueryRowContext(ctx,
			`SELECT id, deleted_at FROM notes WHERE title = ? COLLATE NOCASE AND `+predicate+` LIMIT 1`,
			titleLookupArgs(title, categoryID)...,
		).Scan(&foundID, &deletedAt)
		if errors.Is(probeErr, sql.ErrNoRows) {
			return ErrNoteNotFound
		}
		if probeErr != nil {
			return fmt.Errorf("load note for delete: %w", probeErr)
		}

		id = foundID
		if deletedAt.Valid {
			return ErrNoteTitleInTrash
		}
		return softDeleteNoteTx(ctx, tx, foundID)
	})
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(ctx, id)
}

// SoftDeleteCategoryNotes 软删除分类下所有未删除笔记，返回删除条数；
// 每条被删笔记各记一次 note_delete（与逐条删除同口径）。
func (s *Store) SoftDeleteCategoryNotes(ctx context.Context, categoryID *int64) (int, error) {
	count := 0
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		predicate, args := categoryPredicate(categoryID)
		rows, err := tx.QueryContext(ctx,
			`SELECT id FROM notes WHERE deleted_at IS NULL AND `+predicate, args...)
		if err != nil {
			return fmt.Errorf("list notes to delete: %w", err)
		}

		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return fmt.Errorf("scan note to delete: %w", err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate notes to delete: %w", err)
		}
		rows.Close()

		now := nowUTC()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				`UPDATE notes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`, now, id); err != nil {
				return fmt.Errorf("soft delete note %s: %w", id, err)
			}
			if err := recordActivityTx(ctx, tx, ActivityNoteDelete); err != nil {
				return err
			}
		}
		count = len(ids)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ListNotesInCategory 按 updated_at 倒序列出分类下未删除的笔记。
// 未分类传 DefaultCategoryID：buildNoteQuery 会把它展开成 category_id IS NULL。
func (s *Store) ListNotesInCategory(ctx context.Context, categoryID int64) ([]NoteListItem, error) {
	q := buildNoteQuery(NoteFilter{CategoryID: categoryID})

	rows, err := s.db.QueryContext(ctx,
		listSelectSQL+` WHERE `+q.clause+` ORDER BY `+q.orderBy, q.args...)
	if err != nil {
		return nil, fmt.Errorf("list notes in category: %w", err)
	}
	defer rows.Close()

	return scanNoteItems(rows, false)
}
