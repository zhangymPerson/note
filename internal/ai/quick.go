package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// maxQuickTitleRunes 是 AI 速记标题的码点上限：概括标题给到 60 字足够。
	maxQuickTitleRunes = 60
	// maxQuickContentRunes 是 AI 速记正文的码点上限，与 store/web 的正文上限同值。
	maxQuickContentRunes = 2000
)

// defaultQuickPrompt 是速记专用提示词（内建常量）。它刻意不是 ai.prompt 模板的
// 变体：速记不套用用户的自定义模板，正文直接追加在指令之后（buildQuickPrompt）。
// 要求模型只输出一个 JSON 对象，标题一句概括、正文是整理后的 Markdown。
const defaultQuickPrompt = `你是本地笔记应用的速记整理助手。把下面的速记原文整理成一条简明、条理清晰的 Markdown 笔记。
要求：
1. 只输出一个 JSON 对象，不要任何其它文字、代码块或注释。
2. 格式必须是 {"title":"…","content":"…"}。
3. title 是一句不超过 30 字的概括标题。
4. content 是整理后的 Markdown 正文，保留要点、不编造、不超过 2000 字。
5. JSON 内的换行用 \n 转义。`

// buildQuickPrompt 把速记原文接在专用提示词之后。不展开任何变量：
// 速记提示词不是 ai.prompt 模板，正文直接追加在指令末尾。
func buildQuickPrompt(content string) string {
	return defaultQuickPrompt + "\n\n【速记原文】\n" + content
}

// QuickNoteResult 是 AI 速记的结果：Content 已整理、Title 是概括标题、
// Model 是实际用的模型。
type QuickNoteResult struct {
	Content string
	Title   string
	Model   string
}

// QuickResultError 表示 AI 速记结果不符合预期（缺字段/超长等）。
// Reason 面向用户：网页 502 用它做响应消息，命令行状态行直接回显。
type QuickResultError struct{ Reason string }

func (e QuickResultError) Error() string { return e.Reason }

// QuickNote 把一段速记原文交给模型整理，要求返回 {"title","content"}。
// 只做「预览」：调用方在用户确认前不得落库。结果校验一律不截断，
// 任一不符返回 QuickResultError（或传输层的既有错误集）。
func (c *Client) QuickNote(ctx context.Context, req Request) (QuickNoteResult, error) {
	raw, model, err := c.chat(ctx, req.Model, buildQuickPrompt(req.Content))
	if err != nil {
		return QuickNoteResult{}, err
	}

	// 先解 OpenAI 外壳拿 message.content，再解速记要求的内部 JSON 对象。
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return QuickNoteResult{}, ErrBadResponse
	}
	if len(parsed.Choices) == 0 {
		return QuickNoteResult{}, ErrBadResponse
	}
	text := StripFence(parsed.Choices[0].Message.Content)

	var out struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return QuickNoteResult{}, fmt.Errorf("AI 速记返回的不是 JSON 对象: %w", ErrBadResponse)
	}
	out.Title = strings.TrimSpace(out.Title)
	out.Content = strings.TrimSpace(out.Content)

	switch {
	case out.Title == "":
		return QuickNoteResult{}, QuickResultError{Reason: "AI 速记返回的标题是空的，已放弃保存（不截断）"}
	case utf8.RuneCountInString(out.Title) > maxQuickTitleRunes:
		return QuickNoteResult{}, QuickResultError{Reason: fmt.Sprintf("AI 速记的标题超过 %d 码点，已放弃保存（不截断）", maxQuickTitleRunes)}
	case out.Content == "":
		return QuickNoteResult{}, QuickResultError{Reason: "AI 速记返回的正文是空的，已放弃保存（不截断）"}
	case utf8.RuneCountInString(out.Content) > maxQuickContentRunes:
		return QuickNoteResult{}, QuickResultError{Reason: fmt.Sprintf("AI 速记的正文超过 %d 码点，已放弃保存（不截断）", maxQuickContentRunes)}
	}

	return QuickNoteResult{Title: out.Title, Content: out.Content, Model: model}, nil
}
