package web

import (
	"context"
	"encoding/json"
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
	// CustomPrompt 是本次的额外要求（编辑器里那一栏），只对这一次调用生效，
	// 不会写回 note.yaml 的 ai.prompt。
	CustomPrompt string `json:"custom_prompt"`
	// Stream 要求响应改成 text/event-stream：网页用它把「盯着转圈」变成「看着它写」。
	// 不传或 false 就是原来的 JSON 一次性响应（CLI 与其他调用方不受影响）。
	Stream bool `json:"stream"`
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
	// 自定义提示词只是本次的补充约束：上限比 ai.prompt（2000）小得多，
	// 校验口径由 internal/ai 统一给，web 层不另写一套。
	custom := strings.TrimSpace(req.CustomPrompt)
	if err := ai.ValidateCustomPrompt(custom); err != nil {
		fields["custom_prompt"] = err.Error()
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
	call := ai.Request{Content: req.Content, Model: model, CustomPrompt: custom}

	if req.Stream {
		app.streamOptimize(w, r, client, call)
		return
	}

	started := time.Now()
	client.Timeline = ai.NewTimeline()
	res, err := client.Optimize(ctx, call)
	if err != nil {
		logOptimizeFailure(ctx, r, "optimize note failed", client, call, false, started, nil, err)
		writeOptimizeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, optimizeResponse{Content: res.Content, Model: res.Model})
}

// streamOptimize 把上游的增量原样转成 SSE 事件推给浏览器：
//
//	event: delta  data: {"text":"…"}      —— 每收到一段就发一段
//	event: done   data: {"content":"…","model":"…"} —— 只有它才是「可以替换」
//	event: error  data: {"code":"…","message":"…"}  —— 已经开始流之后的失败
//
// 事件流一旦写了头就不能再改状态码，所以所有校验都在调用方先做完；
// 这里只在「一个 delta 都还没发」时把错误交回普通 JSON 响应。
func (app *application) streamOptimize(w http.ResponseWriter, r *http.Request, client *ai.Client, call ai.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "当前环境不支持流式响应")
		return
	}

	began := time.Now()
	var (
		started bool
		deltas  int
		sent    int
		firstAt time.Duration
	)
	writeHead := func() {
		if started {
			return
		}
		started = true
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		// 反向代理默认会攒满缓冲再转发，这一句让「边生成边看」真的成立。
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), optimizeTimeout)
	defer cancel()

	client.Timeline = ai.NewTimeline()
	res, err := client.OptimizeStream(ctx, call, func(delta string) error {
		if firstAt == 0 {
			firstAt = time.Since(began)
		}
		deltas++
		sent += len(delta)
		writeHead()
		return writeSSE(w, flusher, "delta", map[string]string{"text": delta})
	})
	if err != nil {
		extra := []any{
			"response_started", started,
			"deltas", deltas,
			"delta_bytes", sent,
			"first_delta_ms", millisOrOff(firstAt),
		}
		logOptimizeFailure(ctx, r, "optimize note stream failed", client, call, true, began, extra, err)
		if !started {
			writeOptimizeError(w, err)
			return
		}
		_, code, message, _ := classifyOptimizeError(err)
		_ = writeSSE(w, flusher, "error", map[string]string{"code": code, "message": message})
		return
	}
	writeHead()
	_ = writeSSE(w, flusher, "done", map[string]string{"content": res.Content, "model": res.Model})
}

