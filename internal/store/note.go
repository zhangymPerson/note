package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// 笔记层的结构与容量守卫（与 web 层校验同值，服务端永不信任单一入口）。
const (
	maxTitleRunes   = 200
	maxContentRunes = 2000
	minCipherBytes  = 29 // nonce(12) + 空明文 + tag(16)
	maxCipherBytes  = 65536
	summaryRunes    = 140
	notesPerPage    = 20
)

var (
	// ErrNoteNotFound 表示笔记 id 不存在。
	ErrNoteNotFound = errors.New("note not found")
	// ErrNoteNotInTrash 表示操作要求笔记在回收站里，但它其实没被软删除（或已被彻底删除）。
	ErrNoteNotInTrash = errors.New("note is not in trash")
	// ErrEncryptedImmutable 表示试图把加密笔记改回明文（0001 约定加密后不可取消）。
	ErrEncryptedImmutable = errors.New("encrypted note cannot be converted back to plaintext")
	// ErrNotEncrypted 表示对明文笔记调用只适用于加密笔记的操作。
	ErrNotEncrypted = errors.New("note is not encrypted")
	// ErrInvalidNote 表示明文/密文结构不合法。
	ErrInvalidNote = errors.New("invalid note payload")
)

// Note 是一条笔记的全量记录；加密笔记只带密文，正文列恒为 nil。
// DeletedAt 非空表示这条笔记在回收站里（0006 软删除）。
type Note struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Encrypted    bool    `json:"encrypted"`
	CategoryID   *int64  `json:"category_id"`
	CategoryName string  `json:"category_name,omitempty"`
	Kind         string  `json:"kind"`
	Remark       string  `json:"remark"`
	Rev          int     `json:"rev"`
	Content      *string `json:"content"`
	CipherB64    string  `json:"cipher_b64,omitempty"`
	KDFParams    *string `json:"kdf_params,omitempty"`
	DeletedAt    *string `json:"deleted_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// NoteInput 是新建/更新笔记的入参。ID 非空时由客户端生成——加密笔记的 AAD
// 绑定最终 id，必须在浏览器加密前确定（见设计文档 §2）。
type NoteInput struct {
	ID         string
	Title      string
	Content    *string
	CipherB64  string
	KDFParams  string
	CategoryID *int64
}

// NoteListItem 是列表/搜索结果里的一行，正文只是明文摘要。
type NoteListItem struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Encrypted    bool    `json:"encrypted"`
	Content      *string `json:"content,omitempty"`
	CategoryID   *int64  `json:"category_id"`
	CategoryName string  `json:"category_name,omitempty"`
	DeletedAt    *string `json:"deleted_at,omitempty"`
	UpdatedAt    string  `json:"updated_at"`
}

// NoteFilter 是列表与搜索的入参。Deleted=true 时只返回回收站里的笔记。
type NoteFilter struct {
	Q          string
	CategoryID int64
	Page       int
	Deleted    bool
}

// NotePage 是分页结果。
type NotePage struct {
	Items   []NoteListItem `json:"items"`
	Total   int            `json:"total"`
	Page    int            `json:"page"`
	PerPage int            `json:"per_page"`
}

// NewNoteID 生成 UUID v4（8-4-4-4-12），不引入第三方依赖。
func NewNoteID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("generate note id: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// CreateNote 新建笔记；kind 默认 'note'，rev 与时间戳由本层初始化。
func (s *Store) CreateNote(ctx context.Context, in NoteInput) (Note, error) {
	title, err := normalizeTitle(in.Title)
	if err != nil {
		return Note{}, err
	}
	body, cipher, params, err := normalizeBody(in)
	if err != nil {
		return Note{}, err
	}
	kdfSalt, err := s.kdfSaltFor(ctx, cipher != nil)
	if err != nil {
		return Note{}, err
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = NewNoteID()
	}
	now := nowUTC()
	encrypted := cipher != nil

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO notes (id, title, content, is_encrypted, cipher, kdf_salt, kdf_params, category_id, kind, remark, rev, created_at, updated_at)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'note', '', 1, ?, ?)`,
			id, title, body, boolInt(encrypted), cipher, kdfSalt, params, in.CategoryID, now, now); err != nil {
			return fmt.Errorf("create note: %w", err)
		}
		return recordActivityTx(ctx, tx, ActivityNoteCreate)
	})
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(ctx, id)
}

