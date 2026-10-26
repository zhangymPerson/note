package web

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// csrfProtect 对所有状态变更类 API 请求做双提交校验：
// X-CSRF-Token 头必须与 csrf Cookie 一致，否则返回 403。
func csrfProtect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(csrfCookieName)
		token := r.Header.Get("X-CSRF-Token")
		if err != nil || cookie.Value == "" || token == "" ||
			!validCSRFToken(cookie.Value) ||
			subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(token)) != 1 {
			writeError(w, http.StatusForbidden, "csrf_invalid", "缺少或无效的 CSRF 令牌，请重新打开页面")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				slog.Error("panic recovered", "error", fmt.Sprint(value), "method", r.Method, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	const policy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func hostGuard(listenAddr string, allowRemote bool, next http.Handler) http.Handler {
	allowed := map[string]struct{}{
		"127.0.0.1": {},
		"localhost": {},
		"::1":       {},
	}
	if allowRemote {
		if host, _, err := net.SplitHostPort(listenAddr); err == nil {
			allowed[strings.ToLower(host)] = struct{}{}
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		host = strings.Trim(host, "[]")
		if _, ok := allowed[strings.ToLower(host)]; !ok {
			writeError(w, http.StatusBadRequest, "host_invalid", "请求主机不被允许")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	written, err := w.ResponseWriter.Write(data)
	w.bytes += written
	return written, err
}

func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		writer := &statusWriter{ResponseWriter: w}
		defer func() {
			if writer.status == 0 {
				writer.status = http.StatusOK
			}
			slog.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", writer.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"bytes", writer.bytes,
			)
		}()
		next.ServeHTTP(writer, r)
	})
}
