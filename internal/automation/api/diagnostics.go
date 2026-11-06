package api

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"
)

// Log only server-owned identifiers and fixed outcome codes. Paths, headers,
// bodies and raw errors can contain credentials or user data.
type response struct {
	http.ResponseWriter
	id, endpoint, route, jobID, code string
	status                           int
}

func (w *response) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *response) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	result := &response{ResponseWriter: w, id: rand.Text(), endpoint: "unknown"}
	w.Header().Set("X-Request-ID", result.id)
	defer func() {
		status := result.status
		if status == 0 {
			status = 500
		}
		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		h.logger.Log(r.Context(), level, "workflow HTTP request",
			"request_id", result.id, "endpoint", result.endpoint, "route", result.route,
			"status", status, "code", result.code, "job_id", result.jobID, "duration_ms", time.Since(started).Milliseconds())
	}()
	h.serveHTTP(result, r)
}
