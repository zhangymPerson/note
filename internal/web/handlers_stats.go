package web

import (
	"net/http"
	"strconv"
)

const statsLookbackDays = 370

// handleStats 返回首页使用统计：累计事件数、按类型的事件构成、Top 查询词与近一年按天计数。
// 按天计数与活跃天数覆盖全部事件（search / note_* / category_*），Top 查询词仍只统计 search。
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
