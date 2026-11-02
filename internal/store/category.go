package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Category 是笔记分类及其引用笔记数。
// Reserved 表示内置固定分类（「未分类」与「速记」）：不可改名、不可删除。
type Category struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	NoteCount int    `json:"note_count"`
	Reserved  bool   `json:"reserved"`
}

// 默认分类固定为 id=1「未分类」、id=2「速记」，均不可改名、不可删除（见 sql/0004、sql/0007 头注释）。
const (
	defaultCategoryID = 1
	quickCategoryID   = 2
)

// DefaultCategoryID 是内置「未分类」分类的 id。Web 端把它落库成 category_id = NULL，
// CLI 的 note add 也用同一判断，避免两边对"未分类"的理解分叉。
const DefaultCategoryID = defaultCategoryID

// QuickCategoryID / QuickCategoryName 是内置「速记」分类的 id 与名字：
// CLI 的 note set/get/del 不给分类时默认落在这里（cmd/note 会断言两处常量一致）。
const (
	QuickCategoryID   = quickCategoryID
	QuickCategoryName = "速记"
)

// IsReservedCategory 判断是不是内置固定分类（不可改名、不可删除）。
func IsReservedCategory(id int64) bool {
	return id == defaultCategoryID || id == quickCategoryID
}

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
// 默认分类的笔记数包含 category_id IS NULL 的笔记——「未分类」就是这么存的。
// 计数只算未删除的笔记：回收站里的不算，否则树上的数字与列表条数对不上。
func (s *Store) ListCategories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.name,
		        COUNT(n.id) AS note_count
         FROM categories c
         LEFT JOIN notes n ON (n.category_id = c.id
                               OR (c.id = ? AND n.category_id IS NULL))
                              AND n.deleted_at IS NULL
         GROUP BY c.id, c.name
         ORDER BY c.id`, defaultCategoryID)
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
		cat.Reserved = IsReservedCategory(cat.ID)
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
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return recordActivityTx(ctx, tx, ActivityCategoryCreate)
	})
	if err != nil {
		if errors.Is(err, ErrCategoryExists) || isUniqueConstraint(err) {
			return Category{}, ErrCategoryExists
		}
		return Category{}, fmt.Errorf("create category: %w", err)
	}

	return Category{ID: id, Name: name, NoteCount: 0}, nil
}

// RenameCategory 改名；内置固定分类与不存在的分类分别返回守卫错误。
func (s *Store) RenameCategory(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("rename category: %w", ErrNotFound)
	}
	if IsReservedCategory(id) {
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
		if err != nil {
			return err
		}
		return recordActivityTx(ctx, tx, ActivityCategoryRename)
	})
}

// DeleteCategory 删除分类；内置固定分类不可删，仍有笔记时返回 ErrCategoryNotEmpty。
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	if IsReservedCategory(id) {
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

		// 参数以 VALUES(?) 子查询传入，避免 COUNT(*) 自引用被删除行的歧义。
		// 只数未删除的笔记：分类里只剩回收站笔记时允许删除（那些笔记会被
		// 下面的 UPDATE 摘成分未分类，恢复后落在「未分类」）。
		var count int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM notes n
             WHERE n.deleted_at IS NULL
               AND n.category_id IN (SELECT id FROM categories WHERE id = ?)`,
			id,
		).Scan(&count); err != nil {
			return fmt.Errorf("count notes in category: %w", err)
		}
		if count > 0 {
			return ErrCategoryNotEmpty{Count: count}
		}

		// 无需过滤 deleted_at：外键不区分软删除，回收站里的笔记也得先摘掉分类
		if _, err := tx.ExecContext(ctx,
			`UPDATE notes SET category_id = NULL WHERE category_id = ?`, id,
		); err != nil {
			return fmt.Errorf("detach notes from category: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id); err != nil {
			return fmt.Errorf("delete category: %w", err)
		}
		return recordActivityTx(ctx, tx, ActivityCategoryDelete)
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
