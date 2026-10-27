package web

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"note/internal/store"
)

const (
	maxCategoryNameRunes = 30
	masterSaltBytes      = 16
)

// handleListCategories 返回全部分类及每个分类的笔记数。
func (app *application) handleListCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := app.store.ListCategories(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "读取分类失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, cats)
}

type categoryBody struct {
	Name string `json:"name"`
}

func (app *application) handleCreateCategory(w http.ResponseWriter, r *http.Request) {
	var body categoryBody
	if err := decodeJSON(w, r, &body, 4096); err != nil {
		return
	}
	fields := validateCategoryName(body.Name)
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	created, err := app.store.CreateCategory(r.Context(), strings.TrimSpace(body.Name))
	if err != nil {
		writeCategoryError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (app *application) handleRenameCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := categoryID(w, r)
	if !ok {
		return
	}
	var body categoryBody
	if err := decodeJSON(w, r, &body, 4096); err != nil {
		return
	}
	fields := validateCategoryName(body.Name)
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	if err := app.store.RenameCategory(r.Context(), id, strings.TrimSpace(body.Name)); err != nil {
		writeCategoryError(w, err)
		return
	}
	app.handleListCategories(w, r)
}

// handleDeleteCategory 删除空分类；仍有笔记时返回 409 并带上笔记数。
func (app *application) handleDeleteCategory(w http.ResponseWriter, r *http.Request) {
	id, ok := categoryID(w, r)
	if !ok {
		return
	}
	if err := app.store.DeleteCategory(r.Context(), id); err != nil {
		var notEmpty store.ErrCategoryNotEmpty
		if errors.As(err, &notEmpty) {
			writeJSON(w, http.StatusConflict, apiErrorBody{Error: apiError{
				Code:    "category_not_empty",
				Message: notEmpty.Error(),
				Fields:  map[string]string{"note_count": strconv.Itoa(notEmpty.Count)},
			}})
			return
		}
		writeCategoryError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func validateCategoryName(name string) map[string]string {
	trimmed := strings.TrimSpace(name)
	switch {
	case trimmed == "":
		return map[string]string{"name": "分类名不能为空"}
	case utf8.RuneCountInString(trimmed) > maxCategoryNameRunes:
		return map[string]string{"name": "分类名请控制在 30 个字以内"}
	}
	return nil
}

func writeCategoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrCategoryExists):
		// 撞名是状态冲突（与 category_not_empty 同族），不是输入格式错误：409 + fields.name
		// 让前端仍能把提示标在输入框上。
		writeJSON(w, http.StatusConflict, apiErrorBody{Error: apiError{
			Code:    "category_exists",
			Message: "该分类名已存在",
			Fields:  map[string]string{"name": "该分类名已存在"},
		}})
	case errors.Is(err, store.ErrReservedCategory):
		writeError(w, http.StatusConflict, "category_reserved", "默认分类不可改名或删除")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "分类不存在")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "保存分类失败，请稍后重试")
	}
}

func categoryID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeValidationError(w, map[string]string{"id": "分类 id 不合法"})
		return 0, false
	}
	return id, true
}

// handleGetMasterPassword 暴露公开配置：是否已设置、派生密钥用的盐、助记问题。
// 盐是公开 KDF 参数，浏览器必须拿到它才能用主密码派生 AES 密钥；密码原文永不经过服务端。
func (app *application) handleGetMasterPassword(w http.ResponseWriter, r *http.Request) {
	master, configured, err := app.store.GetMasterPassword(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "读取主密码配置失败，请稍后重试")
		return
	}
	if !configured {
		writeJSON(w, http.StatusOK, map[string]any{"configured": false, "salt_b64": "", "question": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"configured": true,
		"salt_b64":   master.SaltB64,
		"question":   master.Question,
	})
}

// handleSetMasterPassword 一次性写入盐与助记问题；密码原文永不离开浏览器。
func (app *application) handleSetMasterPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SaltB64  string `json:"salt_b64"`
		Question string `json:"question"`
	}
	if err := decodeJSON(w, r, &body, 4096); err != nil {
		return
	}

	fields := map[string]string{}
	salt, err := base64.StdEncoding.DecodeString(body.SaltB64)
	switch {
	case err != nil:
		fields["salt_b64"] = "盐值不是合法的 base64"
	case len(salt) != masterSaltBytes:
		fields["salt_b64"] = "盐值必须为 16 字节"
	}
	question := strings.TrimSpace(body.Question)
	if question == "" {
		fields["question"] = "请填写助记问题"
	} else if utf8.RuneCountInString(question) > 100 {
		fields["question"] = "助记问题请控制在 100 个字以内"
	}
	if len(fields) > 0 {
		writeValidationError(w, fields)
		return
	}

	if _, err := app.store.SetMasterPassword(r.Context(), salt, question); err != nil {
		if errors.Is(err, store.ErrMasterPasswordExists) {
			writeError(w, http.StatusConflict, "master_password_exists", "主密码已设置，无法再次修改")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", "保存主密码配置失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"configured": true, "question": question})
}
