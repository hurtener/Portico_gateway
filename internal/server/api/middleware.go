package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// slogRequestLogger logs each HTTP request as a structured slog event.
func slogRequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", chimw.GetReqID(r.Context()),
				"remote", r.RemoteAddr,
			)
		})
	}
}

// notFoundHandler returns a JSON 404.
func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusNotFound, "not_found", "no route", map[string]any{"path": r.URL.Path})
}

// methodNotAllowedHandler returns a JSON 405.
func methodNotAllowedHandler(w http.ResponseWriter, r *http.Request) {
	writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed",
		map[string]any{"method": r.Method, "path": r.URL.Path})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, code, msg string, details map[string]any) {
	body := map[string]any{
		"error":   code,
		"message": msg,
	}
	if details != nil {
		body["details"] = details
	}
	writeJSON(w, status, body)
}
