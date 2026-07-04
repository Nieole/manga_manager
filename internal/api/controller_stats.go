// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的看板统计子域，负责仪表盘结构/易变统计的缓存（失效/预热/分层加载）与看板、活跃热力图、最近阅读等只读接口。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/database"
	"net/http"
	"strconv"
	"time"
)

func cloneDashboardStats(stats *database.DashboardStats) *database.DashboardStats {
	if stats == nil {
		return nil
	}
	cloned := *stats
	if stats.LibrarySizes != nil {
		cloned.LibrarySizes = append([]database.LibrarySize(nil), stats.LibrarySizes...)
	}
	return &cloned
}

// invalidateDashboardStatsCache 失效全部统计缓存（结构性 + 阅读类）。
// 用于扫描/库结构变化等会改变 total_books/total_pages 的场景。
func (c *Controller) invalidateDashboardStatsCache(reason string) {
	c.structuralStatsMu.Lock()
	c.structuralStatsGen++
	c.structuralStatsCache = nil
	c.structuralStatsMu.Unlock()

	c.dashboardStatsMu.Lock()
	c.dashboardStatsGen++
	c.volatileStatsCache = nil
	c.dashboardStatsMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated dashboard stats cache", "reason", reason)
	}
}

// invalidateVolatileStatsCache 仅失效阅读类统计缓存（read_books/active_days）。
// 用于阅读进度更新等高频场景——这些操作不改变结构性统计，避免触发 books 全表扫描。
func (c *Controller) invalidateVolatileStatsCache(reason string) {
	c.dashboardStatsMu.Lock()
	c.dashboardStatsGen++
	c.volatileStatsCache = nil
	c.dashboardStatsMu.Unlock()
	if reason != "" {
		slog.Debug("Invalidated volatile dashboard stats cache", "reason", reason)
	}
}

func (c *Controller) warmDashboardStatsCacheAsync(reason string) {
	c.runBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.loadDashboardStats(ctx); err != nil {
			slog.Debug("Failed to warm dashboard stats cache", "reason", reason, "error", err)
		}
	})
}

func (c *Controller) loadStructuralStats(ctx context.Context) (*database.DashboardStructuralStats, error) {
	now := time.Now()
	c.structuralStatsMu.RLock()
	if c.structuralStatsCache != nil && now.Before(c.structuralStatsCache.expiresAt) {
		stats := c.structuralStatsCache.stats
		c.structuralStatsMu.RUnlock()
		return &stats, nil
	}
	generation := c.structuralStatsGen
	c.structuralStatsMu.RUnlock()

	stats, err := c.store.GetDashboardStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &database.DashboardStructuralStats{}
	}
	c.structuralStatsMu.Lock()
	if generation == c.structuralStatsGen {
		c.structuralStatsCache = &cachedStructuralStats{
			stats:     *stats,
			expiresAt: now.Add(dashboardStatsCacheTTL),
		}
	}
	c.structuralStatsMu.Unlock()
	return stats, nil
}

func (c *Controller) loadVolatileStats(ctx context.Context) (*database.DashboardVolatileStats, error) {
	now := time.Now()
	c.dashboardStatsMu.RLock()
	if c.volatileStatsCache != nil && now.Before(c.volatileStatsCache.expiresAt) {
		stats := c.volatileStatsCache.stats
		c.dashboardStatsMu.RUnlock()
		return &stats, nil
	}
	generation := c.dashboardStatsGen
	c.dashboardStatsMu.RUnlock()

	stats, err := c.store.GetDashboardVolatileStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		stats = &database.DashboardVolatileStats{}
	}
	c.dashboardStatsMu.Lock()
	if generation == c.dashboardStatsGen {
		c.volatileStatsCache = &cachedVolatileStats{
			stats:     *stats,
			expiresAt: now.Add(dashboardStatsCacheTTL),
		}
	}
	c.dashboardStatsMu.Unlock()
	return stats, nil
}

