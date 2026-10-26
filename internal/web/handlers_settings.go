package web

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"note/internal/config"
	"note/internal/store"
)

type settingsState struct {
	mu      sync.Mutex
	name    string
	nameSet bool
}

// handleGetSettings 返回当前用户姓名与是否已设置，供前端决定引导流程。
func (app *application) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	app.settings.mu.Lock()
	defer app.settings.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     app.settings.name,
		"name_set": app.settings.nameSet,
	})
}

// handleSetName 校验并持久化姓名到配置文件。
func (app *application) handleSetName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &body, 4096); err != nil {
		return
	}

	name := strings.TrimSpace(body.Name)
	if msg := nameError(name); msg != "" {
		writeError(w, http.StatusBadRequest, "name_invalid", msg)
		return
	}

	if err := app.cfg.SaveUserName(name); err != nil {
		slog.Error("save user name", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "无法保存姓名，请检查配置文件是否可写")
		return
	}

	app.settings.mu.Lock()
	app.settings.name = name
	app.settings.nameSet = true
	app.settings.mu.Unlock()

	app.handleGetSettings(w, r)
}

// nameError 返回给用户的校验提示，合法时返回空串。
func nameError(name string) string {
	switch {
	case name == "":
		return "名字不能为空"
	case utf8.RuneCountInString(name) > 32:
		return "名字请控制在 32 个字以内"
	case strings.IndexFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0:
		return "名字不能包含控制字符"
	}
	return ""
}

type application struct {
	cfg      config.Config
	store    *store.Store
	settings settingsState
}

func newApplication(cfg config.Config, st *store.Store) *application {
	return &application{
		cfg:   cfg,
		store: st,
		settings: settingsState{
			name:    cfg.UserName,
			nameSet: cfg.NameSet,
		},
	}
}