// GetNote 读取单条笔记，含分类名。回收站里的笔记照常返回（DeletedAt 非空），
// 详情页靠这个字段决定展示「恢复/彻底删除」而不是「编辑/删除」。
func (s *Store) GetNote(ctx context.Context, id string) (Note, error) {
	var note Note
	row := s.db.QueryRowContext(ctx,
		noteSelectSQL+` WHERE n.id = ?`, id)

	var (
		encrypted  int
		cipher     []byte
		categoryNm sql.NullString
		deletedAt  sql.NullString
	)
	if err := row.Scan(
		&note.ID, &note.Title, &note.Content, &encrypted, &cipher, &note.KDFParams,
		&note.CategoryID, &categoryNm, &note.Kind, &note.Remark, &note.Rev,
		&deletedAt, &note.CreatedAt, &note.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Note{}, ErrNoteNotFound
		}
		return Note{}, fmt.Errorf("get note: %w", err)
	}

	note.Encrypted = encrypted == 1
	note.CategoryName = categoryNm.String
	if deletedAt.Valid {
		value := deletedAt.String
		note.DeletedAt = &value
	}
	if cipher != nil {
		note.CipherB64 = base64.StdEncoding.EncodeToString(cipher)
	}
	return note, nil
}

// UpdateNote 整体替换笔记内容；rev 自增、updated_at 刷新，不存在时报错。
// 回收站里的笔记不可编辑：先恢复再改（与列表、搜索保持一致）。
func (s *Store) UpdateNote(ctx context.Context, id string, in NoteInput) (Note, error) {
	title, err := normalizeTitle(in.Title)
	if err != nil {
		return Note{}, err
	}
	body, cipher, params, err := normalizeBody(in)
	if err != nil {
		return Note{}, err
	}
	kdfSalt, err := s.kdfSaltFor(ctx, cipher != nil)
	if err != nil {
		return Note{}, err
	}
	encrypted := cipher != nil

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var wasEncrypted int
		if err := tx.QueryRowContext(ctx,
			`SELECT is_encrypted FROM notes WHERE id = ? AND deleted_at IS NULL`, id,
		).Scan(&wasEncrypted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoteNotFound
			}
			return fmt.Errorf("load note for update: %w", err)
		}
		if wasEncrypted == 1 && !encrypted {
			return ErrEncryptedImmutable
		}

		res, err := tx.ExecContext(ctx,
			`UPDATE notes
             SET title = ?, content = ?, is_encrypted = ?, cipher = ?, kdf_salt = ?, kdf_params = ?, category_id = ?,
                 rev = rev + 1, updated_at = ?
             WHERE id = ? AND deleted_at IS NULL`,
			title, body, boolInt(encrypted), cipher, kdfSalt, params, in.CategoryID, nowUTC(), id)
		if err != nil {
			return fmt.Errorf("update note: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return ErrNoteNotFound
		}
		return recordActivityTx(ctx, tx, ActivityNoteUpdate)
	})
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(ctx, id)
}

// EncryptedCipher 是改密迁移要搬运的一条密文，只带 id 与密文，不含标题或正文。
type EncryptedCipher struct {
	ID        string `json:"id"`
	CipherB64 string `json:"cipher_b64"`
	KDFParams string `json:"kdf_params"`
}

// ListEncryptedCiphers 一次返回全部加密笔记，供浏览器端逐条重加密；
// 空库返回非 nil 空切片，让 API 输出 [] 而不是 null。
func (s *Store) ListEncryptedCiphers(ctx context.Context) ([]EncryptedCipher, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, cipher, kdf_params FROM notes WHERE is_encrypted = 1 ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list encrypted ciphers: %w", err)
	}
	defer rows.Close()

	items := make([]EncryptedCipher, 0)
	for rows.Next() {
		var (
			item   EncryptedCipher
			cipher []byte
			params *string
		)
		if err := rows.Scan(&item.ID, &cipher, &params); err != nil {
			return nil, fmt.Errorf("scan encrypted cipher: %w", err)
		}
		item.CipherB64 = base64.StdEncoding.EncodeToString(cipher)
		if params != nil {
			item.KDFParams = *params
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate encrypted ciphers: %w", err)
	}
	return items, nil
}

