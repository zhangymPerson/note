package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"note/internal/cli"
	"note/internal/store"
)

// runSet 实现 note set：新建或覆盖一条笔记（同名即覆盖，默认落「速记」）。
// stdout 一个字节都不打印，提示写 stderr——`note set … && note get …` 这类管道
// 不该被提示文案污染。
func runSet(globalConfigPath string, args []string, opts cli.SetOptions) error {
	st, err := openQuickStore(globalConfigPath, args)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	message, err := cli.RunSet(ctx, quickDeps(st), opts.Ref, opts.Content)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrEncryptedImmutable):
			fmt.Fprintf(os.Stderr, "note: 「%s」@%s 是加密笔记，不能在 CLI 里覆盖\n", opts.Ref.Title, opts.Ref.Category)
			return exitError(1)
		case errors.Is(err, cli.ErrQuickInTrash):
			// 报出 id：这条得去网页恢复或彻底删除，没 id 不好找。
			fmt.Fprintf(os.Stderr, "note: 同名笔记在回收站里%s，先去网页恢复或彻底删除\n", trashIDHint(ctx, st, opts.Ref))
			return exitError(1)
		default:
			return err
		}
	}

	fmt.Fprint(os.Stderr, message)
	return nil
}

// runGet 实现 note get 的三种形态：读一条正文、列分类下的笔记、列全部分类。
// 只有这条命令会往 stdout 写内容。
func runGet(globalConfigPath string, args []string, opts cli.GetOptions) error {
	st, err := openQuickStore(globalConfigPath, args)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	deps := quickDeps(st)

	var (
		text    string
		readErr error
	)
	switch {
	case opts.List:
		text, readErr = cli.RunGetCategories(ctx, deps)
	case opts.Ref.CategoryOnly:
		text, readErr = cli.RunGetCategory(ctx, deps, opts.Ref)
	default:
		text, readErr = cli.RunGet(ctx, deps, opts.Ref)
	}
	if readErr != nil {
		return quickReadFailure(readErr, opts.Ref)
	}

	fmt.Print(text)
	return nil
}

// runDel 实现 note del：软删除一条，或（带 --yes 时）整个分类。
func runDel(globalConfigPath string, args []string, opts cli.DelOptions) error {
	st, err := openQuickStore(globalConfigPath, args)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	ctx := context.Background()
	deps := quickDeps(st)

	var (
		message string
		delErr  error
	)
	if opts.Ref.CategoryOnly {
		message, delErr = cli.RunDelCategory(ctx, deps, opts.Ref)
	} else {
		message, delErr = cli.RunDel(ctx, deps, opts.Ref)
	}
	if delErr != nil {
		switch {
		case errors.Is(delErr, cli.ErrQuickNothingDeleted):
			fmt.Fprintf(os.Stderr, "note: 分类「%s」下没有可删除的笔记\n", opts.Ref.Category)
			return exitError(1)
		default:
			return quickReadFailure(delErr, opts.Ref)
		}
	}

	fmt.Fprint(os.Stderr, message)
	return nil
}

// quickReadFailure 把 cli 层的哨兵错误翻成一句人话 + 退出码。
// 认不出的错误原样返回，交给 main 打日志（那是数据层故障，不是用法问题）。
func quickReadFailure(err error, ref cli.Ref) error {
	switch {
	case errors.Is(err, cli.ErrQuickNotFound):
		fmt.Fprintf(os.Stderr, "note: 在分类「%s」里找不到笔记「%s」\n", ref.Category, ref.Title)
	case errors.Is(err, cli.ErrQuickCategoryNotFound):
		fmt.Fprintf(os.Stderr, "note: 分类「%s」不存在（看现有分类用 note get --list）\n", ref.Category)
	case errors.Is(err, cli.ErrQuickInTrash):
		if ref.CategoryOnly {
			fmt.Fprintf(os.Stderr, "note: 分类「%s」里有笔记在回收站里\n", ref.Category)
		} else {
			fmt.Fprintf(os.Stderr, "note: 「%s」@%s 在回收站里\n", ref.Title, ref.Category)
		}
	case errors.Is(err, cli.ErrNoteEncrypted):
		// 加密笔记不是「找不到」：主密码只在浏览器里，CLI 永远解不开。
		fmt.Fprintf(os.Stderr, "note: 「%s」@%s 是加密笔记，CLI 解不开（主密码只在浏览器里），用网页查看\n", ref.Title, ref.Category)
		return exitError(4)
	default:
		return err
	}
	return exitError(1)
}

// trashIDHint 尽力拿到回收站里那条的 id；查不到就返回空串（提示照样给）。
func trashIDHint(ctx context.Context, st *store.Store, ref cli.Ref) string {
	cat, err := st.CategoryByName(ctx, ref.Category)
	if err != nil {
		return ""
	}
	note, err := st.FindNoteByTitle(ctx, quickCategoryID(cliCategory(cat)), ref.Title)
	if err != nil || note.ID == "" {
		return ""
	}
	return fmt.Sprintf("（id %s）", note.ID)
}

