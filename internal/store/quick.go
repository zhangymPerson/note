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

// CreateQuickNote 创建一条速记，固定落在「速记」分类（QuickCategoryID）。
// title 为空 → 自动生成 速记-<UTC 秒级>，同秒撞名自动顺延下一秒重试（≤5 次，
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
		for i := 0; i < quickTitleRetries; i++ {
			t := "速记-" + now.Add(time.Duration(i)*time.Second).UTC().Format(timeLayoutUTC)
			n, err := s.CreateNote(ctx, NoteInput{Title: t, Content: &content, CategoryID: &catID})
			if err == nil {
				return n, nil
			}
			if !errors.Is(err, ErrNoteTitleExists) && !errors.Is(err, ErrNoteTitleInTrash) {
				return Note{}, err
			}
		}
		return Note{}, fmt.Errorf("速记自动标题连续 %d 秒被占用: %w", quickTitleRetries, ErrNoteTitleExists)
	}
	return s.CreateNote(ctx, NoteInput{Title: title, Content: &content, CategoryID: &catID})
}
