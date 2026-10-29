package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"note/internal/cli"
	"note/internal/store"
)

// runList 是 note list 的入口：加载配置、开库、跑列表；选中一条时复用 renderNote 预览。
func runList(globalConfigPath string, args []string, list cli.ListOptions) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	res, err := cli.RunList(ctx, listDeps(st), list)
	if err != nil {
		return err
	}

	if res.Text != "" {
		if _, err := io.WriteString(os.Stdout, res.Text); err != nil {
			return fmt.Errorf("写输出失败：%w", err)
		}
		return nil
	}
	if res.Selected == nil {
		// 用户按 Esc 退出、或非交互路径没有结果：都不是失败
		return nil
	}

	// 选中即预览：风格与宽度都用默认值（跟随终端），与 note show 走同一条渲染路径
	return renderNote(ctx, st, res.Selected.ID, cli.ShowOptions{}, stdoutIsTerminal(), os.Stdout)
}

// listDeps 把 store 的列表项读成 cli 需要的最小形状。
func listDeps(st *store.Store) cli.ListDeps {
	return cli.ListDeps{
		Notes: func(ctx context.Context, deleted bool) ([]cli.ListItem, error) {
			notes, err := st.ListNotesAll(ctx, deleted)
			if err != nil {
				return nil, err
			}

			items := make([]cli.ListItem, 0, len(notes))
			for _, note := range notes {
				item := cli.ListItem{
					ID:           note.ID,
					Title:        note.Title,
					CategoryName: note.CategoryName,
					Encrypted:    note.Encrypted,
					UpdatedAt:    note.UpdatedAt,
				}
				if note.Content != nil {
					item.Summary = *note.Content
				}
				if note.DeletedAt != nil {
					item.Trashed = true
				}
				items = append(items, item)
			}
			return items, nil
		},
	}
}
