package web

import (
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

type Options struct {
	Addr        string
	AllowRemote bool
}

func New(opts Options, dist fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/csrf", handleCSRF)
	mux.Handle("GET /assets/", serveAssets(dist))
	mux.HandleFunc("GET /", spaHandler(dist))

	var handler http.Handler = mux
	handler = recoverPanics(handler)
	handler = hostGuard(opts.Addr, opts.AllowRemote, handler)
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
