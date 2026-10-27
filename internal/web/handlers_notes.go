package web

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"note/internal/store"
)

const (
	// 笔记请求体上限：密文 64 KiB + 元数据，超限直接 413，不静默截断。
	maxNoteBody = 128 << 10

	maxTitleRunes   = 200
	maxContentRunes = 2000
	minCipherBytes  = 29
	maxCipherBytes  = 65536

	kdfAlgo   = "pbkdf2"
	minKDFIter = 100_000
	maxKDFIter = 1_000_000
	kdfKeyLen  = 32
)

var noteIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// noteRequestBody 是 POST/PUT /api/notes 的载荷；kdf_params 内含一段 JSON 文本。
type noteRequestBody struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Content    *string `json:"content"`
	CipherB64  string  `json:"cipher_b64"`
	KDFParams  string  `json:"kdf_params"`
	CategoryID *int64  `json:"category_id"`
}

func (b noteRequestBody) toInput() store.NoteInput {
	return store.NoteInput{
		ID:         strings.TrimSpace(b.ID),
		Title:      b.Title,
		Content:    b.Content,
		CipherB64:  b.CipherB64,
		KDFParams:  b.KDFParams,
		CategoryID: b.CategoryID,
	}
}

// validateNoteRequest 返回字段级错误；为空表示载荷合法。
func validateNoteRequest(b noteRequestBody) map[string]string {
	fields := map[string]string{}

	title := strings.TrimSpace(b.Title)
	switch {
	case title == "":
		fields["title"] = "标题不能为空"
	case utf8.RuneCountInString(title) > maxTitleRunes:
		fields["title"] = "标题请控制在 200 个字以内"
	}

	hasPlaintext := b.Content != nil
	hasCipher := strings.TrimSpace(b.CipherB64) != ""
	switch {
	case !hasPlaintext && !hasCipher:
		fields["content"] = "请至少填写正文内容，或先加密正文"
	case hasPlaintext && hasCipher:
		fields["content"] = "明文正文与密文不能同时提交"
	case hasPlaintext && utf8.RuneCountInString(*b.Content) > maxContentRunes:
		fields["content"] = "正文请控制在 2000 个字符以内"
	}

	if b.ID != "" && !noteIDPattern.MatchString(b.ID) {
		fields["id"] = "笔记 id 不是合法的 UUID"
	}

	if hasCipher && !hasPlaintext {
		raw, err := base64.StdEncoding.DecodeString(b.CipherB64)
		switch {
		case err != nil:
			fields["cipher_b64"] = "密文不是合法的 base64"
		case len(raw) < minCipherBytes || len(raw) > maxCipherBytes:
			fields["cipher_b64"] = "密文长度需在 29 到 65536 字节之间"
		}
		if msg := kdfParamsError(b.KDFParams); msg != "" {
			fields["kdf_params"] = msg
		}
	}
	return fields
}

// kdfParamsError 校验 KDF 参数：只接受 pbkdf2、合理迭代次数与 32 字节密钥。
func kdfParamsError(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "加密笔记必须携带 kdf_params"
	}
	var params struct {
		Algo   string `json:"algo"`
		PRF    string `json:"prf"`
		Iter   int    `json:"iter"`
		KeyLen int    `json:"keylen"`
	}
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return "kdf_params 不是有效的 JSON"
	}
	if params.Algo != kdfAlgo {
		return "只支持 pbkdf2 密钥派生"
	}
	if params.Iter < minKDFIter || params.Iter > maxKDFIter {
		return "kdf_params.iter 超出允许范围"
	}
	if params.KeyLen != kdfKeyLen {
		return "kdf_params.keylen 必须为 32"
	}
	return ""
}

// decodeNoteBody 读取笔记请求体，超限时返回 413。
func decodeNoteBody(w http.ResponseWriter, r *http.Request, target *noteRequestBody) bool {
	if r.ContentLength > maxNoteBody {
		writeError(w, http.StatusRequestEntityTooLarge, "body_too_large", "请求体过大")
		return false
	}
	return decodeJSON(w, r, target, maxNoteBody) == nil
}

// writeNoteStoreError 把 store 层错误映射为语义化 HTTP 响应。
func writeNoteStoreError(w http.ResponseWriter, err error, fields map[string]string) {
	switch {
	case errors.Is(err, store.ErrNoteNotFound):
		writeError(w, http.StatusNotFound, "not_found", "笔记不存在")
	case errors.Is(err, store.ErrMasterPasswordMissing):
		writeError(w, http.StatusConflict, "master_password_missing", "请先设置主密码再保存加密笔记")
	case errors.Is(err, store.ErrEncryptedImmutable):
		writeError(w, http.StatusConflict, "encrypted_immutable", "加密笔记不能改回明文")
	case errors.Is(err, store.ErrInvalidNote):
		if len(fields) == 0 {
			fields = map[string]string{"content": "正文与密文必须二选一"}
		}
		writeValidationError(w, fields)
	case isForeignKeyError(err):
		writeValidationError(w, map[string]string{"category_id": "分类不存在"})
	case isUniqueError(err):
		writeError(w, http.StatusConflict, "id_conflict", "该笔记 id 已存在，请刷新后重试")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "保存笔记失败，请稍后重试")
	}
}

func (app *application) handleListNotes(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	page := atoiOr(query.Get("page"), 1)
	categoryID := atoiOr(query.Get("category"), 0)

	result, err := app.store.ListNotes(r.Context(), query.Get("q"), int64(categoryID), page)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "查询失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (app *application) handleCreateNote(w http.ResponseWriter, r *http.Request) {
	var body noteRequestBody
	if !decodeNoteBody(w, r, &body) {
		return
	}
	fields := validateNoteRequest(body)
	if !app.categoryExists(w, r, body.CategoryID, fields) {
		return
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	note, err := app.store.CreateNote(r.Context(), body.toInput())
	if err != nil {
		writeNoteStoreError(w, err, fields)
		return
	}
	writeJSON(w, http.StatusCreated, note)
}

func (app *application) handleGetNote(w http.ResponseWriter, r *http.Request) {
	note, err := app.store.GetNote(r.Context(), r.PathValue("id"))
	if err != nil {
		writeNoteStoreError(w, err, nil)
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (app *application) handleUpdateNote(w http.ResponseWriter, r *http.Request) {
	var body noteRequestBody
	if !decodeNoteBody(w, r, &body) {
		return
	}
	fields := validateNoteRequest(body)
	if !app.categoryExists(w, r, body.CategoryID, fields) {
		return
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	note, err := app.store.UpdateNote(r.Context(), r.PathValue("id"), body.toInput())
	if err != nil {
		writeNoteStoreError(w, err, fields)
		return
	}
	writeJSON(w, http.StatusOK, note)
}

func (app *application) handleDeleteNote(w http.ResponseWriter, r *http.Request) {
	if err := app.store.DeleteNote(r.Context(), r.PathValue("id")); err != nil {
		writeNoteStoreError(w, err, nil)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// categoryExists 校验 category_id 指向存在的分类，不存在时写入字段错误。
func (app *application) categoryExists(w http.ResponseWriter, r *http.Request, id *int64, fields map[string]string) bool {
	if id == nil {
		return true
	}
	cats, err := app.store.ListCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "读取分类失败，请稍后重试")
		return false
	}
	for _, cat := range cats {
		if cat.ID == *id {
			return true
		}
	}
	fields["category_id"] = "分类不存在"
	writeValidationError(w, fields)
	return false
}

func atoiOr(raw string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func isUniqueError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isForeignKeyError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