// openQuickStore 加载配置并打开数据库：set/get/del 三兄弟的公共开场。
func openQuickStore(globalConfigPath string, args []string) (*store.Store, error) {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return nil, err
	}
	return store.Open(cfg.DBPath)
}

// quickDeps 用 store 实现 set/get/del 需要的动作，并把 store 的哨兵错误
// 翻译成 cli 的哨兵（cli 不 import store，也就不会把持久层细节带进 UI 层）。
func quickDeps(st *store.Store) cli.QuickDeps {
	return cli.QuickDeps{
		Categories: func(ctx context.Context) ([]cli.Category, error) {
			list, err := st.ListCategories(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]cli.Category, 0, len(list))
			for _, c := range list {
				out = append(out, cliCategory(c))
			}
			return out, nil
		},
		Category: func(ctx context.Context, name string) (cli.Category, error) {
			c, err := st.CategoryByName(ctx, name)
			if errors.Is(err, store.ErrNotFound) {
				return cli.Category{}, cli.ErrQuickCategoryNotFound
			}
			if err != nil {
				return cli.Category{}, err
			}
			return cliCategory(c), nil
		},
		EnsureCategory: func(ctx context.Context, name string) (cli.Category, error) {
			c, err := st.EnsureCategory(ctx, name)
			if err != nil {
				return cli.Category{}, err
			}
			return cliCategory(c), nil
		},
		Set: func(ctx context.Context, cat cli.Category, title, content string) (cli.QuickNote, bool, error) {
			note, created, err := st.UpsertNoteByTitle(ctx, quickCategoryID(cat), title, content)
			if errors.Is(err, store.ErrNoteTitleInTrash) {
				return cli.QuickNote{}, false, cli.ErrQuickInTrash
			}
			if err != nil {
				return cli.QuickNote{}, false, err
			}
			return quickNote(note), created, nil
		},
		Get: func(ctx context.Context, cat cli.Category, title string) (cli.QuickNote, error) {
			note, err := st.FindNoteByTitle(ctx, quickCategoryID(cat), title)
			if errors.Is(err, store.ErrNoteNotFound) {
				return cli.QuickNote{}, cli.ErrQuickNotFound
			}
			if err != nil {
				return cli.QuickNote{}, err
			}
			return quickNote(note), nil
		},
		Notes: func(ctx context.Context, cat cli.Category) ([]cli.QuickNote, error) {
			list, err := st.ListNotesInCategory(ctx, cat.ID)
			if err != nil {
				return nil, err
			}
			out := make([]cli.QuickNote, 0, len(list))
			for _, item := range list {
				out = append(out, cli.QuickNote{
					Title:     item.Title,
					Encrypted: item.Encrypted,
					UpdatedAt: item.UpdatedAt,
				})
			}
			return out, nil
		},
		Delete: func(ctx context.Context, cat cli.Category, title string) (cli.QuickNote, error) {
			note, err := st.SoftDeleteNoteByTitle(ctx, quickCategoryID(cat), title)
			if errors.Is(err, store.ErrNoteNotFound) {
				return cli.QuickNote{}, cli.ErrQuickNotFound
			}
			if errors.Is(err, store.ErrNoteTitleInTrash) {
				return cli.QuickNote{}, cli.ErrQuickInTrash
			}
			if err != nil {
				return cli.QuickNote{}, err
			}
			return quickNote(note), nil
		},
		DeleteNotes: func(ctx context.Context, cat cli.Category) (int, error) {
			return st.SoftDeleteCategoryNotes(ctx, quickCategoryID(cat))
		},
	}
}

// quickCategoryID 把分类映射成库里的写法：内置「未分类」是 category_id IS NULL。
func quickCategoryID(cat cli.Category) *int64 {
	if cat.IsDefault || cat.ID == store.DefaultCategoryID {
		return nil
	}
	id := cat.ID
	return &id
}

// cliCategory 把 store 的分类转成 cli 的形状（字段名不同，语义一致）。
func cliCategory(c store.Category) cli.Category {
	return cli.Category{
		ID:        c.ID,
		Name:      c.Name,
		Count:     c.NoteCount,
		IsDefault: c.ID == store.DefaultCategoryID,
		Reserved:  c.Reserved,
	}
}

// quickNote 把 store 的笔记转成 cli 需要的最小形状。
func quickNote(note store.Note) cli.QuickNote {
	content := ""
	if note.Content != nil {
		content = *note.Content
	}
	return cli.QuickNote{
		Title:     note.Title,
		Content:   content,
		Encrypted: note.Encrypted,
		Trashed:   note.DeletedAt != nil,
		UpdatedAt: note.UpdatedAt,
	}
}
