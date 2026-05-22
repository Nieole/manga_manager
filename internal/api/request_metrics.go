package api

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"sort"
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
	Time             time.Time `json:"time"`
	Method           string    `json:"method"`
	Path             string    `json:"path"`
	Route            string    `json:"route"`
	Status           int       `json:"status"`
	Bytes            int       `json:"bytes"`
	DurationMS       int64     `json:"duration_ms"`
	RemoteIP         string    `json:"remote_ip"`
	CacheHit         bool      `json:"cache_hit"`
	CacheSource      string    `json:"cache_source,omitempty"`
	BookID           *int64    `json:"book_id,omitempty"`
	PageNumber       *int64    `json:"page_number,omitempty"`
	Transform        string    `json:"transform,omitempty"`
	ArchiveOpen      bool      `json:"archive_open"`
	ManifestCacheHit bool      `json:"manifest_cache_hit"`
	RawPassthrough   bool      `json:"raw_passthrough"`
	Processed        bool      `json:"processed"`
}

type SystemPerformanceResponse struct {
	SampleCount              int                          `json:"sample_count"`
	StartedAt                *time.Time                   `json:"started_at,omitempty"`
	EndedAt                  *time.Time                   `json:"ended_at,omitempty"`
	SlowThresholdMS          int64                        `json:"slow_threshold_ms"`
	TotalRequests            int                          `json:"total_requests"`
	ErrorRequests            int                          `json:"error_requests"`
	SlowRequests             int                          `json:"slow_requests"`
	TotalBytes               int64                        `json:"total_bytes"`
	CacheHits                int                          `json:"cache_hits"`
	PageImageRequests        int                          `json:"page_image_requests"`
	PageImageCacheHits       int                          `json:"page_image_cache_hits"`
	PageImageArchiveOpens    int                          `json:"page_image_archive_opens"`
	PageImageManifestHits    int                          `json:"page_image_manifest_hits"`
	PageImageRawPassthroughs int                          `json:"page_image_raw_passthroughs"`
	PageImageProcessed       int                          `json:"page_image_processed"`
	AverageMS                int64                        `json:"average_ms"`
	P95MS                    int64                        `json:"p95_ms"`
	MaxMS                    int64                        `json:"max_ms"`
	Routes                   []SystemRoutePerformance     `json:"routes"`
	Transforms               []SystemTransformPerformance `json:"transforms"`
	RecentSlow               []RequestDiagnosticEvent     `json:"recent_slow"`
	RecentErrors             []RequestDiagnosticEvent     `json:"recent_errors"`
	ProtocolCounts           SystemProtocolCounts         `json:"protocol_counts"`
}

type SystemRoutePerformance struct {
	Route      string    `json:"route"`
	Path       string    `json:"path"`
	Count      int       `json:"count"`
	Errors     int       `json:"errors"`
	Slow       int       `json:"slow"`
	AverageMS  int64     `json:"average_ms"`
	P95MS      int64     `json:"p95_ms"`
	MaxMS      int64     `json:"max_ms"`
	LastSeen   time.Time `json:"last_seen"`
	LastStatus int       `json:"last_status"`
	LastPath   string    `json:"last_path"`
}

type SystemProtocolCounts struct {
	API      int `json:"api"`
	OPDS     int `json:"opds"`
	Mihon    int `json:"mihon"`
	KOReader int `json:"koreader"`
	Other    int `json:"other"`
}

type SystemTransformPerformance struct {
	Transform string `json:"transform"`
	Count     int    `json:"count"`
	CacheHits int    `json:"cache_hits"`
	AverageMS int64  `json:"average_ms"`
	P95MS     int64  `json:"p95_ms"`
	MaxMS     int64  `json:"max_ms"`
}

type routePerformanceAccumulator struct {
	route      string
	path       string
	count      int
	errors     int
	slow       int
	totalMS    int64
	durations  []int64
	maxMS      int64
	lastSeen   time.Time
	lastStatus int
	lastPath   string
}

