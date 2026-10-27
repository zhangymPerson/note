package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"note/internal/store"
)

// handleListEncryptedCiphers 一次给出全部加密笔记的密文与派生参数，
// 供浏览器端改密时逐条重加密；不含标题与正文，避免 N+1 拉取。
func (app *application) handleListEncryptedCiphers(w http.ResponseWriter, r *http.Request) {
	items, err := app.store.ListEncryptedCiphers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "读取加密笔记清单失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"notes": items})
}

// cipherRequestBody 是 PUT /api/notes/{id}/cipher 的载荷：只接受密文与派生参数。
type cipherRequestBody struct {
	CipherB64 string `json:"cipher_b64"`
	KDFParams string `json:"kdf_params"`
}

// validateCipherRequest 与 validateNoteRequest 的密文分支同值，只是不再要求标题与正文。
func validateCipherRequest(b cipherRequestBody) map[string]string {
	fields := map[string]string{}

	raw, err := base64.StdEncoding.DecodeString(b.CipherB64)
	switch {
	case strings.TrimSpace(b.CipherB64) == "":
		fields["cipher_b64"] = "必须提交密文"
	case err != nil:
		fields["cipher_b64"] = "密文不是合法的 base64"
	case len(raw) < minCipherBytes || len(raw) > maxCipherBytes:
		fields["cipher_b64"] = "密文长度需在 29 到 65536 字节之间"
	}
	if msg := kdfParamsError(b.KDFParams); msg != "" {
		fields["kdf_params"] = msg
	}
	return fields
}

// handleUpdateNoteCipher 替换单条笔记的密文；服务端不解释密文，也不碰标题与修订号。
func (app *application) handleUpdateNoteCipher(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > maxNoteBody {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "请求体过大")
		return
	}
	var body cipherRequestBody
	if err := decodeJSON(w, r, &body, maxNoteBody); err != nil {
		return
	}
	fields := validateCipherRequest(body)
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	note, err := app.store.UpdateNoteCipher(r.Context(), r.PathValue("id"), body.CipherB64, body.KDFParams)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrNoteNotFound):
			writeError(w, http.StatusNotFound, "not_found", "笔记不存在")
		case errors.Is(err, store.ErrNotEncrypted):
			writeError(w, http.StatusConflict, "not_encrypted", "这条笔记不是加密笔记")
		case errors.Is(err, store.ErrMasterPasswordMissing):
			writeError(w, http.StatusConflict, "master_password_missing", "请先设置主密码再保存加密笔记")
		case errors.Is(err, store.ErrInvalidNote):
			writeValidationError(w, fields)
		default:
			writeError(w, http.StatusInternalServerError, "internal", "更新密文失败，请稍后重试")
		}
		return
	}
	writeJSON(w, http.StatusOK, note)
}
