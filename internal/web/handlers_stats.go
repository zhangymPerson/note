package web

import (
	"net/http"
	"strconv"
)

const statsLookbackDays = 370

// handleSearchNotes 模糊查询笔记；非空查询会记录一次使用活动。
func (app *application) handleSearchNotes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	hits, err := app.store.SearchNotes(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "查询失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

// handleStats 返回首页使用统计：总查询次数、Top 查询词与近一年按天计数。
func (app *application) handleStats(w http.ResponseWriter, r *http.Request) {
	days := statsLookbackDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 3650 {
			days = parsed
		}
	}

	stats, err := app.store.GetStats(r.Context(), days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "统计加载失败，请稍后重试")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
