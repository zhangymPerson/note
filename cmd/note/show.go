package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/term"

	"note/internal/cli"
	"note/internal/store"
)

// runShow 是 note show 的入口：加载配置、开库、探测终端，再交给 renderNote 打印。
func runShow(globalConfigPath string, args []string, show cli.ShowOptions) error {
	cfg, err := loadConfig(globalConfigPath, args)
	if err != nil {
		return err
	}

	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()

	return renderNote(context.Background(), st, show.ID, show, stdoutIsTerminal(), os.Stdout)
}

// renderNote 是 note show 与 note list 共用的预览：算出内容 → 打印 → 把「加密笔记」
// 映射成退出码 4。tty 与输出流由调用方传入，测试因此不需要真终端。
func renderNote(ctx context.Context, st *store.Store, id string, opts cli.ShowOptions, tty bool, out io.Writer) error {
	// id 是「要渲染哪一条」的唯一来源：note list 选中后预览时 opts 里没有 id
	opts.ID = id
	res, err := cli.BuildShow(ctx, showDeps(st), opts, tty, stdoutWidth(tty))
	if printErr := printShow(res, out); printErr != nil {
		return printErr
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, cli.ErrNoteEncrypted) {
		// 不是故障：头部与提示已经打出去了，退出码单独区分「看不了」与「没找到」
		return exitError(4)
	}
	return err
}

// printShow 打印预览结果：有头部时先打头部与一个空行，正文以换行收尾。
// 什么都没算出来（比如找不到笔记）时一个字节都不写；
// 正文本身已带结尾换行时不再补一个，`--raw` 因此是逐字节原样输出。
func printShow(res cli.ShowResult, out io.Writer) error {
	if res.Header == "" && res.Body == "" {
		return nil
	}

	var b strings.Builder
	if res.Header != "" {
		b.WriteString(res.Header)
		b.WriteString("\n\n")
	}
	b.WriteString(res.Body)
	if !strings.HasSuffix(res.Body, "\n") {
		b.WriteString("\n")
	}

	if _, err := io.WriteString(out, b.String()); err != nil {
		return fmt.Errorf("写输出失败：%w", err)
	}
	return nil
}

// showDeps 把 store 的一条笔记读成 cli 需要的最小形状。
// 找不到笔记时换成中文错误：store 层的文案是英文，不该直接甩给用户。
func showDeps(st *store.Store) cli.ShowDeps {
	return cli.ShowDeps{
		Note: func(ctx context.Context, id string) (cli.NoteDetail, error) {
			note, err := st.GetNote(ctx, id)
			if err != nil {
				if errors.Is(err, store.ErrNoteNotFound) {
					return cli.NoteDetail{}, fmt.Errorf("找不到笔记 %s", id)
				}
				return cli.NoteDetail{}, err
			}

			detail := cli.NoteDetail{
				ID:           note.ID,
				Title:        note.Title,
				CategoryName: note.CategoryName,
				Encrypted:    note.Encrypted,
				UpdatedAt:    note.UpdatedAt,
			}
			if note.DeletedAt != nil {
				detail.Trashed = true
			}
			if note.Content != nil {
				detail.Content = *note.Content
			}
			return detail, nil
		},
	}
}

// stdoutIsTerminal 走 internal/cli 的同一套判定：两边口径必须一致，
// 否则可能进了 TUI 却不打印头部（Cygwin/msys 伪终端是最容易踩的例子）。
func stdoutIsTerminal() bool {
	return cli.IsTerminal(os.Stdout)
}

// stdoutWidth 返回渲染用的终端宽度；非终端或取不到时返回 0（由 cli 层回落到默认值）。
func stdoutWidth(tty bool) int {
	if !tty {
		return 0
	}
	width, _, err := term.GetSize(uintptr(os.Stdout.Fd()))
	if err != nil {
		return 0
	}
	return width
}
