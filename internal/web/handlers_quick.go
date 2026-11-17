package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"note/internal/ai"
	"note/internal/config"
)

const (
	// quickMaxBytes 是速记请求体上限：正文最多 2000 字，标题最多 200 字，
	// 与 /api/ai/optimize 同量级。
	quickMaxBytes = 64 << 10
)

// quickRequest 是 POST /api/quick 的请求体。title 省略时由 store 自动生成
// 「速记-<UTC 秒级时间戳>」，撞秒顺延重试。
type quickRequest struct {
	Content string `json:"content"`
	Title   string `json:"title"`
}

// handleQuickNote 落一条速记到内置「速记」分类。与 POST /api/notes 不同：
// 标题可省（自动生成），分类固定为速记分类，不新增其它字段。
func (app *application) handleQuickNote(w http.ResponseWriter, r *http.Request) {
	var req quickRequest
	if err := decodeJSON(w, r, &req, quickMaxBytes); err != nil {
		return
	}

	fields := validateQuickContent(req.Content)
	title := strings.TrimSpace(req.Title)
	if title != "" && utf8.RuneCountInString(title) > maxTitleRunes {
		fields["title"] = fmt.Sprintf("标题请控制在 %d 字以内", maxTitleRunes)
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	note, err := app.store.CreateQuickNote(r.Context(), req.Content, title)
	if err != nil {
		writeNoteStoreError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusCreated, note)
}

// aiQuickRequest 是 POST /api/ai/quick 的请求体：只带速记原文，
// 标题由模型生成（只预览不落库）。
type aiQuickRequest struct {
	Content string `json:"content"`
}

// handleAIQuickNote 把速记原文交给 AI 整理，返回 {title, content, model}。
// 与 /api/ai/optimize 同构：服务端代发、每次读盘、结果不落库，由前端预览确认。
func (app *application) handleAIQuickNote(w http.ResponseWriter, r *http.Request) {
	liftWriteDeadline(w)
	var req aiQuickRequest
	if err := decodeJSON(w, r, &req, quickMaxBytes); err != nil {
		return
	}

	if fields := validateQuickContent(req.Content); len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	aiCfg, _, err := config.ReadAI(app.cfg.ConfigPath)
	if err != nil {
		slog.Error("read ai config", "path", app.cfg.ConfigPath, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "读取配置文件失败，请检查 note.yaml 的格式")
		return
	}
	client := ai.NewClient(aiCfg)
	if !client.Configured() {
		writeError(w, http.StatusConflict, "ai_not_configured",
			"AI 还没配置好：缺 "+strings.Join(client.Missing(), "、")+
				"。用 note config set <键>=<值> 补上，或在信息页的 AI 配置卡片里填。")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)
	defer cancel()
	res, err := client.QuickNote(ctx, ai.Request{Content: req.Content})
	if err != nil {
		// 日志只记 host / model / 状态与错误摘要：绝不记正文、生成的标题与密钥。
		slog.Error("quick note", "host", client.Host(), "model", res.Model, "error", err)
		var qe ai.QuickResultError
		if errors.As(err, &qe) {
			// 结果不合规（缺字段/超长不截断）：把明确原因带回前端，比通用文案有用。
			writeError(w, http.StatusBadGateway, "ai_bad_response", qe.Reason)
			return
		}
		writeOptimizeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"title":   res.Title,
		"content": res.Content,
		"model":   res.Model,
	})
}

// validateQuickContent 校验速记正文（与 /api/ai/optimize 同一口径）：
// trim 后非空、码点 ≤ maxContentRunes。
func validateQuickContent(content string) map[string]string {
	fields := map[string]string{}
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		fields["content"] = "正文不能为空"
	} else if n := utf8.RuneCountInString(content); n > maxContentRunes {
		fields["content"] = fmt.Sprintf("正文请控制在 %d 字以内，当前 %d 字", maxContentRunes, n)
	}
	return fields
}
