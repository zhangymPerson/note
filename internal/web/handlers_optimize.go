package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"note/internal/ai"
	"note/internal/config"
)

const (
	// optimizeMaxBytes 是请求体上限：正文最多 2000 字（UTF-8 最多 8000 字节），
	// 留足 JSON 转义与 model 字段的余量。
	optimizeMaxBytes = 64 << 10
	// optimizeTimeout 是单次上游调用的上限，超时映射成 504。
	optimizeTimeout = 60 * time.Second
)

type optimizeRequest struct {
	Content string `json:"content"`
	Model   string `json:"model"`
}

type optimizeResponse struct {
	Content string `json:"content"`
	Model   string `json:"model"`
}

// handleOptimizeNote 把正文交给配置好的 OpenAI 兼容接口润色格式。
//
// 之所以由服务端代发：api_key 明文只允许存在于 note.yaml，浏览器永远拿不到它。
// 每次请求都重新读配置（手改 note.yaml 立即生效），调用本身不落库、不改笔记：
// 结果回给前端预览，用户确认后才会走正常的 PUT /api/notes/{id}。
func (app *application) handleOptimizeNote(w http.ResponseWriter, r *http.Request) {
	var req optimizeRequest
	if err := decodeJSON(w, r, &req, optimizeMaxBytes); err != nil {
		return
	}

	fields := map[string]string{}
	trimmed := strings.TrimSpace(req.Content)
	if trimmed == "" {
		fields["content"] = "正文不能为空"
	} else if n := utf8.RuneCountInString(req.Content); n > maxContentRunes {
		fields["content"] = fmt.Sprintf("正文请控制在 %d 字以内，当前 %d 字", maxContentRunes, n)
	}
	if len(fields) > 0 {
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

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = client.DefaultModel()
	} else if !containsModel(client.Models, model) {
		writeValidationError(w, map[string]string{
			"model": fmt.Sprintf("模型 %q 不在 ai.models 里", model),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)
	defer cancel()
	res, err := client.Optimize(ctx, req.Content, model)
	if err != nil {
		// 日志只记 host / model / 状态 / 耗时与错误摘要：绝不记正文与密钥
		slog.Error("optimize note", "host", client.Host(), "model", model, "error", err)
		writeOptimizeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, optimizeResponse{Content: res.Content, Model: res.Model})
}

// writeOptimizeError 把 internal/ai 的错误翻译成前端能区分处理的响应。
func writeOptimizeError(w http.ResponseWriter, err error) {
	var modelErr ai.ModelError
	var upstream ai.UpstreamError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "ai_timeout",
			"AI 接口超时（60 秒），可以重试或换一个模型")
	case errors.As(err, &modelErr):
		writeValidationError(w, map[string]string{"model": modelErr.Error()})
	case errors.As(err, &upstream):
		writeError(w, http.StatusBadGateway, "ai_upstream_failed", upstream.Error())
	case errors.Is(err, ai.ErrBadResponse), errors.Is(err, ai.ErrEmptyResult), errors.Is(err, ai.ErrTooLarge):
		writeError(w, http.StatusBadGateway, "ai_bad_response", "AI 接口返回了无法识别的内容")
	case errors.Is(err, ai.ErrNotConfigured):
		writeError(w, http.StatusConflict, "ai_not_configured", "AI 还没配置好，请先在信息页填写接口地址、密钥与模型")
	default:
		writeError(w, http.StatusBadGateway, "ai_upstream_failed", "调用 AI 接口失败，请稍后重试或检查接口地址")
	}
}

func containsModel(models []string, model string) bool {
	for _, candidate := range models {
		if candidate == model {
			return true
		}
	}
	return false
}
