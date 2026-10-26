package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newTestHandler() http.Handler {
	dist := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>note</title>")},
		"assets/app.js": {Data: []byte("console.log('ok')")},
	}
	return New(Options{Addr: "127.0.0.1:8080"}, dist)
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
	rec := doRequest(newTestHandler(), http.MethodGet, "/notes/demo", "127.0.0.1:8080", "text/html")

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
	handler := newTestHandler()

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
	rec := doRequest(newTestHandler(), http.MethodGet, "/api/notes", "127.0.0.1:8080", "")

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
	handler := newTestHandler()
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
	rec := doRequest(newTestHandler(), http.MethodGet, "/", "evil.example", "text/html")
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
	rec := doRequest(newTestHandler(), http.MethodGet, "/missing.txt", "127.0.0.1:8080", "application/json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