// UpdateNoteCipher 只替换密文与派生参数：改密不是用户可见的修订，
// title / content / category_id / rev / updated_at 一概不动（设计文档 §4）。
func (s *Store) UpdateNoteCipher(ctx context.Context, id, cipherB64, kdfParams string) (Note, error) {
	raw, err := decodeCipher(cipherB64)
	if err != nil {
		return Note{}, err
	}
	params := strings.TrimSpace(kdfParams)
	if params == "" {
		return Note{}, fmt.Errorf("encrypted note requires kdf_params: %w", ErrInvalidNote)
	}

	// 先探一次目标：主密码未配置只是前置条件错误，不能盖掉「这条笔记不存在/不是加密笔记」。
	// kdf_salt 的读取也必须在事务外——连接池只有一条连接，事务内再查会自锁。
	var probe int
	if err := s.db.QueryRowContext(ctx,
		`SELECT is_encrypted FROM notes WHERE id = ?`, id,
	).Scan(&probe); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Note{}, ErrNoteNotFound
		}
		return Note{}, fmt.Errorf("probe note for cipher update: %w", err)
	}
	if probe != 1 {
		return Note{}, ErrNotEncrypted
	}

	salt, err := s.kdfSaltFor(ctx, true)
	if err != nil {
		return Note{}, err
	}

	err = s.inTx(ctx, func(tx *sql.Tx) error {
		var encrypted int
		if err := tx.QueryRowContext(ctx,
			`SELECT is_encrypted FROM notes WHERE id = ?`, id,
		).Scan(&encrypted); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoteNotFound
			}
			return fmt.Errorf("load note for cipher update: %w", err)
		}
		if encrypted != 1 {
			return ErrNotEncrypted
		}

		if _, err := tx.ExecContext(ctx,
			`UPDATE notes SET cipher = ?, kdf_params = ?, kdf_salt = ? WHERE id = ?`,
			raw, params, salt, id,
		); err != nil {
			return fmt.Errorf("update note cipher: %w", err)
		}
		return nil
	})
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(ctx, id)
}

// SoftDeleteNote 把笔记移进回收站：只写 deleted_at，正文/密文与 rev、updated_at 都不动。
// 这是「删除」的默认语义（0006）；真正清数据走 PurgeNote。
func (s *Store) SoftDeleteNote(ctx context.Context, id string) (Note, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE notes SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
			nowUTC(), id)
		if err != nil {
			return fmt.Errorf("soft delete note: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("soft delete note rows affected: %w", err)
		}
		if n == 0 {
			return ErrNoteNotFound
		}
		return recordActivityTx(ctx, tx, ActivityNoteDelete)
	})
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(ctx, id)
}

// RestoreNote 把笔记从回收站取回：只清 deleted_at，不改 updated_at（
// 恢复不是内容修订，也不该让它在列表里跳到最前），分类保持不变。
func (s *Store) RestoreNote(ctx context.Context, id string) (Note, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE notes SET deleted_at = NULL WHERE id = ? AND deleted_at IS NOT NULL`, id)
		if err != nil {
			return fmt.Errorf("restore note: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("restore note rows affected: %w", err)
		}
		if n == 0 {
			// 不存在与「不在回收站」是两件事，前端要能用 409 提示而不是 404
			return classifyMissingNote(ctx, tx, id, ErrNoteNotInTrash)
		}
		return recordActivityTx(ctx, tx, ActivityNoteRestore)
	})
	if err != nil {
		return Note{}, err
	}
	return s.GetNote(ctx, id)
}

// PurgeNote 彻底删除回收站里的笔记：真删行，提交后立刻 TRUNCATE WAL，
// 避免已删正文在日志文件里残留。未删除的笔记不允许直接彻底删除。
func (s *Store) PurgeNote(ctx context.Context, id string) error {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`DELETE FROM notes WHERE id = ? AND deleted_at IS NOT NULL`, id)
		if err != nil {
			return fmt.Errorf("purge note: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("purge note rows affected: %w", err)
		}
		if n == 0 {
			return classifyMissingNote(ctx, tx, id, ErrNoteNotInTrash)
		}
		return recordActivityTx(ctx, tx, ActivityNotePurge)
	})
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("wal checkpoint after purge: %w", err)
	}
	return nil
}

// classifyMissingNote 区分「行不存在」（ErrNoteNotFound）与「存在但不在回收站」
// （otherwise），让 404 与 409 各自可读。
func classifyMissingNote(ctx context.Context, tx *sql.Tx, id string, otherwise error) error {
	var present int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM notes WHERE id = ?`, id).Scan(&present)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoteNotFound
	}
	if err != nil {
		return fmt.Errorf("probe note %s: %w", id, err)
	}
	return otherwise
}

