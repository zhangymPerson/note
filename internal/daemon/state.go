// Package daemon 负责 note 的后台运行形态：状态文件、日志位置、控制地址推导、
// 运行实例探测与跨平台的子进程启动/存活判定。
// 它不含任何 HTTP 服务装配（那是 internal/web）与命令分发（那是 cmd/note）。
package daemon

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// 状态文件与日志文件名固定，与 SQLite 数据库同目录：
// 备份/迁移整个数据目录时三样东西一起走，排查时也不必先找配置。
const (
	stateFileName = "note.json"
	logFileName   = "note.log"
)

// State 是后台实例写下的运行状态，供 stop / status 读取。
// Token 是关停令牌，文件权限保证它是本机同用户可见的秘密。
type State struct {
	Pid       int      `json:"pid"`
	Addr      string   `json:"addr"`
	URLs      []string `json:"urls"`
	StartedAt string   `json:"started_at"`
	DB        string   `json:"db"`
	Log       string   `json:"log"`
	Token     string   `json:"token"`
	Version   string   `json:"version"`
}

// StatePath 返回状态文件路径：与数据库同目录的 note.json。
func StatePath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), stateFileName)
}

// LogPath 返回日志文件路径：与数据库同目录的 note.log。
func LogPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), logFileName)
}

// NewToken 生成 32 字节随机关停令牌（base64url，无填充）。
func NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate shutdown token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Load 读取状态文件；文件不存在时返回包装了 os.ErrNotExist 的错误。
// 解析失败与「不存在」必须可区分：前者说明有实例留下了半截文件，
// 不能当成「没在运行」静默删掉。
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return State{}, fmt.Errorf("read daemon state %s: %w", path, err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("parse daemon state %s: %w", path, err)
	}
	return st, nil
}

// Save 原子写入状态文件：同目录临时文件 + rename，权限 0600。
// 中断只会留下临时文件，不会留下解析不了的 note.json。
func Save(path string, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode daemon state: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".note-state-*.json")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace state %s: %w", path, err)
	}
	return nil
}

// RemoveIfPid 只在状态文件记录的就是 pid 所代表的那个实例时删除它，
// 避免退出晚了一步的旧实例把新实例的状态文件删掉。文件不存在不算错误。
func RemoveIfPid(path string, pid int) error {
	st, err := Load(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if st.Pid != pid {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove daemon state %s: %w", path, err)
	}
	return nil
}
