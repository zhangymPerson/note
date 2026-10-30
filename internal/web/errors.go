package web

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
)

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type apiErrorBody struct {
	Error apiError `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write json response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorBody{Error: apiError{Code: code, Message: message}})
}

// writeValidationError 返回 400 与字段级提示，前端据此标红对应输入框。
func writeValidationError(w http.ResponseWriter, fields map[string]string) {
	message := "请修正输入内容"
	for _, label := range []string{"title", "content", "base_url", "models", "prompt", "api_key", "cipher_b64", "kdf_params", "category_id", "id", "name", "salt_b64", "question", "q"} {
		if msg, ok := fields[label]; ok {
			message = msg
			break
		}
	}
	writeJSON(w, http.StatusBadRequest, apiErrorBody{Error: apiError{
		Code:    "validation_failed",
		Message: message,
		Fields:  fields,
	}})
}

// decodeJSON 读取并解析请求体，失败时已写好错误响应。
func decodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "body_invalid", "请求体不是有效的 JSON")
		return err
	}
	return nil
}
