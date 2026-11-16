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

// runAdd 跑 note add：带正文走快速记法（直接落速记，--ai 先预览确认），
// 无参数走三步向导。打开库、注入 store 能力、打印结果。
// 用户放弃（Esc/Ctrl+C）与缺终端都不是"失败"意义上的崩溃，用固定退出码表达。
func runAdd(globalConfigPath string, args []string, addOpts cli.AddOptions) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	if addOpts.Content != "" {
		return runAddQuick(cfg, st, addOpts)
	}

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

// runAddQuick 执行快速记法：ai=false 直接落库，ai=true 进单屏预览确认。
// AI 预览要终端；非 AI 的 echo … | xargs note add 管道不需要。
func runAddQuick(cfg config.Config, st *store.Store, addOpts cli.AddOptions) error {
	deps := cli.QuickAddDeps{
		CreateQuick: func(ctx context.Context, content, title string) (cli.NoteRef, error) {
			n, err := st.CreateQuickNote(ctx, content, title)
			if err != nil {
				return cli.NoteRef{}, err
			}
			return cli.NoteRef{ID: n.ID, Title: n.Title, CategoryName: n.CategoryName}, nil
		},
	}
	if addOpts.AI {
		if !cli.IsTerminal(os.Stdin) {
			fmt.Fprintln(os.Stderr, cli.ErrNotTerminal)
			return exitError(1)
		}
		client := ai.NewClient(cfg.AI)
		deps.AIModel, deps.AIHost = client.DefaultModel(), client.Host()
		if client.Configured() {
			deps.QuickNote = func(ctx context.Context, content string) (cli.QuickNoteResult, error) {
				res, err := client.QuickNote(ctx, ai.Request{Content: content})
				if err != nil {
					return cli.QuickNoteResult{}, err
				}
				return cli.QuickNoteResult{Title: res.Title, Content: res.Content, Model: res.Model}, nil
			}
		}
	}

	ref, saved, err := cli.RunAddQuick(context.Background(), deps, addOpts.Content, addOpts.AI)
	if err != nil {
		return err
	}
	if !saved {
		fmt.Fprintln(os.Stderr, "已放弃，未新建笔记")
		return exitError(130)
	}

	// 成功提示写 stderr：stdout 保持干净，脚本管道不被打扰。
	fmt.Fprintf(os.Stderr, "已记入速记「%s」\n", ref.Title)
	fmt.Fprintf(os.Stderr, "  ID    %s\n", ref.ID)
	fmt.Fprintf(os.Stderr, "  链接  %s/notes/%s\n", baseURL(cfg.Addr), ref.ID)
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
					Reserved:  c.Reserved,
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

// addDepsWithAI 补齐 AI 四项：配置不齐时 Optimize 保持 nil（Ctrl+O 只提示怎么补配置），
// 齐了就把 internal/ai 包成向导要的形状——CLI 不要求 note web 正在跑。
func addDepsWithAI(deps cli.AddDeps, cfg config.Config) cli.AddDeps {
	client := ai.NewClient(cfg.AI)
	deps.AIModel, deps.AIHost = client.DefaultModel(), client.Host()
	if !client.Configured() {
		return deps
	}
	deps.Optimize = func(ctx context.Context, content string) (cli.OptimizeResult, error) {
		// 命令行没有「自定义提示词」这一栏（CLI 不该再开一个多行输入），
		// 用配置里的 ai.prompt / 内置默认提示词就够了。
		res, err := client.Optimize(ctx, ai.Request{Content: content})
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