func (c *Controller) loadDashboardStats(ctx context.Context) (*database.DashboardStats, error) {
	if c.store == nil {
		return nil, errors.New("store is not configured")
	}

	structural, err := c.loadStructuralStats(ctx)
	if err != nil {
		return nil, err
	}
	volatile, err := c.loadVolatileStats(ctx)
	if err != nil {
		return nil, err
	}

	stats := &database.DashboardStats{
		TotalSeries:  structural.TotalSeries,
		TotalBooks:   structural.TotalBooks,
		TotalPages:   structural.TotalPages,
		LibrarySizes: structural.LibrarySizes,
		ReadBooks:    volatile.ReadBooks,
		ActiveDays7:  volatile.ActiveDays7,
	}
	return cloneDashboardStats(stats), nil
}

// getDashboardStats 返回全局统计看板数据
func (c *Controller) getDashboardStats(w http.ResponseWriter, r *http.Request) {
	stats, err := c.loadDashboardStats(r.Context())
	if err != nil {
		slog.Error("GetDashboardStats failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get dashboard stats")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

// getActivityHeatmap 返回近 N 周每日阅读页数热力数据
func (c *Controller) getActivityHeatmap(w http.ResponseWriter, r *http.Request) {
	weeksStr := r.URL.Query().Get("weeks")
	weeks := 16 // 默认 16 周
	if w, err := strconv.Atoi(weeksStr); err == nil && w > 0 && w <= 52 {
		weeks = w
	}

	offset := fmt.Sprintf("-%d days", weeks*7)
	rows, err := c.store.GetActivityHeatmap(r.Context(), offset)
	if err != nil {
		slog.Error("GetActivityHeatmap failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get activity heatmap")
		return
	}
	data := make([]database.ActivityDay, 0, len(rows))
	for _, row := range rows {
		count := 0
		if row.PageCount.Valid {
			count = int(row.PageCount.Float64)
		}
		data = append(data, database.ActivityDay{Date: row.Date, PageCount: count})
	}
	jsonResponse(w, http.StatusOK, data)
}

// getRecentReadAll 返回跨库的最近阅读记录（用于 Dashboard 首页）
func (c *Controller) getRecentReadAll(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := int64(20)
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 {
			limit = l
		}
	}

	items, err := c.store.GetRecentReadAll(r.Context(), limit)
	if err != nil {
		slog.Error("GetRecentReadAll failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get recent reads")
		return
	}

	sequels, err := c.store.GetContinueReadingSequels(r.Context())
	if err != nil {
		slog.Error("GetContinueReadingSequels failed", "error", err)
		// Ignore error and continue with items
	}

	type DashboardContinueItem struct {
		SeriesID           int64       `json:"series_id"`
		SeriesName         string      `json:"series_name"`
		BookID             int64       `json:"book_id"`
		BookName           string      `json:"book_name"`
		BookTitle          interface{} `json:"book_title"`
		CoverPath          string      `json:"cover_path"`
		LastReadPage       interface{} `json:"last_read_page"`
		LastReadAt         interface{} `json:"last_read_at"`
		PageCount          int64       `json:"page_count"`
		IsSequelSuggestion bool        `json:"is_sequel_suggestion,omitempty"`
		RelationType       string      `json:"relation_type,omitempty"`
		SourceSeriesName   string      `json:"source_series_name,omitempty"`
	}

	result := make([]DashboardContinueItem, 0, len(items)+len(sequels))
	for _, item := range items {
		cover := item.CoverPath
		result = append(result, DashboardContinueItem{
			SeriesID:     item.SeriesID,
			SeriesName:   item.SeriesName,
			BookID:       item.BookID,
			BookName:     item.BookName,
			BookTitle:    item.BookTitle,
			CoverPath:    cover,
			LastReadPage: item.LastReadPage,
			LastReadAt:   item.LastReadAt,
			PageCount:    int64(item.PageCount),
		})
	}

	for _, s := range sequels {
		cover := ""
		if s.CoverPath.Valid {
			cover = s.CoverPath.String
		}
		result = append(result, DashboardContinueItem{
			SeriesID:           s.SeriesID,
			SeriesName:         s.SeriesName,
			BookID:             0,
			BookName:           "",
			BookTitle:          nil,
			CoverPath:          cover,
			LastReadPage:       nil,
			LastReadAt:         nil,
			PageCount:          0,
			IsSequelSuggestion: true,
			RelationType:       s.RelationType,
			SourceSeriesName:   s.SourceSeriesName,
		})
	}

	jsonResponse(w, http.StatusOK, result)
}
