package api

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type captureLogHandler struct {
	records []slog.Record
}

func (h *captureLogHandler) Enabled(context.Context, slog.Level) bool {
	return true
}

func (h *captureLogHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())
	return nil
}

func (h *captureLogHandler) WithAttrs([]slog.Attr) slog.Handler {
	return h
}

func (h *captureLogHandler) WithGroup(string) slog.Handler {
	return h
}

func TestRequestMetricsLogsAPIRequests(t *testing.T) {
	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	handler := RequestMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/libraries", strings.NewReader("body")))

	if len(capture.records) != 1 {
		t.Fatalf("expected one request log, got %d", len(capture.records))
	}
	record := capture.records[0]
	if record.Level != slog.LevelInfo {
		t.Fatalf("expected info log, got %s", record.Level)
	}
	attrs := attrsFromRecord(record)
	if attrs["method"] != http.MethodPost {
		t.Fatalf("expected method attr, got %v", attrs["method"])
	}
	if attrs["path"] != "/api/libraries" {
		t.Fatalf("expected path attr, got %v", attrs["path"])
	}
	if attrs["status"] != int64(http.StatusCreated) {
		t.Fatalf("expected status 201, got %v", attrs["status"])
	}
	if attrs["bytes"] != int64(len("created")) {
		t.Fatalf("expected response byte count, got %v", attrs["bytes"])
	}
}

func TestRequestMetricsSkipsFastStaticSuccess(t *testing.T) {
	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	handler := RequestMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("asset"))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/index.js", nil))

	if len(capture.records) != 0 {
		t.Fatalf("expected static success request to be skipped, got %d logs", len(capture.records))
	}
}

func TestRequestMetricsLogsSlowAndFailedStaticRequests(t *testing.T) {
	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	previousSlowThreshold := slowRequestThreshold
	slowRequestThreshold = time.Nanosecond
	t.Cleanup(func() {
		slowRequestThreshold = previousSlowThreshold
	})

	handler := RequestMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("missing"))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/missing.js", nil))

	if len(capture.records) != 1 {
		t.Fatalf("expected failed static request to be logged, got %d logs", len(capture.records))
	}
	if capture.records[0].Level != slog.LevelWarn {
		t.Fatalf("expected warning for failed static request, got %s", capture.records[0].Level)
	}
}

func attrsFromRecord(record slog.Record) map[string]any {
	attrs := make(map[string]any)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	return attrs
}
