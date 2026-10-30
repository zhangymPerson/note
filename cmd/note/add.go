package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"note/internal/ai"
	"note/internal/cli"
	"note/internal/config"
	"note/internal/store"
)

// runAdd 跑 note add 向导：打开库、注入 store 能力、打印结果。
// 用户放弃（Esc/Ctrl+C）与缺终端都不是"失败"意义上的崩溃，用固定退出码表达。
func runAdd(globalConfigPath string, args []string) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ref, saved, err := cli.RunAdd(context.Background(), addDepsWithAI(addDeps(st), cfg))
	if err != nil {
		if errors.Is(err, cli.ErrNotTerminal) {
			fmt.Fprintln(os.Stderr, err)
			return exitError(1)
		}
		return err
	}
	if !saved {
		fmt.Fprintln(os.Stderr, "已放弃，未新建笔记")
		return exitError(130)
	}

	printAddedNote(ref, cfg)
	return nil
}

// addDeps 用 store 实现向导需要的三个动作；cli 因此不必依赖持久层。
func addDeps(st *store.Store) cli.AddDeps {
	return cli.AddDeps{
		Categories: func(ctx context.Context) ([]cli.Category, error) {
			list, err := st.ListCategories(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]cli.Category, 0, len(list))
			for _, c := range list {
				out = append(out, cli.Category{
					ID:        c.ID,
					Name:      c.Name,
					Count:     c.NoteCount,
					IsDefault: c.ID == store.DefaultCategoryID,
				})
			}
			return out, nil
		},
		CreateCategory: func(ctx context.Context, name string) (cli.Category, error) {
			c, err := st.CreateCategory(ctx, name)
			if err != nil {
				if errors.Is(err, store.ErrCategoryExists) {
					return cli.Category{}, fmt.Errorf("分类「%s」已存在", name)
				}
				return cli.Category{}, err
			}
			return cli.Category{ID: c.ID, Name: c.Name, Count: c.NoteCount}, nil
		},
		CreateNote: func(ctx context.Context, in cli.AddNote) (cli.NoteRef, error) {
			body := in.Content
			note, err := st.CreateNote(ctx, store.NoteInput{
				Title:      in.Title,
				Content:    &body,
				CategoryID: in.CategoryID,
			})
			if err != nil {
				return cli.NoteRef{}, err
			}
			return cli.NoteRef{ID: note.ID, Title: note.Title, CategoryName: note.CategoryName}, nil
		},
	}
}

// addDepsWithAI 补齐 AI 三项：配置不齐时 Optimize 保持 nil（Ctrl+O 只提示怎么补配置），
// 齐了就把 internal/ai 包成向导要的形状——CLI 不要求 note web 正在跑。
func addDepsWithAI(deps cli.AddDeps, cfg config.Config) cli.AddDeps {
	client := ai.NewClient(cfg.AI)
	deps.AIModel, deps.AIHost = client.DefaultModel(), client.Host()
	if !client.Configured() {
		return deps
	}
	deps.Optimize = func(ctx context.Context, content string) (cli.OptimizeResult, error) {
		res, err := client.Optimize(ctx, content, "")
		if err != nil {
			return cli.OptimizeResult{}, err
		}
		return cli.OptimizeResult{Content: res.Content, Model: res.Model}, nil
	}
	return deps
}

// printAddedNote 打印新建结果；链接用配置里的监听地址拼，需要 note web 正在跑才能打开。
func printAddedNote(ref cli.NoteRef, cfg config.Config) {
	fmt.Printf("已新建笔记「%s」\n", ref.Title)
	fmt.Printf("  分类  %s\n", ref.CategoryName)
	fmt.Printf("  ID    %s\n", ref.ID)
	fmt.Printf("  链接  %s/notes/%s\n", baseURL(cfg.Addr), ref.ID)
}

// baseURL 把监听地址补成可点击的 URL；已带 scheme 时只去掉尾部斜杠。
func baseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	return "http://" + addr
}
