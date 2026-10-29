// Package template 管理新建笔记的 markdown 骨架文件（note-example.md）。
// 骨架与 note.yaml 同目录，用户在页面或编辑器里改，服务端只管读写。
package template

import (
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName 是骨架文件的固定文件名，与 note.yaml 放在同一目录。
const FileName = "note-example.md"

// defaultContent 是编译进二进制的默认骨架：单二进制分发时也要能补出这个文件。
//
//go:embed default.md
var defaultContent string

// Path 是纯函数：骨架永远落在配置文件所在目录，不读环境变量、不引入新配置项。
func Path(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), FileName)
}

func Default() string {
	return defaultContent
}

// Ensure 只在文件缺失时写入默认骨架，绝不覆盖用户改过的内容。
func Ensure(path string) (created bool, err error) {
	_, statErr := os.Stat(path)
	switch {
	case statErr == nil:
		return false, nil
	case !errors.Is(statErr, fs.ErrNotExist):
		return false, fmt.Errorf("stat note template: %w", statErr)
	}

	if err := Save(path, Default()); err != nil {
		return false, err
	}
	return true, nil
}

// Load 读文件；文件不存在或不可读时回退到内置默认值，fromDefault 标记来源。
// 错误只用来记日志：模板缺失不该让页面报错。
func Load(path string) (content string, fromDefault bool, err error) {
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		return string(data), false, nil
	case errors.Is(readErr, fs.ErrNotExist):
		return Default(), true, nil
	default:
		return Default(), true, fmt.Errorf("read note template: %w", readErr)
	}
}

// Save 原子替换骨架文件：同目录临时文件 + rename，中断不会留下半截模板。
func Save(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".note-example-*.md")
	if err != nil {
		return fmt.Errorf("create temp template: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp template: %w", err)
	}
	// 模板不含密码或正文，0644 足够；配置目录本身是 0700。
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp template: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp template: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace note template: %w", err)
	}
	return nil
}