const noteSelectSQL = `SELECT n.id, n.title, n.content, n.is_encrypted, n.cipher, n.kdf_params,
        n.category_id, c.name, n.kind, n.remark, n.rev, n.deleted_at, n.created_at, n.updated_at
        FROM notes n LEFT JOIN categories c ON c.id = n.category_id`

// ListNotes 分页列出或搜索笔记：CategoryID 为 0 时不过滤，Page 从 1 起。
// Deleted=false 只看未删除的（默认）；Deleted=true 只看回收站，并按移入时间倒序。
// Q 非空时记录一次查询活动，且标题命中的笔记排在正文命中之前。
func (s *Store) ListNotes(ctx context.Context, filter NoteFilter) (NotePage, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	result := NotePage{Items: []NoteListItem{}, Page: page, PerPage: notesPerPage}

	q := buildNoteQuery(filter)
	if q.search != "" {
		if err := s.RecordSearch(ctx, q.search); err != nil {
			return result, err
		}
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notes n WHERE `+q.clause, q.args...,
	).Scan(&result.Total); err != nil {
		return result, fmt.Errorf("count notes: %w", err)
	}

	args := q.args
	titleRank := ""
	// 标题命中排前：ORDER BY 的占位符必须排在 WHERE 之后、LIMIT 之前
	if q.like != "" && !filter.Deleted {
		titleRank = titleRankSQL
		args = append(args, q.like)
	}
	args = append(args, notesPerPage, (page-1)*notesPerPage)

	rows, err := s.db.QueryContext(ctx,
		listSelectSQL+` WHERE `+q.clause+`
         ORDER BY `+titleRank+q.orderBy+`
         LIMIT ? OFFSET ?`,
		args...)
	if err != nil {
		return result, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	items, err := scanNoteItems(rows)
	if err != nil {
		return result, err
	}
	result.Items = items
	return result, nil
}

// noteQuery 是列表查询共用的 WHERE / ORDER BY 组装结果。
// ListNotes 与 ListNotesAll 必须走同一条组装路径，否则两条路径的过滤口径
// （未分类 = category_id IS NULL、回收站只看 deleted_at）会各自漂移。
type noteQuery struct {
	clause  string
	orderBy string
	// search 是去掉首尾空白的查询词；非空表示本次带关键词，由调用方决定要不要记搜索活动。
	search string
	// like 是查询词对应的 LIKE 模式；非空时调用方要在 WHERE 参数之后追加标题命中排序的占位符参数。
	like string
	args []any
}

// titleRankSQL 让标题命中的笔记排在正文命中之前；它自带一个占位符，
// 位置必须在 WHERE 参数之后、LIMIT 之前。
const titleRankSQL = `CASE WHEN n.title LIKE ? ESCAPE '\' THEN 0 ELSE 1 END, `

func buildNoteQuery(filter NoteFilter) noteQuery {
	where := []string{"1 = 1"}
	if filter.Deleted {
		where = append(where, "n.deleted_at IS NOT NULL")
	} else {
		where = append(where, "n.deleted_at IS NULL")
	}
	// 回收站按移入时间倒序；活动列表按更新时间倒序
	orderBy := "n.updated_at DESC, n.id DESC"
	if filter.Deleted {
		orderBy = "n.deleted_at DESC, n.id DESC"
	}

	q := noteQuery{orderBy: orderBy}
	if trimmed := strings.TrimSpace(filter.Q); trimmed != "" {
		q.search = trimmed
		q.like = "%" + escapeLike(trimmed) + "%"
		q.args = append(q.args, q.like, q.like)
		where = append(where, `(n.title LIKE ? ESCAPE '\' OR (n.is_encrypted = 0 AND n.content LIKE ? ESCAPE '\'))`)
	}
	if filter.CategoryID > 0 {
		q.args = append(q.args, filter.CategoryID)
		if filter.CategoryID == defaultCategoryID {
			// 未分类的笔记存的是 category_id IS NULL，点默认分类时要把它们一起带出来
			where = append(where, `(n.category_id = ? OR n.category_id IS NULL)`)
		} else {
			where = append(where, `n.category_id = ?`)
		}
	}
	q.clause = strings.Join(where, " AND ")
	return q
}

// scanNoteItems 把列表查询的行读成 NoteListItem；加密笔记没有明文摘要列，
// 明文只回前 140 码点用于预览。
func scanNoteItems(rows *sql.Rows) ([]NoteListItem, error) {
	items := []NoteListItem{}
	for rows.Next() {
		var (
			item       NoteListItem
			encrypted  int
			content    *string
			categoryID sql.NullInt64
			categoryNm sql.NullString
			deletedAt  sql.NullString
		)
		if err := rows.Scan(
			&item.ID, &item.Title, &content, &encrypted,
			&categoryID, &categoryNm, &deletedAt, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan note list item: %w", err)
		}

		item.Encrypted = encrypted == 1
		if categoryID.Valid {
			value := categoryID.Int64
			item.CategoryID = &value
		}
		item.CategoryName = categoryNm.String
		if deletedAt.Valid {
			value := deletedAt.String
			item.DeletedAt = &value
		}
		if content != nil && !item.Encrypted {
			summary := truncateRunes(*content, summaryRunes)
			item.Content = &summary
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notes: %w", err)
	}
	return items, nil
}

// ListNotesAll 返回全部（deleted 为真时只返回回收站）笔记列表项：不分页、不记搜索活动。
// note list 需要一次性读进内存做本地模糊筛选：逐键查库会经 RecordSearch 污染搜索历史，
// 而每页 20 条的 LIMIT 会让「筛选」退化成「只筛第一页」。
func (s *Store) ListNotesAll(ctx context.Context, deleted bool) ([]NoteListItem, error) {
	q := buildNoteQuery(NoteFilter{Deleted: deleted})

	rows, err := s.db.QueryContext(ctx,
		listSelectSQL+` WHERE `+q.clause+`
         ORDER BY `+q.orderBy,
		q.args...)
	if err != nil {
		return nil, fmt.Errorf("list all notes: %w", err)
	}
	defer rows.Close()

	return scanNoteItems(rows)
}

const listSelectSQL = `SELECT n.id, n.title, n.content, n.is_encrypted, n.category_id, c.name, n.deleted_at, n.updated_at
        FROM notes n LEFT JOIN categories c ON c.id = n.category_id`

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}

func normalizeTitle(title string) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" || utf8.RuneCountInString(trimmed) > maxTitleRunes {
		return "", fmt.Errorf("create or update note: title must be 1-%d characters: %w", maxTitleRunes, ErrInvalidNote)
	}
	return trimmed, nil
}

