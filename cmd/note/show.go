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
	"note/internal/clipboard"
	"note/internal/store"
)

// copyToClipboard 留成变量：测试可以替换它，免得测试真的写开发机的剪贴板。
var copyToClipboard = clipboard.Copy

// runShow 是 note show 的入口：加载配置、开库、探测终端。
// 给了 id 就直接渲染；没给 id 时进交互选择器，选中再渲染。两条路都会把
// 原文（库里的 Markdown，不是渲染后的终端文本）复制到剪贴板。
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

	ctx := context.Background()
	tty := stdoutIsTerminal()
	if strings.TrimSpace(show.ID) == "" {
		return showPicked(ctx, st, show, tty)
	}
	return openNote(ctx, st, show.ID, show, tty, os.Stdout)
}

// showPicked 跑 note show 的选择器：非终端、空库、用户 Esc 各给出可读的结果与退出码。
func showPicked(ctx context.Context, st *store.Store, show cli.ShowOptions, tty bool) error {
	if !tty || !cli.IsTerminal(os.Stdin) {
		return fmt.Errorf("note show 需要一个笔记 id；想在终端里挑一条就交互式运行 note show")
	}

	res, err := cli.PickNote(ctx, listDeps(st))
	if err != nil {
		return err
	}
	if res.Text != "" {
		// 空库提示（“还没有笔记，用 note add 新建一条”）走 stderr：
		// stdout 只留笔记正文，脚本永远能拿到干净输出。
		fmt.Fprint(os.Stderr, res.Text)
		return exitError(1)
	}
	if res.Selected == nil {
		// 没选就退出不是失败，但也没看成任何笔记：130 与 note add 的「放弃」一致
		return exitError(130)
	}
	return openNote(ctx, st, res.Selected.ID, show, tty, os.Stdout)
}

// showNote 读一条笔记并算出要打印的内容（不打印、不复制）。
func showNote(ctx context.Context, st *store.Store, id string, opts cli.ShowOptions, tty bool) (cli.ShowResult, error) {
	// id 是要渲染哪一条的唯一来源：选择器选中后预览时 opts 里没有 id
	opts.ID = id
	return cli.BuildShow(ctx, showDeps(st), opts, tty, stdoutWidth(tty))
}

// openNote = 渲染并打印 + 把原文复制到剪贴板。note show 与 note list 选中都走它：
// 「打开一条笔记」在 CLI 里只该有一个含义，两条入口不该有两套行为。
// 复制只在终端下做：管道与脚本（`note show x --raw > a.md`）不该被偷偷改剪贴板。
func openNote(ctx context.Context, st *store.Store, id string, opts cli.ShowOptions, tty bool, out io.Writer) error {
	res, err := showNote(ctx, st, id, opts, tty)
	if printErr := printShow(res, out); printErr != nil {
		return printErr
	}
	if err == nil && tty {
		copySource(res.Source)
	}
	return showExitError(err)
}

// copySource 复制库里的原始 Markdown。失败只提示，绝不影响查看；
// 提示走 stderr：stdout 要留给正文（`note show x --raw` 的输出不能被污染）。
func copySource(source string) {
	if err := copyToClipboard(source); err != nil {
		fmt.Fprintln(os.Stderr, clipboard.ExplainFailure(err))
		return
	}
	fmt.Fprintln(os.Stderr, "已复制原文到剪贴板")
}

// showExitError 把渲染结果里的错误映射成退出码：加密笔记是 4，不是故障。
func showExitError(err error) error {
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
			// 走 ViewNote 而不是 GetNote：cli 的阅读（note show 与 note list 的 Enter
			// 预览）也算一次「查看」，与网页详情页同口径。
			note, err := st.ViewNote(ctx, id)
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
