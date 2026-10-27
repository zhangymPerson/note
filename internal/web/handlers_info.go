package web

import (
	"net/http"
	"os"
	"runtime"
	"time"

	"note/internal/build"
)

// handleGetInfo 汇报构建与运行态；信息页只读这些公开展示值，
// 响应里不出现任何加密材料（密文、盐、助记问题）。
func (app *application) handleGetInfo(w http.ResponseWriter, r *http.Request) {
	info := build.Describe()

	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "未知"
	}

	schemaVersion, err := app.store.SchemaVersion(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "读取数据库结构版本失败，请稍后重试")
		return
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	writeJSON(w, http.StatusOK, map[string]any{
		"version":         info.Version,
		"git_hash":        info.GitHash,
		"git_dirty":       info.GitDirty != "",
		"build_date":      info.BuildDate,
		"build_label":     info.Label(),
		"go_version":      info.GoVersion,
		"goos":            runtime.GOOS,
		"goarch":          runtime.GOARCH,
		"hostname":        hostname,
		"cpu_count":       runtime.NumCPU(),
		"mem_alloc_bytes": mem.Alloc,
		"mem_sys_bytes":   mem.Sys,
		"started_at":      app.startedAt.Format(time.RFC3339),
		"uptime_seconds":  int64(time.Since(app.startedAt).Seconds()),
		"db_path":         app.cfg.DBPath,
		"schema_version":  schemaVersion,
		"addr":            app.cfg.Addr,
		"allow_remote":    app.cfg.AllowRemote,
	})
}
