package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
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

// RunGetCategory 执行 note get @分类：每行「标题\tupdated_at」，按更新时间倒序。
// 空分类返回空串而不是错误：脚本里 `get @分类 | wc -l` 拿 0 就够了。
func RunGetCategory(ctx context.Context, deps QuickDeps, ref Ref) (string, error) {
	category, err := deps.Category(ctx, ref.Category)
	if err != nil {
		return "", err
	}
	notes, err := deps.Notes(ctx, category)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	for _, note := range notes {
		b.WriteString(note.Title)
		b.WriteString("\t")
		b.WriteString(note.UpdatedAt)
		b.WriteString("\n")
	}
	return b.String(), nil
}

// RunGetCategories 执行 note get --list：列出全部分类（含笔记数与固定分类标记）。
func RunGetCategories(ctx context.Context, deps QuickDeps) (string, error) {
	categories, err := deps.Categories(ctx)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	writer := tabwriter.NewWriter(&b, 0, 8, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "ID\t分类\t笔记数\t备注"); err != nil {
		return "", err
	}
	for _, category := range categories {
		remark := ""
		if category.Reserved {
			remark = "固定"
		}
		if _, err := fmt.Fprintf(writer, "%d\t%s\t%d\t%s\n",
			category.ID, category.Name, category.Count, remark); err != nil {
			return "", err
		}
	}
	if err := writer.Flush(); err != nil {
		return "", err
	}
	return b.String(), nil
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
		return "", fmt.Errorf("分类「%s」下没有可删除的笔记", category.Name)
	}
	return fmt.Sprintf("已移入回收站 %d 条（分类「%s」）\n", deleted, category.Name), nil
}
