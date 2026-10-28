package web

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
)

const (
	// shutdownPath 是后台模式的关停端点。它对 CSRF 有意豁免（凭证即令牌），
	// 因此 middleware.go 里只放行这一个路径，不得扩大成「写请求通用豁免」。
	shutdownPath = "/api/shutdown"
	// shutdownTokenHeader 与 internal/daemon.ShutdownTokenHeader 同名。
	shutdownTokenHeader = "X-Shutdown-Token"
)

// Control 是后台模式的运行控制面：令牌为空表示未启用（前台模式），
// 此时关停端点一律 403，普通用户与浏览器页面都碰不到关停能力。
type Control struct {
	// Token 由后台子进程自己生成并写入状态文件，父进程全程不经手。
	Token string
	// Shutdown 命中后异步调用，负责触发优雅关停（服务与数据库收尾）。
	Shutdown func()
}

func (app *application) handleShutdown(w http.ResponseWriter, r *http.Request) {
	// 按路径注册全部方法（而不是只注册 POST），错误方法才能拿到 405
	// 而不是被 SPA 回退吞成 404——与其它 API 的语义保持一致。
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "关停接口只接受 POST")
		return
	}
	if app.control.Token == "" {
		writeError(w, http.StatusForbidden, "shutdown_disabled", "后台模式未启用关停接口")
		return
	}
	got := r.Header.Get(shutdownTokenHeader)
	if subtle.ConstantTimeCompare([]byte(got), []byte(app.control.Token)) != 1 {
		writeError(w, http.StatusForbidden, "shutdown_denied", "关停令牌无效")
		return
	}

	// 只记录来源地址：令牌、请求体一概不进日志。
	slog.Info("shutdown requested", "remote", r.RemoteAddr)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "shutting_down"})

	if app.control.Shutdown == nil {
		slog.Warn("shutdown requested but no shutdown handler wired")
		return
	}
	go app.control.Shutdown()
}