// logOptimizeFailure 把一次失败调用能拿到的排查信息一次打全：
// 每一项入参（模型、端点、temperature、max_tokens、提示词与正文的规模与指纹、
// 密钥掩码、客户端超时配置）、网络各阶段耗时、SSE 进度、ctx 剩余时间与错误链。
//
// 硬约束：正文、渲染后的提示词、自定义提示词与 api_key 一律不落日志
// （见 AGENTS.md「AI 调用的日志」），所以它们只以 rune/byte 数与 sha256 前 12 位出现——
// 足够判断「两次是不是同一份输入」「提示词是不是被自定义要求改写过」。
func logOptimizeFailure(ctx context.Context, r *http.Request, msg string, client *ai.Client,
	call ai.Request, stream bool, started time.Time, extra []any, err error) {
	attrs := client.Describe(call, stream).Attrs()
	attrs = append(attrs, "elapsed_ms", time.Since(started).Milliseconds())
	if deadline, ok := ctx.Deadline(); ok {
		// 剩余预算为负说明已经超了：这条比「elapsed_ms」更能说明是超时还是瞬时失败。
		attrs = append(attrs, "deadline_in_ms", time.Until(deadline).Milliseconds())
	}
	// 用户点了「停止」或关掉页面时请求会被取消，这类错误不该被误读成上游故障。
	attrs = append(attrs, "client_gone", r.Context().Err() != nil)
	attrs = append(attrs, client.Timeline.Attrs()...)
	attrs = append(attrs, extra...)
	attrs = append(attrs, ai.ErrorAttrs(err)...)
	slog.Error(msg, attrs...)
}

// millisOrOff 让「一个 delta 都没收到」在日志里显示成 off 而不是 0 毫秒。
func millisOrOff(d time.Duration) any {
	if d == 0 {
		return "off"
	}
	return d.Milliseconds()
}

// writeSSE 发一个事件并立即 flush；写失败（浏览器关页、用户停止）就返回错误，
// 由调用链中止上游请求。
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeOptimizeError 把 internal/ai 的错误翻译成前端能区分处理的响应。
func writeOptimizeError(w http.ResponseWriter, err error) {
	status, code, message, fields := classifyOptimizeError(err)
	if fields != nil {
		// 模型名这类「改哪个输入框」的错误继续保持字段级提示。
		writeValidationError(w, fields)
		return
	}
	writeError(w, status, code, message)
}

// classifyOptimizeError 是错误映射的唯一出处：JSON 响应与 SSE 的 error 事件
// 都从这里取状态码与文案，免得两条路径对同一个错误说不一样的话。
func classifyOptimizeError(err error) (status int, code, message string, fields map[string]string) {
	var modelErr ai.ModelError
	var upstream ai.UpstreamError
	var timeoutErr ai.TimeoutError
	var truncated ai.TruncatedError
	switch {
	case errors.As(err, &timeoutErr):
		// 文案由 internal/ai 统一给（含模型名与时长），网页与 CLI 说的是同一句话。
		return http.StatusGatewayTimeout, "ai_timeout", timeoutErr.Error(), nil
	case errors.Is(err, context.DeadlineExceeded):
		// 兜底：ctx 到期但没有经过 wrapTransportError 或链式调用方自己的 deadline。
		return http.StatusGatewayTimeout, "ai_timeout",
			"AI 接口在规定时间内没有响应，可以稍后重试或换一个模型", nil
	case errors.As(err, &modelErr):
		return http.StatusBadRequest, "invalid_model", modelErr.Error(), map[string]string{"model": modelErr.Error()}
	case errors.As(err, &truncated):
		// 半截正文比报错更危险：明确告诉用户结果不完整。
		return http.StatusBadGateway, "ai_truncated", truncated.Error(), nil
	case errors.As(err, &upstream):
		return http.StatusBadGateway, "ai_upstream_failed", upstream.Error(), nil
	case errors.Is(err, ai.ErrBadResponse), errors.Is(err, ai.ErrEmptyResult), errors.Is(err, ai.ErrTooLarge):
		return http.StatusBadGateway, "ai_bad_response", "AI 接口返回了无法识别的内容", nil
	case errors.Is(err, ai.ErrNotConfigured):
		return http.StatusConflict, "ai_not_configured", "AI 还没配置好，请先在信息页填写接口地址、密钥与模型", nil
	default:
		return http.StatusBadGateway, "ai_upstream_failed", "调用 AI 接口失败，请稍后重试或检查接口地址", nil
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
