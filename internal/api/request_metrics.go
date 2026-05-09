package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var slowRequestThreshold = 500 * time.Millisecond

type metricsResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *metricsResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *metricsResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(data)
	w.bytes += n
	return n, err
}

func (w *metricsResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// RequestMetrics records structured request timings for API and protocol traffic.
func RequestMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rec := &metricsResponseWriter{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started)
		if !shouldLogRequest(r.URL.Path, status, duration) {
			return
		}

		routePattern := ""
		if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
			routePattern = routeCtx.RoutePattern()
		}

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"route", routePattern,
			"status", status,
			"bytes", rec.bytes,
			"duration_ms", duration.Milliseconds(),
			"remote_ip", r.RemoteAddr,
		}
		if requestID := middleware.GetReqID(r.Context()); requestID != "" {
			attrs = append(attrs, "request_id", requestID)
		}
		if length := r.Header.Get("Content-Length"); length != "" {
			if parsed, err := strconv.ParseInt(length, 10, 64); err == nil {
				attrs = append(attrs, "request_bytes", parsed)
			}
		}

		switch {
		case status >= 500:
			slog.Error("HTTP request completed", attrs...)
		case status >= 400 || duration >= slowRequestThreshold:
			slog.Warn("HTTP request completed", attrs...)
		default:
			slog.Info("HTTP request completed", attrs...)
		}
	})
}

func shouldLogRequest(path string, status int, duration time.Duration) bool {
	if status >= 400 || duration >= slowRequestThreshold {
		return true
	}
	return strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/opds/") ||
		strings.HasPrefix(path, "/koreader/")
}