type transformPerformanceAccumulator struct {
	transform string
	count     int
	cacheHits int
	totalMS   int64
	durations []int64
	maxMS     int64
}

type RequestPerformanceInfo struct {
	CacheHit         bool
	CacheSource      string
	BookID           *int64
	PageNumber       *int64
	Transform        string
	ArchiveOpen      bool
	ManifestCacheHit bool
	RawPassthrough   bool
	Processed        bool
}

type requestPerformanceInfoKey struct{}

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

func (c *Controller) getSystemPerformance(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, http.StatusOK, buildSystemPerformanceSummary(requestDiagnostics.snapshot()))
}

func annotateRequestPerformance(ctx context.Context, update func(*RequestPerformanceInfo)) {
	info, ok := ctx.Value(requestPerformanceInfoKey{}).(*RequestPerformanceInfo)
	if !ok || info == nil || update == nil {
		return
	}
	update(info)
}

func annotatePageImageRequest(ctx context.Context, bookID, pageNumber int64, cacheHit bool, cacheSource, transform string) {
	annotateRequestPerformance(ctx, func(info *RequestPerformanceInfo) {
		info.BookID = int64Ptr(bookID)
		info.PageNumber = int64Ptr(pageNumber)
		info.CacheHit = cacheHit
		info.CacheSource = cacheSource
		info.Transform = transform
	})
}

func annotatePageImageDiagnostics(ctx context.Context, archiveOpen, manifestCacheHit, rawPassthrough, processed bool) {
	annotateRequestPerformance(ctx, func(info *RequestPerformanceInfo) {
		info.ArchiveOpen = info.ArchiveOpen || archiveOpen
		info.ManifestCacheHit = info.ManifestCacheHit || manifestCacheHit
		info.RawPassthrough = info.RawPassthrough || rawPassthrough
		info.Processed = info.Processed || processed
	})
}

