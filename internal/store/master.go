package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// masterSaltLen 是 PBKDF2 输入盐的字节数，与前端 crypto.ts 约定一致。
const masterSaltLen = 16

// ErrMasterPasswordExists 表示主密码配置已写入（只允许配置一次）。
var ErrMasterPasswordExists = errors.New("master password already configured")

// MasterPassword 是全局主密码的公开配置：盐 + 助记问题，绝不含密码本身。
type MasterPassword struct {
	SaltB64   string `json:"salt_b64"`
	Question  string `json:"question"`
	CreatedAt string `json:"created_at"`
}

// GetMasterPassword 返回配置及其是否存在；未配置时返回零值、false、nil。
func (s *Store) GetMasterPassword(ctx context.Context) (MasterPassword, bool, error) {
	var (
		master MasterPassword
		salt   []byte
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT salt, question, created_at FROM master_password WHERE id = 1`,
	).Scan(&salt, &master.Question, &master.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return MasterPassword{}, false, nil
	}
	if err != nil {
		return MasterPassword{}, false, fmt.Errorf("query master password: %w", err)
	}

	master.SaltB64 = base64.StdEncoding.EncodeToString(salt)
	return master, true, nil
}

// SetMasterPassword 写入唯一一行主密码配置；服务端永不接触密码原文。
func (s *Store) SetMasterPassword(ctx context.Context, salt []byte, question string) (MasterPassword, error) {
	if len(salt) != masterSaltLen {
		return MasterPassword{}, fmt.Errorf("master salt must be %d bytes, got %d", masterSaltLen, len(salt))
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return MasterPassword{}, errors.New("master password question is required")
	}

	master := MasterPassword{
		SaltB64:   base64.StdEncoding.EncodeToString(salt),
		Question:  question,
		CreatedAt: nowUTC(),
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO master_password (id, salt, question, created_at) VALUES (1, ?, ?, ?)`,
		salt, master.Question, master.CreatedAt)
	if err != nil {
		// 第二次 INSERT 先撞 PRIMARY KEY(=1)；CHECK(id=1) 兜住手工写入的其它 id
		if isUniqueConstraint(err) || isCheckConstraintViolation(err) {
			return MasterPassword{}, ErrMasterPasswordExists
		}
		return MasterPassword{}, fmt.Errorf("insert master password: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n != 1 {
		return MasterPassword{}, ErrMasterPasswordExists
	}
	return master, nil
}

func isCheckConstraintViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "CHECK constraint failed") ||
		strings.Contains(msg, "constraint failed: CHECK")
}
