package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// problem is an RFC 7807 error body.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type: "about:blank", Title: http.StatusText(status), Status: status, Detail: detail,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// serverError logs the cause and answers with a generic 500, so internal
// details never reach the client.
func serverError(w http.ResponseWriter, log *slog.Logger, msg string, err error) {
	log.Error(msg, "error", err)
	writeProblem(w, http.StatusInternalServerError, "")
}
