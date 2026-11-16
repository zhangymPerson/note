package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// quickTitleRetries 是自动标题撞秒时的顺延重试次数（含首秒）。
const quickTitleRetries = 5

// timeLayoutLocal 是自动标题（速记/合并）的时间格式：本地时间、24 小时制、无时区后缀。
// 注意：这是项目「时间戳统一 UTC 秒级」规则的唯一例外之一（见 AGENTS.md），仅作展示标题。
const timeLayoutLocal = "2006-01-02 15:04:05"

// CreateQuickNote 创建一条速记，固定落在「速记」分类（QuickCategoryID）。
// title 为空 → 自动生成 速记-<本地时间>，同秒撞名自动顺延下一秒重试（≤5 次，
// ErrNoteTitleExists 与 ErrNoteTitleInTrash 都算）；title 非空 → 单次创建，撞名原样返回。
// 校验：正文非空且 ≤maxContentRunes 码点；title 非空时 ≤maxTitleRunes 码点。
func (s *Store) CreateQuickNote(ctx context.Context, content, title string) (Note, error) {
	return s.createQuickNoteAt(ctx, content, title, time.Now())
}

func (s *Store) createQuickNoteAt(ctx context.Context, content, title string, now time.Time) (Note, error) {
	content = strings.TrimSpace(content)
	if content == "" || utf8.RuneCountInString(content) > maxContentRunes {
		return Note{}, ErrInvalidNote
	}
	title = strings.TrimSpace(title)
	if title != "" && utf8.RuneCountInString(title) > maxTitleRunes {
		return Note{}, ErrInvalidNote
	}
	catID := int64(QuickCategoryID)
	if title == "" {
		return s.createWithAutoTitleAt(ctx, "速记", content, &catID, now)
	}
	return s.CreateNote(ctx, NoteInput{Title: title, Content: &content, CategoryID: &catID})
}

// createWithAutoTitleAt 生成 <prefix>-<now 自身时区、timeLayoutLocal 格式> 的自动标题并落库；
// 同秒撞名顺延下一秒重试（≤quickTitleRetries 次，ErrNoteTitleExists 与 ErrNoteTitleInTrash 都算），
// 永不覆盖已有笔记。categoryID 为 nil 时落「未分类」（category_id NULL）。
func (s *Store) createWithAutoTitleAt(ctx context.Context, prefix, content string, categoryID *int64, now time.Time) (Note, error) {
	for i := 0; i < quickTitleRetries; i++ {
		t := prefix + "-" + now.Add(time.Duration(i)*time.Second).Format(timeLayoutLocal)
		n, err := s.CreateNote(ctx, NoteInput{Title: t, Content: &content, CategoryID: categoryID})
		if err == nil {
			return n, nil
		}
		if !errors.Is(err, ErrNoteTitleExists) && !errors.Is(err, ErrNoteTitleInTrash) {
			return Note{}, err
		}
	}
	return Note{}, fmt.Errorf("%s 自动标题连续 %d 秒被占用: %w", prefix, quickTitleRetries, ErrNoteTitleExists)
}
