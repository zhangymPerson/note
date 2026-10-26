package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type apiErrorBody struct {
	Error apiError `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Error("write json response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, apiErrorBody{Error: apiError{Code: code, Message: message}})
}
