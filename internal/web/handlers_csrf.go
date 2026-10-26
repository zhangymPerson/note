package web

import (
	"crypto/rand"
	"encoding/base64"
	"log/slog"
	"net/http"
)

const (
	csrfCookieName = "csrf"
	csrfTokenBytes = 32
)

func handleCSRF(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && validCSRFToken(cookie.Value) {
		writeJSON(w, http.StatusOK, map[string]string{"token": cookie.Value})
		return
	}

	token, err := newCSRFToken()
	if err != nil {
		slog.Error("generate csrf token", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func newCSRFToken() (string, error) {
	buf := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func validCSRFToken(token string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(decoded) == csrfTokenBytes
}
