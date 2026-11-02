package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxQuickContentRunes 与 store/web 的正文上限同值。
const maxQuickContentRunes = 2000

var (
	// ErrQuickNotFound 表示按标题找不到笔记。
	ErrQuickNotFound = errors.New("note not found by title")
	// ErrQuickInTrash 表示命名的那条在回收站里：set/get/del 都不碰回收站。
	ErrQuickInTrash = errors.New("note is in trash")
	// ErrQuickCategoryNotFound 表示分类不存在。
	ErrQuickCategoryNotFound = errors.New("category not found")
	// ErrQuickNothingDeleted 表示整分类删除没删到任何笔记（都是回收站里的或本来就空）。
	ErrQuickNothingDeleted = errors.New("no note to delete in category")
)

// QuickNote 是 note set/get/del 需要的最小笔记形状。
type QuickNote struct {
	Title     string
	Content   string
	Encrypted bool
	Trashed   bool
	UpdatedAt string
}

// QuickDeps 是 note set/get/del 需要的持久层能力。
// cli 不依赖 store：store 的哨兵错误由 cmd/note 的适配层翻译成上面三个哨兵。
type QuickDeps struct {
	Categories     func(ctx context.Context) ([]Category, error)
	Category       func(ctx context.Context, name string) (Category, error)
	EnsureCategory func(ctx context.Context, name string) (Category, error)
	Set            func(ctx context.Context, category Category, title, content string) (QuickNote, bool, error)
	Get            func(ctx context.Context, category Category, title string) (QuickNote, error)
	Notes          func(ctx context.Context, category Category) ([]QuickNote, error)
	Delete         func(ctx context.Context, category Category, title string) (QuickNote, error)
	DeleteNotes    func(ctx context.Context, category Category) (int, error)
}

// RunSet 执行 note set：新建或覆盖同名笔记。返回值是写给 stderr 的提示
// （stdout 永远为空，脚本管道里不该出现看不见的副作用）。
func RunSet(ctx context.Context, deps QuickDeps, ref Ref, content string) (string, error) {
	if ref.CategoryOnly || strings.TrimSpace(ref.Title) == "" {
		return "", fmt.Errorf("note set 需要标题：写成 note set <标题[@分类]> <正文>")
	}
	if utf8.RuneCountInString(content) > maxQuickContentRunes {
		return "", fmt.Errorf("正文最多 %d 个字符（得到 %d 个）", maxQuickContentRunes, utf8.RuneCountInString(content))
	}

	// EnsureCategory：`note set 标题@新分类` 顺带把分类建出来。
	category, err := deps.EnsureCategory(ctx, ref.Category)
	if err != nil {
		return "", err
	}
	_, created, err := deps.Set(ctx, category, ref.Title, content)
	if err != nil {
		return "", err
	}

	verb := "已更新"
	if created {
		verb = "已保存"
	}
	return fmt.Sprintf("%s「%s」@%s\n", verb, ref.Title, category.Name), nil
}

// RunGet 执行 note get <标题[@分类]>：返回正文，缺结尾换行时补一个。
func RunGet(ctx context.Context, deps QuickDeps, ref Ref) (string, error) {
	if ref.CategoryOnly || strings.TrimSpace(ref.Title) == "" {
		return "", fmt.Errorf("note get 需要标题：写成 note get <标题[@分类]>，或 note get @<分类> / note get --list")
	}

	category, err := deps.Category(ctx, ref.Category)
	if err != nil {
		return "", err
	}
	note, err := deps.Get(ctx, category, ref.Title)
	if err != nil {
		return "", err
	}
	if note.Trashed {
		return "", ErrQuickInTrash
	}
	if note.Encrypted {
		return "", ErrNoteEncrypted
	}
	if !strings.HasSuffix(note.Content, "\n") {
		return note.Content + "\n", nil
	}
	return note.Content, nil
}

