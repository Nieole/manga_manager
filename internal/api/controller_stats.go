// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的看板统计子域，负责仪表盘结构/易变统计的缓存（失效/预热/分层加载）与看板、活跃热力图、最近阅读等只读接口。

package api

import (
	"context"
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

// invalidateDashboardStatsCache / invalidateVolatileStatsCache 是 statsCache 的薄委托（保留方法名以维持
// 各处失效调用点不变）。缓存的状态与加载逻辑已抽入 stats_cache.go。
func (c *Controller) invalidateDashboardStatsCache(reason string) {
	c.stats.invalidateAll(reason)
}

func (c *Controller) invalidateVolatileStatsCache(reason string) {
	c.stats.invalidateVolatile(reason)
}

// warmDashboardStatsCacheAsync 经受生命周期管理的后台 goroutine 预热仪表盘缓存（故留在 Controller）。
func (c *Controller) warmDashboardStatsCacheAsync(reason string) {
	c.runBackground(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.stats.loadDashboard(ctx, c.store); err != nil {
			slog.Debug("Failed to warm dashboard stats cache", "reason", reason, "error", err)
		}
	})
}

// getDashboardStats 返回统计看板数据。结构性统计（系列/书/页总数）与近 7 日活跃天数为全局缓存；
// 已读书本数（read_books）按当前用户改写（每用户进度）。活动热力图本阶段仍为全局。
func (c *Controller) getDashboardStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := c.stats.loadDashboard(ctx, c.store)
	if err != nil {
		slog.Error("GetDashboardStats failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get dashboard stats")
		return
	}
	if uid := c.currentUserID(r); uid > 0 {
		if n, e := c.store.GetUserReadBooksCount(ctx, uid); e == nil {
			stats.ReadBooks = int(n)
		}
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
	// 已登录用户走每用户活动表；否则退回全局（首启 / 单用户 / 测试）。
	if uid := c.currentUserID(r); uid > 0 {
		data, err := c.store.GetUserActivityHeatmap(r.Context(), uid, offset)
		if err != nil {
			slog.Error("GetUserActivityHeatmap failed", "user_id", uid, "error", err)
			jsonError(w, http.StatusInternalServerError, "Failed to get activity heatmap")
			return
		}
		if data == nil {
			data = []database.ActivityDay{}
		}
		jsonResponse(w, http.StatusOK, data)
		return
	}

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

// getReadingStreak 返回当前用户的连续阅读天数（当前 / 最长）。
func (c *Controller) getReadingStreak(w http.ResponseWriter, r *http.Request) {
	uid := c.currentUserID(r)
	if uid == 0 {
		jsonResponse(w, http.StatusOK, map[string]int{"current": 0, "longest": 0})
		return
	}
	current, longest, err := c.store.GetUserReadingStreak(r.Context(), uid)
	if err != nil {
		slog.Error("GetUserReadingStreak failed", "user_id", uid, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get streak")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]int{"current": current, "longest": longest})
}

// getReadingTimeStats 返回当前用户的累计阅读时长与「每本时长」排行。
func (c *Controller) getReadingTimeStats(w http.ResponseWriter, r *http.Request) {
	uid := c.currentUserID(r)
	if uid == 0 {
		jsonResponse(w, http.StatusOK, map[string]any{"total_seconds": 0, "top": []database.BookReadingTimeRow{}})
		return
	}
	total, err := c.store.GetUserTotalReadingTime(r.Context(), uid)
	if err != nil {
		slog.Error("GetUserTotalReadingTime failed", "user_id", uid, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get reading time")
		return
	}
	limit := 10
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 100 {
		limit = v
	}
	top, err := c.store.GetUserBookReadingTimeTop(r.Context(), uid, limit)
	if err != nil {
		slog.Error("GetUserBookReadingTimeTop failed", "user_id", uid, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get reading time")
		return
	}
	if top == nil {
		top = []database.BookReadingTimeRow{}
	}
	jsonResponse(w, http.StatusOK, map[string]any{"total_seconds": total, "top": top})
}

// getPeriodStats 返回当前用户在某年（无 month 或 month=0）或某年月的阅读回顾。
func (c *Controller) getPeriodStats(w http.ResponseWriter, r *http.Request) {
	uid := c.currentUserID(r)
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year <= 0 {
		year = time.Now().UTC().Year()
	}
	if month < 0 || month > 12 {
		month = 0
	}
	if uid == 0 {
		jsonResponse(w, http.StatusOK, database.UserPeriodStats{TopSeries: []database.PeriodTopSeries{}})
		return
	}
	stats, err := c.store.GetUserPeriodStats(r.Context(), uid, year, month)
	if err != nil {
		slog.Error("GetUserPeriodStats failed", "user_id", uid, "year", year, "month", month, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get period stats")
		return
	}
	if stats.TopSeries == nil {
		stats.TopSeries = []database.PeriodTopSeries{}
	}
	jsonResponse(w, http.StatusOK, stats)
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

	var (
		items []database.GetRecentReadAllRow
		err   error
	)
	if uid := c.currentUserID(r); uid > 0 {
		items, err = c.store.GetUserRecentReadAll(r.Context(), uid, limit)
	} else {
		items, err = c.store.GetRecentReadAll(r.Context(), limit)
	}
	if err != nil {
		slog.Error("GetRecentReadAll failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to get recent reads")
		return
	}

	// 续读建议基于全局系列完成度（本阶段不做每用户拆分）。
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
