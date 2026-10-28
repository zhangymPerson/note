package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultConfigFile 返回配置文件的默认位置，与其他常见开源项目一致：
// 跟随操作系统的用户配置目录，而不是进程工作目录。
//
//	Windows: %AppData%\note\note.yaml
//	macOS:   ~/Library/Application Support/note/note.yaml
//	Linux:   $XDG_CONFIG_HOME/note/note.yaml（默认 ~/.config/note/note.yaml）
func DefaultConfigFile() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "note", "note.yaml"), nil
}

// DefaultDataDir 返回数据文件的默认目录，跟随操作系统的用户数据目录惯例：
//
//	Windows: %AppData%\note
//	macOS:   ~/Library/Application Support/note
//	Linux:   $XDG_DATA_HOME/note（默认 ~/.local/share/note）
func DefaultDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("resolve user config directory: %w", err)
		}
		return filepath.Join(dir, "note"), nil
	case "darwin", "ios":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home directory: %w", err)
		}
		return filepath.Join(home, "Library", "Application Support", "note"), nil
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "note"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home directory: %w", err)
		}
		return filepath.Join(home, ".local", "share", "note"), nil
	}
}

// DefaultDBFile 返回 SQLite 数据库的默认位置（用户数据目录下的 note/notes.db）。
func DefaultDBFile() (string, error) {
	dir, err := DefaultDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "notes.db"), nil
}
