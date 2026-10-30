package web

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"note/internal/config"
	"note/internal/store"
)

// New 装配前台模式的路由（无关停端点，行为与引入后台模式前完全一致）。
func New(cfg config.Config, st *store.Store, dist fs.FS) http.Handler {
	return NewWithControl(cfg, st, dist, Control{})
}

// NewWithControl 装配路由；ctl.Token 非空时启用 POST /api/shutdown，
// 供 note web stop 优雅停止后台实例。
func NewWithControl(cfg config.Config, st *store.Store, dist fs.FS, ctl Control) http.Handler {
	app := newApplication(cfg, st, ctl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/csrf", handleCSRF)
	// 关停端点显式登记两种方法：只登记 POST 的话，GET 会被 `GET /` 的 SPA
	// 回退吞成 404；显式登记后错误方法统一得到 405 + Allow: POST。
	mux.HandleFunc("POST "+shutdownPath, app.handleShutdown)
	mux.HandleFunc("GET "+shutdownPath, app.handleShutdown)
	mux.HandleFunc("GET /api/info", app.handleGetInfo)
	mux.HandleFunc("GET /api/settings", app.handleGetSettings)
	mux.HandleFunc("PUT /api/settings/name", app.handleSetName)
	// 新建笔记的 markdown 骨架：文件在 note.yaml 同目录，GET 只读、PUT 需 CSRF。
	mux.HandleFunc("GET /api/template", app.handleGetTemplate)
	mux.HandleFunc("PUT /api/template", app.handlePutTemplate)
	// OpenAI 兼容接口的三项配置：GET 只回密钥掩码，PUT 原子写入 note.yaml。
	mux.HandleFunc("GET /api/ai-config", app.handleGetAIConfig)
	mux.HandleFunc("PUT /api/ai-config", app.handlePutAIConfig)
	// 用 AI 润色正文：服务端代发（密钥不出机器），结果只回前端预览，不落库。
	mux.HandleFunc("POST /api/ai/optimize", app.handleOptimizeNote)
	mux.HandleFunc("GET /api/notes", app.handleListNotes)
	mux.HandleFunc("POST /api/notes", app.handleCreateNote)
	mux.HandleFunc("GET /api/notes/encrypted", app.handleListEncryptedCiphers)
	mux.HandleFunc("GET /api/notes/{id}", app.handleGetNote)
	mux.HandleFunc("PUT /api/notes/{id}", app.handleUpdateNote)
	mux.HandleFunc("PUT /api/notes/{id}/cipher", app.handleUpdateNoteCipher)
	mux.HandleFunc("DELETE /api/notes/{id}", app.handleDeleteNote)
	mux.HandleFunc("POST /api/notes/{id}/restore", app.handleRestoreNote)
	mux.HandleFunc("GET /api/categories", app.handleListCategories)
	mux.HandleFunc("POST /api/categories", app.handleCreateCategory)
	mux.HandleFunc("PUT /api/categories/{id}", app.handleRenameCategory)
	mux.HandleFunc("DELETE /api/categories/{id}", app.handleDeleteCategory)
	mux.HandleFunc("GET /api/master-password", app.handleGetMasterPassword)
	mux.HandleFunc("POST /api/master-password", app.handleSetMasterPassword)
	mux.HandleFunc("PUT /api/master-password/question", app.handleUpdateMasterQuestion)
	mux.HandleFunc("GET /api/stats", app.handleStats)
	mux.Handle("GET /assets/", serveAssets(dist))
	mux.HandleFunc("GET /", spaHandler(dist))

	var handler http.Handler = mux
	handler = recoverPanics(handler)
	handler = csrfProtect(handler)
	handler = hostGuard(cfg.Addr, cfg.AllowRemote, handler)
	handler = securityHeaders(handler)
	handler = accessLog(handler)
	return handler
}

func serveAssets(dist fs.FS) http.Handler {
	files := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if !fs.ValidPath(name) || strings.HasSuffix(name, "/") {
			http.NotFound(w, r)
			return
		}

		file, err := dist.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		info, statErr := file.Stat()
		file.Close()
		if statErr != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		files.ServeHTTP(w, r)
	})
}

func spaHandler(dist fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found", "接口不存在")
			return
		}
		if !wantsHTML(r) {
			http.NotFound(w, r)
			return
		}

		index, err := fs.ReadFile(dist, "index.html")
		if err != nil {
			slog.Error("read embedded index.html", "error", err)
			http.Error(w, "服务器内部错误", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(index); err != nil {
			slog.Error("write index.html", "error", err)
		}
	}
}

func wantsHTML(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		return true
	}
	return !strings.Contains(path.Base(r.URL.Path), ".")
}
