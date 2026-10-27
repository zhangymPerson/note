package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Category 是笔记分类及其引用笔记数。
type Category struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	NoteCount int    `json:"note_count"`
}

// 默认分类固定为 id=1「未分类」，不可改名、不可删除（见 sql/0004 头注释）。
const defaultCategoryID = 1

var (
	// ErrCategoryExists 表示分类名已被占用（NOCASE，大小写不敏感）。
	ErrCategoryExists = errors.New("category name already exists")
	// ErrReservedCategory 表示操作命中默认分类。
	ErrReservedCategory = errors.New("default category is reserved")
	// ErrNotFound 表示记录不存在。
	ErrNotFound = errors.New("record not found")
)

// ErrCategoryNotEmpty 表示分类下仍有笔记，删除被拒绝。
type ErrCategoryNotEmpty struct {
	Count int
}

func (e ErrCategoryNotEmpty) Error() string {
	return fmt.Sprintf("分类下仍有 %d 条笔记", e.Count)
}

// ListCategories 按 id 升序返回全部分类，含每个分类的笔记数。
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.name,
		        COUNT(n.id) AS note_count
         FROM categories c
         LEFT JOIN notes n ON n.category_id = c.id
         GROUP BY c.id, c.name
         ORDER BY c.id`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	cats := []Category{}
	for rows.Next() {
		var cat Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.NoteCount); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		cats = append(cats, cat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return cats, nil
}

// CreateCategory 新建分类；name 由调用方去除空白，长度校验在 web 层。
func (s *Store) CreateCategory(ctx context.Context, name string) (Category, error) {
	name = strings.TrimSpace(name)

	var id int64
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		// NOCASE 列上的等值比较天然忽略大小写，先显式查重再依赖 UNIQUE 兜底
		var existing int64
		dupErr := tx.QueryRowContext(ctx,
			`SELECT id FROM categories WHERE name = ?`, name,
		).Scan(&existing)
		if !errors.Is(dupErr, sql.ErrNoRows) {
			if dupErr != nil {
				return dupErr
			}
			return ErrCategoryExists
		}

		res, err := tx.ExecContext(ctx,
			`INSERT INTO categories (name, created_at) VALUES (?, ?)`,
			name, nowUTC())
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		if errors.Is(err, ErrCategoryExists) || isUniqueConstraint(err) {
			return Category{}, ErrCategoryExists
		}
		return Category{}, fmt.Errorf("create category: %w", err)
	}

	return Category{ID: id, Name: name, NoteCount: 0}, nil
}

// RenameCategory 改名；默认分类与不存在的分类分别返回守卫错误。
func (s *Store) RenameCategory(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("rename category: %w", ErrNotFound)
	}
	if id == defaultCategoryID {
		return ErrReservedCategory
	}

	return s.inTx(ctx, func(tx *sql.Tx) error {
		found, err := categoryExists(ctx, tx, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}

		var dupID int64
		dupErr := tx.QueryRowContext(ctx,
			`SELECT id FROM categories WHERE name = ? AND id <> ?`, name, id,
		).Scan(&dupID)
		if !errors.Is(dupErr, sql.ErrNoRows) {
			if dupErr != nil {
				return dupErr
			}
			return ErrCategoryExists
		}

		_, err = tx.ExecContext(ctx, `UPDATE categories SET name = ? WHERE id = ?`, name, id)
		if isUniqueConstraint(err) {
			return ErrCategoryExists
		}
		return err
	})
}

// DeleteCategory 删除分类；默认分类不可删，仍有笔记时返回 ErrCategoryNotEmpty。
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	if id == defaultCategoryID {
		return ErrReservedCategory
	}

	return s.inTx(ctx, func(tx *sql.Tx) error {
		found, err := categoryExists(ctx, tx, id)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}

		// 参数以 VALUES(?) 子查询传入，避免 COUNT(*) 自引用被删除行的歧义
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM notes n
             WHERE n.category_id IN (SELECT id FROM categories WHERE id = ?)`,
			id,
		).Scan(&count); err != nil {
			return fmt.Errorf("count notes in category: %w", err)
		}
		if count > 0 {
			return ErrCategoryNotEmpty{Count: count}
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes SET category_id = NULL WHERE category_id = ?`, id,
		); err != nil {
			return fmt.Errorf("detach notes from category: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete category: %w", err)
		}
		return nil
	})
}

func categoryExists(ctx context.Context, tx *sql.Tx, id int64) (bool, error) {
	var found int64
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM categories WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query category %d: %w", id, err)
	}
	return true, nil
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}