// RunGetCategory 执行 note get @分类：默认每行只给标题，按更新时间倒序。
// detail 为真时改用 tabwriter 表格，多给更新时间与正文摘要（加密笔记没有可展示的正文）。
// 空分类返回空串而不是错误：脚本里 `get @分类 | wc -l` 拿 0 就够了。
func RunGetCategory(ctx context.Context, deps QuickDeps, ref Ref, detail bool) (string, error) {
	category, err := deps.Category(ctx, ref.Category)
	if err != nil {
		return "", err
	}
	notes, err := deps.Notes(ctx, category)
	if err != nil {
		return "", err
	}
	if len(notes) == 0 {
		return "", nil
	}

	var b strings.Builder
	if !detail {
		for _, note := range notes {
			b.WriteString(note.Title)
			b.WriteString("\n")
		}
		return b.String(), nil
	}

	rows := make([][]string, 0, len(notes))
	for _, note := range notes {
		body := "（加密）"
		switch {
		case note.Encrypted:
		case strings.TrimSpace(note.Content) == "":
			body = "（空）"
		default:
			body = foldForTable(note.Content, detailContentRunes)
		}
		rows = append(rows, []string{note.Title, note.UpdatedAt, body})
	}
	return renderTable([]string{"标题", "更新时间", "正文"}, rows), nil
}

// detailContentRunes 是 --detail 表格里正文摘要的上限：表格要能一眼看完，
// 完整正文仍旧走 note get <标题>。
const detailContentRunes = 60

// foldForTable 把多行正文折成一行（表格里不能有换行），超长时按码点截断加省略号。
func foldForTable(content string, limit int) string {
	folded := strings.Join(strings.Fields(content), " ")
	if utf8.RuneCountInString(folded) <= limit {
		return folded
	}
	runes := []rune(folded)
	return string(runes[:limit]) + "…"
}

// RunGetCategories 执行 note get --list：列出全部分类（含笔记数与固定分类标记）。
func RunGetCategories(ctx context.Context, deps QuickDeps) (string, error) {
	categories, err := deps.Categories(ctx)
	if err != nil {
		return "", err
	}

	rows := make([][]string, 0, len(categories))
	for _, category := range categories {
		row := []string{strconv.FormatInt(category.ID, 10), category.Name, strconv.Itoa(category.Count)}
		// 没有备注就少写一列：多一个空列会让管道里多出一个空字段。
		if category.Reserved {
			row = append(row, "固定")
		}
		rows = append(rows, row)
	}
	return renderTable([]string{"ID", "分类", "笔记数", "备注"}, rows), nil
}

// renderTable 用显示宽度（uniseg）对齐一张表：tabwriter 按码点算列宽，
// 中文列会错位，所以这里按显示宽度补空格。行可以比表头短（缺的列就当没有），
// 最后一列不补齐，避免管道里出现行尾空格。
func renderTable(header []string, rows [][]string) string {
	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = displayWidth(cell)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && displayWidth(cell) > widths[i] {
				widths[i] = displayWidth(cell)
			}
		}
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			if i < len(cells)-1 {
				b.WriteString(padRight(cell, widths[i]))
				continue
			}
			b.WriteString(cell)
		}
		b.WriteString("\n")
	}
	writeRow(header)
	for _, row := range rows {
		writeRow(row)
	}
	return b.String()
}

// RunDel 执行 note del <标题[@分类]>：软删除一条（移入回收站）。
func RunDel(ctx context.Context, deps QuickDeps, ref Ref) (string, error) {
	if ref.CategoryOnly || strings.TrimSpace(ref.Title) == "" {
		return "", fmt.Errorf("note del 需要标题：写成 note del <标题[@分类]>，删整个分类用 note del @<分类> --yes")
	}

	category, err := deps.Category(ctx, ref.Category)
	if err != nil {
		return "", err
	}
	if _, err := deps.Delete(ctx, category, ref.Title); err != nil {
		return "", err
	}
	return fmt.Sprintf("已移入回收站「%s」@%s\n", ref.Title, category.Name), nil
}

// RunDelCategory 执行 note del @分类 --yes：软删除分类下所有未删除笔记。
// 「有没有删到东西」是脚本要判的事，因此 0 条返回错误（退出码 1）。
func RunDelCategory(ctx context.Context, deps QuickDeps, ref Ref) (string, error) {
	if !ref.CategoryOnly {
		return "", fmt.Errorf("note del @<分类> 需要分类引用")
	}

	category, err := deps.Category(ctx, ref.Category)
	if err != nil {
		return "", err
	}
	deleted, err := deps.DeleteNotes(ctx, category)
	if err != nil {
		return "", err
	}
	if deleted == 0 {
		return "", ErrQuickNothingDeleted
	}
	return fmt.Sprintf("已移入回收站 %d 条（分类「%s」）\n", deleted, category.Name), nil
}
