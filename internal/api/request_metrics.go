package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var slowRequestThreshold = 500 * time.Millisecond
var requestDiagnostics = newRequestDiagnosticsBuffer(300)

type RequestDiagnosticEvent struct {
	Time       time.Time `json:"time"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Route      string    `json:"route"`
	Status     int       `json:"status"`
	Bytes      int       `json:"bytes"`
	DurationMS int64     `json:"duration_ms"`
	RemoteIP   string    `json:"remote_ip"`
}

type requestDiagnosticsBuffer struct {
	mu     sync.RWMutex
	limit  int
	events []RequestDiagnosticEvent
}

func newRequestDiagnosticsBuffer(limit int) *requestDiagnosticsBuffer {
	return &requestDiagnosticsBuffer{
		limit:  limit,
		events: make([]RequestDiagnosticEvent, 0, limit),
	}
}

func (b *requestDiagnosticsBuffer) record(event RequestDiagnosticEvent) {
	if b == nil || !shouldRecordRequestDiagnostic(event.Path) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == b.limit {
		copy(b.events, b.events[1:])
		b.events[len(b.events)-1] = event
		return
	}
	b.events = append(b.events, event)
}

func (b *requestDiagnosticsBuffer) snapshot() []RequestDiagnosticEvent {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	items := make([]RequestDiagnosticEvent, len(b.events))
	copy(items, b.events)
	return items
}

func (b *requestDiagnosticsBuffer) reset() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = b.events[:0]
}

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

		routePattern := ""
		if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
			routePattern = routeCtx.RoutePattern()
		}

		requestDiagnostics.record(RequestDiagnosticEvent{
			Time:       time.Now(),
			Method:     r.Method,
			Path:       r.URL.Path,
			Route:      routePattern,
			Status:     status,
			Bytes:      rec.bytes,
			DurationMS: duration.Milliseconds(),
			RemoteIP:   r.RemoteAddr,
		})

		if !shouldLogRequest(r.URL.Path, status, duration) {
			return
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

func shouldRecordRequestDiagnostic(path string) bool {
	if strings.HasPrefix(path, "/assets/") {
		return false
	}
	if path == "/" || strings.HasPrefix(path, "/reader/") || strings.HasPrefix(path, "/series/") {
		return false
	}
	return strings.HasPrefix(path, "/api/") ||
		strings.HasPrefix(path, "/opds/") ||
		strings.HasPrefix(path, "/koreader/") ||
		looksLikeKOReaderPath(path)
}

func looksLikeKOReaderPath(path string) bool {
	return strings.Contains(path, "/syncs/progress") ||
		strings.Contains(path, "/users/auth") ||
		strings.Contains(path, "/users/create") ||
		strings.Contains(path, "/healthcheck") ||
		strings.Contains(path, "/healthstatus")
}
