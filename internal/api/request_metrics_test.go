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
	requestDiagnostics.reset()
	t.Cleanup(requestDiagnostics.reset)
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
	diagnostics := requestDiagnostics.snapshot()
	if len(diagnostics) != 1 || diagnostics[0].Path != "/api/libraries" || diagnostics[0].Status != http.StatusCreated {
		t.Fatalf("unexpected request diagnostics: %+v", diagnostics)
	}
}

func TestRequestMetricsSkipsFastStaticSuccess(t *testing.T) {
	requestDiagnostics.reset()
	t.Cleanup(requestDiagnostics.reset)
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
	if got := requestDiagnostics.snapshot(); len(got) != 0 {
		t.Fatalf("expected static success request to be skipped by diagnostics, got %+v", got)
	}
}

func TestRequestMetricsLogsSlowAndFailedStaticRequests(t *testing.T) {
	requestDiagnostics.reset()
	t.Cleanup(requestDiagnostics.reset)
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

func TestRequestMetricsRecordsCustomKOReaderDiagnostics(t *testing.T) {
	requestDiagnostics.reset()
	t.Cleanup(requestDiagnostics.reset)

	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	handler := RequestMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"state":"OK"}`))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/sync/custom/healthcheck", nil))

	if len(capture.records) != 0 {
		t.Fatalf("expected custom KOReader healthcheck to stay out of structured logs, got %d", len(capture.records))
	}
	diagnostics := requestDiagnostics.snapshot()
	if len(diagnostics) != 1 || diagnostics[0].Path != "/sync/custom/healthcheck" {
		t.Fatalf("expected custom KOReader path in diagnostics, got %+v", diagnostics)
	}
}

func TestRequestMetricsRecordsPerformanceFields(t *testing.T) {
	requestDiagnostics.reset()
	t.Cleanup(requestDiagnostics.reset)
	capture := &captureLogHandler{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})

	handler := RequestMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		annotatePageImageRequest(r.Context(), 10, 3, true, "memory", "format:webp")
		_, _ = w.Write([]byte("image"))
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/pages/10/3?format=webp", nil))

	diagnostics := requestDiagnostics.snapshot()
	if len(diagnostics) != 1 {
		t.Fatalf("expected one diagnostic, got %+v", diagnostics)
	}
	event := diagnostics[0]
	if !event.CacheHit || event.CacheSource != "memory" || event.Transform != "format:webp" {
		t.Fatalf("unexpected performance fields: %+v", event)
	}
	if event.BookID == nil || *event.BookID != 10 || event.PageNumber == nil || *event.PageNumber != 3 {
		t.Fatalf("unexpected page identity fields: %+v", event)
	}

	if len(capture.records) != 1 {
		t.Fatalf("expected one request log, got %d", len(capture.records))
	}
	attrs := attrsFromRecord(capture.records[0])
	if attrs["cache_hit"] != true || attrs["cache_source"] != "memory" {
		t.Fatalf("expected cache attrs, got %+v", attrs)
	}
	if attrs["book_id"] != int64(10) || attrs["page_number"] != int64(3) || attrs["transform"] != "format:webp" {
		t.Fatalf("expected page attrs, got %+v", attrs)
	}
}

func TestBuildSystemPerformanceSummary(t *testing.T) {
	previousSlowThreshold := slowRequestThreshold
	slowRequestThreshold = 500 * time.Millisecond
	t.Cleanup(func() {
		slowRequestThreshold = previousSlowThreshold
	})

	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	summary := buildSystemPerformanceSummary([]RequestDiagnosticEvent{
		{
			Time:       base,
			Method:     http.MethodGet,
			Path:       "/api/pages/1/1",
			Route:      "/api/pages/{bookId}/{pageNumber}",
			Status:     http.StatusOK,
			Bytes:      100,
			DurationMS: 40,
			BookID:     int64Ptr(1),
			PageNumber: int64Ptr(1),
			Transform:  "raw",
		},
		{
			Time:        base.Add(time.Second),
			Method:      http.MethodGet,
			Path:        "/api/pages/1/2",
			Route:       "/api/pages/{bookId}/{pageNumber}",
			Status:      http.StatusOK,
			Bytes:       200,
			DurationMS:  650,
			CacheHit:    true,
			CacheSource: "memory",
			BookID:      int64Ptr(1),
			PageNumber:  int64Ptr(2),
			Transform:   "format:webp",
		},
		{
			Time:       base.Add(2 * time.Second),
			Method:     http.MethodGet,
			Path:       "/opds/v1.2/",
			Route:      "/opds/v1.2/",
			Status:     http.StatusInternalServerError,
			Bytes:      50,
			DurationMS: 20,
		},
		{
			Time:       base.Add(3 * time.Second),
			Method:     http.MethodGet,
			Path:       "/sync/custom/healthcheck",
			Status:     http.StatusOK,
			Bytes:      30,
			DurationMS: 10,
		},
	})

	if summary.TotalRequests != 4 || summary.SampleCount != 4 {
		t.Fatalf("expected 4 total requests, got %+v", summary)
	}
	if summary.ErrorRequests != 1 || summary.SlowRequests != 1 {
		t.Fatalf("expected one error and one slow request, got errors=%d slow=%d", summary.ErrorRequests, summary.SlowRequests)
	}
	if summary.TotalBytes != 380 {
		t.Fatalf("expected 380 total bytes, got %d", summary.TotalBytes)
	}
	if summary.ProtocolCounts.API != 2 || summary.ProtocolCounts.OPDS != 1 || summary.ProtocolCounts.KOReader != 1 {
		t.Fatalf("unexpected protocol counts: %+v", summary.ProtocolCounts)
	}
	if summary.CacheHits != 1 || summary.PageImageRequests != 2 || summary.PageImageCacheHits != 1 {
		t.Fatalf("unexpected cache aggregates: cache=%d page=%d page_cache=%d", summary.CacheHits, summary.PageImageRequests, summary.PageImageCacheHits)
	}
	if len(summary.RecentSlow) != 1 || summary.RecentSlow[0].Path != "/api/pages/1/2" {
		t.Fatalf("unexpected recent slow events: %+v", summary.RecentSlow)
	}
	if len(summary.RecentErrors) != 1 || summary.RecentErrors[0].Path != "/opds/v1.2/" {
		t.Fatalf("unexpected recent error events: %+v", summary.RecentErrors)
	}
	if len(summary.Routes) == 0 || summary.Routes[0].Route != "/api/pages/{bookId}/{pageNumber}" {
		t.Fatalf("expected page route to rank first, got %+v", summary.Routes)
	}
	if summary.Routes[0].Count != 2 || summary.Routes[0].Slow != 1 || summary.Routes[0].AverageMS != 345 {
		t.Fatalf("unexpected page route aggregate: %+v", summary.Routes[0])
	}
	if len(summary.Transforms) != 2 || summary.Transforms[0].Transform != "format:webp" || summary.Transforms[0].CacheHits != 1 {
		t.Fatalf("unexpected transform aggregates: %+v", summary.Transforms)
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
