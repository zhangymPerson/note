package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"note/internal/config"
	"note/internal/store"
)

func newTestHandler(t *testing.T) (http.Handler, config.Config) {
	t.Helper()
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>note</title>")},
		"assets/app.js": {Data: []byte("console.log('ok')")},
	}
	dir := t.TempDir()
	cfg := config.Config{
		Addr:       "127.0.0.1:8080",
		DBPath:     filepath.Join(dir, "notes.db"),
		ConfigPath: filepath.Join(dir, "note.yaml"),
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return New(cfg, st, dist), cfg
}

func doRequest(handler http.Handler, method, target, host, accept string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	req.Host = host
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestSPAFallbackAndSecurityHeaders(t *testing.T) {
	handler, _ := newTestHandler(t)
	rec := doRequest(handler, http.MethodGet, "/notes/demo", "127.0.0.1:8080", "text/html")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<!doctype html>") {
		t.Fatalf("expected index.html, got %q", rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store, got %q", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("missing CSP header: %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("unexpected X-Frame-Options: %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected X-Content-Type-Options: %q", got)
	}
}

func TestAssetsAreCachedAndMissingAssetsReturn404(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := doRequest(handler, http.MethodGet, "/assets/app.js", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected asset 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("expected immutable cache, got %q", got)
	}

	for _, target := range []string{"/assets/missing.js", "/assets/"} {
		rec := doRequest(handler, http.MethodGet, target, "127.0.0.1:8080", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: expected 404, got %d", target, rec.Code)
		}
	}
}

func TestUnknownAPIReturnsJSON404(t *testing.T) {
	handler, _ := newTestHandler(t)
	rec := doRequest(handler, http.MethodGet, "/api/whatever", "127.0.0.1:8080", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON content type, got %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected no-store, got %q", got)
	}

	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "not_found" {
		t.Fatalf("expected not_found, got %q", body.Error.Code)
	}
}

func TestCSRFIssuesAndReusesToken(t *testing.T) {
	handler, _ := newTestHandler(t)
	rec := doRequest(handler, http.MethodGet, "/api/csrf", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if !validCSRFToken(body.Token) {
		t.Fatalf("invalid token %q", body.Token)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != csrfCookieName || cookie.Value != body.Token {
		t.Fatalf("unexpected cookie: %+v", cookie)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
		t.Fatalf("unexpected cookie attributes: %+v", cookie)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/csrf", nil)
	req.Host = "127.0.0.1:8080"
	req.AddCookie(cookie)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, req)

	var reused struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &reused); err != nil {
		t.Fatalf("decode reused token: %v", err)
	}
	if reused.Token != body.Token {
		t.Fatalf("expected token reuse, got %q and %q", body.Token, reused.Token)
	}
	if len(second.Result().Cookies()) != 0 {
		t.Fatal("expected no new cookie when reusing token")
	}
}

func TestHostGuardRejectsUnknownHost(t *testing.T) {
	handler, _ := newTestHandler(t)
	rec := doRequest(handler, http.MethodGet, "/", "evil.example", "text/html")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}

	var body apiErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Error.Code != "host_invalid" {
		t.Fatalf("expected host_invalid, got %q", body.Error.Code)
	}
}

func TestNonHTMLPathWithExtensionReturns404(t *testing.T) {
	handler, _ := newTestHandler(t)
	rec := doRequest(handler, http.MethodGet, "/missing.txt", "127.0.0.1:8080", "application/json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestSettingsGateAndSetName(t *testing.T) {
	handler, cfg := newTestHandler(t)

	rec := doRequest(handler, http.MethodGet, "/api/settings", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var settings struct {
		Name    string `json:"name"`
		NameSet bool   `json:"name_set"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if settings.NameSet {
		t.Fatalf("expected unset name initially: %+v", settings)
	}

	setName := func(csrf string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/settings/name", strings.NewReader(`{"name":"阿澜"}`))
		req.Host = "127.0.0.1:8080"
		req.Header.Set("Content-Type", "application/json")
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
			req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: csrf})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := setName(""); rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf token, got %d", rec.Code)
	}

	tokenRec := doRequest(handler, http.MethodGet, "/api/csrf", "127.0.0.1:8080", "")
	var tokenBody struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(tokenRec.Body.Bytes(), &tokenBody); err != nil {
		t.Fatalf("decode csrf token: %v", err)
	}

	rec = setName(tokenBody.Token)
	if rec.Code != http.StatusOK {
		body, _ := io.ReadAll(rec.Body)
		t.Fatalf("expected 200 with csrf token, got %d: %s", rec.Code, body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode updated settings: %v", err)
	}
	if settings.Name != "阿澜" || !settings.NameSet {
		t.Fatalf("unexpected settings: %+v", settings)
	}

	reloaded, _, err := config.Load([]string{"--config", cfg.ConfigPath})
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.UserName != "阿澜" || !reloaded.NameSet {
		t.Fatalf("name not persisted to yaml: %+v", reloaded)
	}
}

func TestStatsAndSearchEndpoints(t *testing.T) {
	handler, _ := newTestHandler(t)

	rec := doRequest(handler, http.MethodGet, "/api/stats", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var stats struct {
		TotalSearches int `json:"total_searches"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	if stats.TotalSearches != 0 {
		t.Fatalf("expected zero searches, got %d", stats.TotalSearches)
	}

	rec = doRequest(handler, http.MethodGet, "/api/notes?q=%E6%B5%8B%E8%AF%95", "127.0.0.1:8080", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var items struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode search items: %v", err)
	}
	if len(items.Items) != 0 {
		t.Fatalf("expected empty result, got %v", items.Items)
	}

	rec = doRequest(handler, http.MethodGet, "/api/stats", "127.0.0.1:8080", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats after search: %v", err)
	}
	if stats.TotalSearches != 1 {
		t.Fatalf("expected search recorded, got %d", stats.TotalSearches)
	}
}

func TestMissingYamlUserSectionStillLoads(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "note.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  addr: 127.0.0.1:8080\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, _, err := config.Load([]string{"--config", cfgPath})
	if err != nil {
		t.Fatalf("load legacy config: %v", err)
	}
	if cfg.NameSet {
		t.Fatal("expected name_set false for legacy config")
	}
}