func int64Ptr(value int64) *int64 {
	v := value
	return &v
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
		performanceInfo := &RequestPerformanceInfo{}
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), requestPerformanceInfoKey{}, performanceInfo)))

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
			Time:             time.Now(),
			Method:           r.Method,
			Path:             r.URL.Path,
			Route:            routePattern,
			Status:           status,
			Bytes:            rec.bytes,
			DurationMS:       duration.Milliseconds(),
			RemoteIP:         r.RemoteAddr,
			CacheHit:         performanceInfo.CacheHit,
			CacheSource:      performanceInfo.CacheSource,
			BookID:           performanceInfo.BookID,
			PageNumber:       performanceInfo.PageNumber,
			Transform:        performanceInfo.Transform,
			ArchiveOpen:      performanceInfo.ArchiveOpen,
			ManifestCacheHit: performanceInfo.ManifestCacheHit,
			RawPassthrough:   performanceInfo.RawPassthrough,
			Processed:        performanceInfo.Processed,
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
		if performanceInfo.CacheSource != "" {
			attrs = append(attrs, "cache_hit", performanceInfo.CacheHit, "cache_source", performanceInfo.CacheSource)
		}
		if performanceInfo.BookID != nil {
			attrs = append(attrs, "book_id", *performanceInfo.BookID)
		}
		if performanceInfo.PageNumber != nil {
			attrs = append(attrs, "page_number", *performanceInfo.PageNumber)
		}
		if performanceInfo.Transform != "" {
			attrs = append(attrs, "transform", performanceInfo.Transform)
		}
		if performanceInfo.BookID != nil && performanceInfo.PageNumber != nil {
			attrs = append(attrs,
				"archive_open", performanceInfo.ArchiveOpen,
				"manifest_cache_hit", performanceInfo.ManifestCacheHit,
				"raw_passthrough", performanceInfo.RawPassthrough,
				"processed", performanceInfo.Processed,
			)
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

func buildSystemPerformanceSummary(events []RequestDiagnosticEvent) SystemPerformanceResponse {
	response := SystemPerformanceResponse{
		SampleCount:     len(events),
		SlowThresholdMS: slowRequestThreshold.Milliseconds(),
		TotalRequests:   len(events),
		Routes:          []SystemRoutePerformance{},
		RecentSlow:      []RequestDiagnosticEvent{},
		RecentErrors:    []RequestDiagnosticEvent{},
	}
	if len(events) == 0 {
		return response
	}

	routeStats := make(map[string]*routePerformanceAccumulator)
	transformStats := make(map[string]*transformPerformanceAccumulator)
	durations := make([]int64, 0, len(events))
	var totalMS int64

	for i, event := range events {
		if i == 0 || event.Time.Before(*response.StartedAt) {
			t := event.Time
			response.StartedAt = &t
		}
		if i == 0 || event.Time.After(*response.EndedAt) {
			t := event.Time
			response.EndedAt = &t
		}

		response.TotalBytes += int64(event.Bytes)
		totalMS += event.DurationMS
		durations = append(durations, event.DurationMS)
		if event.DurationMS > response.MaxMS {
			response.MaxMS = event.DurationMS
		}
		if event.CacheHit {
			response.CacheHits++
		}
		if event.BookID != nil && event.PageNumber != nil {
			response.PageImageRequests++
			if event.CacheHit {
				response.PageImageCacheHits++
			}
			if event.ArchiveOpen {
				response.PageImageArchiveOpens++
			}
			if event.ManifestCacheHit {
				response.PageImageManifestHits++
			}
			if event.RawPassthrough {
				response.PageImageRawPassthroughs++
			}
			if event.Processed {
				response.PageImageProcessed++
			}
		}
		if event.Status >= 400 {
			response.ErrorRequests++
			response.RecentErrors = appendRecentDiagnostic(response.RecentErrors, event, 5)
		}
		if event.DurationMS >= response.SlowThresholdMS {
			response.SlowRequests++
			response.RecentSlow = appendRecentDiagnostic(response.RecentSlow, event, 5)
		}
		response.ProtocolCounts = incrementProtocolCount(response.ProtocolCounts, event.Path)

		key := routePerformanceKey(event)
		stat := routeStats[key]
		if stat == nil {
			stat = &routePerformanceAccumulator{
				route:     key,
				path:      event.Path,
				durations: make([]int64, 0, 1),
			}
			routeStats[key] = stat
		}
		stat.count++
		stat.totalMS += event.DurationMS
		stat.durations = append(stat.durations, event.DurationMS)
		if event.Status >= 400 {
			stat.errors++
		}
		if event.DurationMS >= response.SlowThresholdMS {
			stat.slow++
		}
		if event.DurationMS > stat.maxMS {
			stat.maxMS = event.DurationMS
		}
		if event.Time.After(stat.lastSeen) {
			stat.lastSeen = event.Time
			stat.lastStatus = event.Status
			stat.lastPath = event.Path
		}

		if event.Transform != "" {
			transform := event.Transform
			transformStat := transformStats[transform]
			if transformStat == nil {
				transformStat = &transformPerformanceAccumulator{
					transform: transform,
					durations: make([]int64, 0, 1),
				}
				transformStats[transform] = transformStat
			}
			transformStat.count++
			transformStat.totalMS += event.DurationMS
			transformStat.durations = append(transformStat.durations, event.DurationMS)
			if event.CacheHit {
				transformStat.cacheHits++
			}
			if event.DurationMS > transformStat.maxMS {
				transformStat.maxMS = event.DurationMS
			}
		}
	}

	response.AverageMS = totalMS / int64(len(events))
	response.P95MS = percentileDuration(durations, 0.95)
	response.Routes = topRoutePerformance(routeStats, 8)
	response.Transforms = topTransformPerformance(transformStats, 8)
	return response
}

func appendRecentDiagnostic(events []RequestDiagnosticEvent, event RequestDiagnosticEvent, limit int) []RequestDiagnosticEvent {
	events = append(events, event)
	if len(events) > limit {
		return events[len(events)-limit:]
	}
	return events
}

func routePerformanceKey(event RequestDiagnosticEvent) string {
	if event.Route != "" {
		return event.Route
	}
	if strings.HasPrefix(event.Path, "/api/pages/") {
		return "/api/pages/*"
	}
	if strings.HasPrefix(event.Path, "/api/thumbnails/") {
		return "/api/thumbnails/*"
	}
	if strings.HasPrefix(event.Path, "/api/covers/") {
		return "/api/covers/*"
	}
	if strings.HasPrefix(event.Path, "/opds/") {
		return "/opds/*"
	}
	if strings.HasPrefix(event.Path, "/api/mihon/") {
		return "/api/mihon/*"
	}
	if strings.HasPrefix(event.Path, "/koreader/") || looksLikeKOReaderPath(event.Path) {
		return "/koreader/*"
	}
	return event.Path
}

func incrementProtocolCount(counts SystemProtocolCounts, path string) SystemProtocolCounts {
	switch {
	case strings.HasPrefix(path, "/api/mihon/"):
		counts.Mihon++
	case strings.HasPrefix(path, "/api/"):
		counts.API++
	case strings.HasPrefix(path, "/opds/"):
		counts.OPDS++
	case strings.HasPrefix(path, "/koreader/") || looksLikeKOReaderPath(path):
		counts.KOReader++
	default:
		counts.Other++
	}
	return counts
}

func topRoutePerformance(stats map[string]*routePerformanceAccumulator, limit int) []SystemRoutePerformance {
	routes := make([]SystemRoutePerformance, 0, len(stats))
	for _, stat := range stats {
		avg := int64(0)
		if stat.count > 0 {
			avg = stat.totalMS / int64(stat.count)
		}
		routes = append(routes, SystemRoutePerformance{
			Route:      stat.route,
			Path:       stat.path,
			Count:      stat.count,
			Errors:     stat.errors,
			Slow:       stat.slow,
			AverageMS:  avg,
			P95MS:      percentileDuration(stat.durations, 0.95),
			MaxMS:      stat.maxMS,
			LastSeen:   stat.lastSeen,
			LastStatus: stat.lastStatus,
			LastPath:   stat.lastPath,
		})
	}

	sortRoutePerformance(routes)
	if len(routes) > limit {
		return routes[:limit]
	}
	return routes
}

func topTransformPerformance(stats map[string]*transformPerformanceAccumulator, limit int) []SystemTransformPerformance {
	transforms := make([]SystemTransformPerformance, 0, len(stats))
	for _, stat := range stats {
		avg := int64(0)
		if stat.count > 0 {
			avg = stat.totalMS / int64(stat.count)
		}
		transforms = append(transforms, SystemTransformPerformance{
			Transform: stat.transform,
			Count:     stat.count,
			CacheHits: stat.cacheHits,
			AverageMS: avg,
			P95MS:     percentileDuration(stat.durations, 0.95),
			MaxMS:     stat.maxMS,
		})
	}
	sort.SliceStable(transforms, func(i, j int) bool {
		if transforms[i].Count == transforms[j].Count {
			if transforms[i].P95MS == transforms[j].P95MS {
				return transforms[i].Transform < transforms[j].Transform
			}
			return transforms[i].P95MS > transforms[j].P95MS
		}
		return transforms[i].Count > transforms[j].Count
	})
	if len(transforms) > limit {
		return transforms[:limit]
	}
	return transforms
}

func percentileDuration(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64{}, values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(float64(len(sorted))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func sortRoutePerformance(routes []SystemRoutePerformance) {
	sort.SliceStable(routes, func(i, j int) bool {
		leftScore := routes[i].Slow*1000 + routes[i].Errors*100 + routes[i].Count
		rightScore := routes[j].Slow*1000 + routes[j].Errors*100 + routes[j].Count
		if leftScore == rightScore {
			if routes[i].P95MS == routes[j].P95MS {
				return routes[i].Route < routes[j].Route
			}
			return routes[i].P95MS > routes[j].P95MS
		}
		return leftScore > rightScore
	})
}
