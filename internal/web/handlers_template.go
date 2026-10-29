package web

import (
	"fmt"
	"log/slog"
	"net/http"
	"unicode/utf8"

	"note/internal/template"
)

// templateBody 是 PUT /api/template 的载荷：只有骨架正文，没有别的可改项。
type templateBody struct {
	Content string `json:"content"`
}

// templateResponse 统一 GET 与 PUT 的响应形状。default_content 给页面的
// 「恢复默认」按钮用，避免前端再维护一份默认骨架。
func (app *application) templateResponse(content string, fromDefault bool) map[string]any {
	source := "file"
	if fromDefault {
		source = "default"
	}
	return map[string]any{
		"content":         content,
		"path":            app.templatePath,
		"source":          source,
		"default_content": template.Default(),
	}
}

// handleGetTemplate 返回新建笔记的 markdown 骨架：文件读不到就回退内置默认值，
// 不让「模板文件被删/不可读」变成页面错误。
func (app *application) handleGetTemplate(w http.ResponseWriter, r *http.Request) {
	content, fromDefault, err := template.Load(app.templatePath)
	if err != nil {
		// 只记原因，不记模板内容（正文同级信息不进日志）。
		slog.Warn("read note template", "error", err)
	}
	writeJSON(w, http.StatusOK, app.templateResponse(content, fromDefault))
}

// handlePutTemplate 校验并原子写回骨架文件。
func (app *application) handlePutTemplate(w http.ResponseWriter, r *http.Request) {
	var body templateBody
	if err := decodeJSON(w, r, &body, 128<<10); err != nil {
		return
	}

	// 模板会原样变成笔记正文，所以上限必须与正文一致，
	// 否则用户新建一条就撞上 2000 字上限、还以为是编辑器坏了。
	if n := utf8.RuneCountInString(body.Content); n > maxContentRunes {
		writeValidationError(w, map[string]string{
			"content": fmt.Sprintf("模板请控制在 %d 字以内，当前 %d 字", maxContentRunes, n),
		})
		return
	}

	if err := template.Save(app.templatePath, body.Content); err != nil {
		slog.Error("save note template", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "无法写入模板文件，请检查配置目录是否可写")
		return
	}
	writeJSON(w, http.StatusOK, app.templateResponse(body.Content, false))
}
