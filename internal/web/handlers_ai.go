package web

import (
	"errors"
	"log/slog"
	"net/http"

	"note/internal/config"
)

// aiConfigMaxBytes 是 PUT /api/ai-config 的请求体上限：
// 提示词上限 2000 字（UTF-8 最多 8000 字节），留一倍余量。
const aiConfigMaxBytes = 16 << 10

// aiConfigResponse 是 GET/PUT 的响应体。密钥永远只出现在 api_key_masked 里，
// 明文既不进响应也不进日志（安全不变量见 docs/TECH_DESIGN.md 1.2）。
type aiConfigResponse struct {
	BaseURL      string `json:"base_url"`
	Prompt       string `json:"prompt"`
	APIKeySet    bool   `json:"api_key_set"`
	APIKeyMasked string `json:"api_key_masked"`
	Path         string `json:"path"`
}

// aiConfigRequest 是 PUT 的请求体。api_key 用指针区分「不动它」（省略/null）
// 与「清空它」（空串）：界面上密钥框留空表示沿用已有的，点清除才真的抹掉。
type aiConfigRequest struct {
	BaseURL string  `json:"base_url"`
	Prompt  string  `json:"prompt"`
	APIKey  *string `json:"api_key"`
}

// handleGetAIConfig 读取配置文件的 AI 三项。每次都在磁盘上读：
// 手改 note.yaml 之后刷新页面就能看到，不存在内存与文件不一致的窗口。
func (app *application) handleGetAIConfig(w http.ResponseWriter, r *http.Request) {
	ai, _, err := config.ReadAI(app.cfg.ConfigPath)
	if err != nil {
		slog.Error("read ai config", "path", app.cfg.ConfigPath, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "读取配置文件失败，请检查 note.yaml 的格式")
		return
	}
	writeJSON(w, http.StatusOK, newAIConfigResponse(app.cfg.ConfigPath, ai))
}

// handlePutAIConfig 写入 AI 三项：先把每一项都校验一遍，再一次性原子落盘，
// 免得「地址写进去了、密钥写失败」这种半截状态。
func (app *application) handlePutAIConfig(w http.ResponseWriter, r *http.Request) {
	var req aiConfigRequest
	if err := decodeJSON(w, r, &req, aiConfigMaxBytes); err != nil {
		return
	}

	// 与 config.SetValues 用同一套归一化，免得同一个值在命令行能写、在网页却报错
	baseURL := config.Normalize("ai.base_url", req.BaseURL)
	prompt := config.Normalize("ai.prompt", req.Prompt)

	fields := map[string]string{}
	if err := config.ValidateBaseURL(baseURL); err != nil {
		fields["base_url"] = err.Error()
	}
	if err := config.ValidatePrompt(prompt); err != nil {
		fields["prompt"] = err.Error()
	}

	values := []config.KeyValue{
		{Key: "ai.base_url", Value: baseURL},
		{Key: "ai.prompt", Value: prompt},
	}
	if req.APIKey != nil {
		apiKey := config.Normalize("ai.api_key", *req.APIKey)
		if err := config.ValidateAPIKey(apiKey); err != nil {
			fields["api_key"] = err.Error()
		}
		values = append(values, config.KeyValue{Key: "ai.api_key", Value: apiKey})
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	if err := config.SetValues(app.cfg.ConfigPath, values); err != nil {
		// 密钥不在这里出现：日志只记键名与失败原因
		slog.Error("save ai config", "path", app.cfg.ConfigPath, "error", err)
		var invalid config.ValueError
		if errors.As(err, &invalid) {
			writeError(w, http.StatusBadRequest, "validation_failed", invalid.Error())
			return
		}
		if errors.Is(err, config.ErrUnknownKey) {
			writeError(w, http.StatusConflict, "config_invalid",
				"配置文件里有无法识别的键，请先手动修正 note.yaml 再保存")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "写入配置文件失败，请检查 note.yaml 是否可写")
		return
	}

	ai, _, err := config.ReadAI(app.cfg.ConfigPath)
	if err != nil {
		slog.Error("re-read ai config", "path", app.cfg.ConfigPath, "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "配置已写入，但读取回来时失败，请刷新页面确认")
		return
	}
	writeJSON(w, http.StatusOK, newAIConfigResponse(app.cfg.ConfigPath, ai))
}

func newAIConfigResponse(path string, ai config.AI) aiConfigResponse {
	return aiConfigResponse{
		BaseURL:      ai.BaseURL,
		Prompt:       ai.Prompt,
		APIKeySet:    ai.APIKey != "",
		APIKeyMasked: config.MaskSecret(ai.APIKey),
		Path:         path,
	}
}