// normalizeBody 校验明文/密文成对结构，返回入库用的正文与密文。
func normalizeBody(in NoteInput) (*string, []byte, *string, error) {
	hasPlaintext := in.Content != nil
	hasCipher := strings.TrimSpace(in.CipherB64) != ""
	if hasPlaintext == hasCipher {
		return nil, nil, nil, fmt.Errorf("note must carry either content or cipher: %w", ErrInvalidNote)
	}

	if hasPlaintext {
		body := *in.Content
		if utf8.RuneCountInString(body) > maxContentRunes {
			return nil, nil, nil, fmt.Errorf("content exceeds %d characters: %w", maxContentRunes, ErrInvalidNote)
		}
		return &body, nil, nil, nil
	}

	if strings.TrimSpace(in.KDFParams) == "" {
		return nil, nil, nil, fmt.Errorf("encrypted note requires kdf_params: %w", ErrInvalidNote)
	}
	raw, err := decodeCipher(in.CipherB64)
	if err != nil {
		return nil, nil, nil, err
	}
	params := in.KDFParams
	return nil, raw, &params, nil
}

// decodeCipher 解开线上传来的 base64 密文并做长度守卫，越界一律拒绝。
func decodeCipher(cipherB64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(cipherB64)
	if err != nil {
		return nil, fmt.Errorf("decode cipher: %w", err)
	}
	if len(raw) < minCipherBytes || len(raw) > maxCipherBytes {
		return nil, fmt.Errorf("cipher must be %d-%d bytes: %w", minCipherBytes, maxCipherBytes, ErrInvalidNote)
	}
	return raw, nil
}

// kdfSaltFor 返回加密笔记要落库的 kdf_salt：master_password.salt 的冗余副本，
// 仅为满足 0001 的 CHECK（加密态 kdf_salt NOT NULL）；解密以 master_password.salt 为准。
func (s *Store) kdfSaltFor(ctx context.Context, encrypted bool) ([]byte, error) {
	if !encrypted {
		return nil, nil
	}
	var salt []byte
	err := s.db.QueryRowContext(ctx, `SELECT salt FROM master_password WHERE id = 1`).Scan(&salt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMasterPasswordMissing
	}
	if err != nil {
		return nil, fmt.Errorf("load master salt: %w", err)
	}
	return salt, nil
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
