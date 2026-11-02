package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"manga-manager/internal/database"
	"manga-manager/internal/metadata"
	"manga-manager/internal/reading"
	"manga-manager/internal/seriescontext"
)

func (c *ReaderHTTP) getBooksBySeries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	view, err := c.books.SeriesBooks(ctx, subject, seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch books")
		return
	}

	books := readerBookResponses(view.Books)
	jsonResponse(w, http.StatusOK, books)
}

func (c *ReaderHTTP) getBookInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	book, err := c.books.BookView(ctx, subject, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	jsonResponse(w, http.StatusOK, readerBookResponse(book))
}

func (c *ReaderHTTP) searchSeriesPaged(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libIDStr := r.URL.Query().Get("libraryId")
	libID, err := strconv.ParseInt(libIDStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	// 上限保护：limit 只有下限校验时，一个 limit=1000000 就能让本端点物化并 JSON 编码整库
	// (~24 列含 summary 长文本) 的全部系列，等同单请求 OOM/超时。UI 最大页大小为 100，200 留足余量。
	limit := queryLimit(r, "limit", 50, maxSeriesPageLimit)
	// page 同样必须钳制：无上界时 (page-1)*limit 会溢出，offset 再经 int32 截断成 0，
	// 极大页码会静默返回第一页而不是空页。
	page := queryPage(r, "page")
	offset := int(pageOffset(page, limit))

	var tags []string
	if tagsParam := r.URL.Query().Get("tags"); tagsParam != "" {
		tags = strings.Split(tagsParam, ",")
	}

	var authors []string
	if authorsParam := r.URL.Query().Get("authors"); authorsParam != "" {
		authors = strings.Split(authorsParam, ",")
	}

	sortBy := r.URL.Query().Get("sortBy")
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))

	query := r.URL.Query()
	filters := reading.CatalogueFilters{
		Keyword:         query.Get("q"),
		Letter:          query.Get("letter"),
		Status:          query.Get("status"),
		Tags:            tags,
		Authors:         authors,
		ReadState:       parseReadState(query.Get("readState")),
		MinRating:       parseOptionalFloat(query.Get("minRating")),
		MaxRating:       parseOptionalFloat(query.Get("maxRating")),
		MinProgress:     parseOptionalFloat(query.Get("minProgress")),
		MaxProgress:     parseOptionalFloat(query.Get("maxProgress")),
		AddedWithinDays: parseNonNegativeInt(query.Get("addedWithinDays")),
	}

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	view, err := c.catalogue.Search(ctx, subject, reading.CatalogueSearch{
		LibraryID: libID, Filters: filters, Limit: int32(limit), Offset: int32(offset), SortBy: sortBy, Cursor: cursor,
	})
	if err != nil {
		slog.Error("Series search failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}
	items := view.Items
	if items == nil {
		items = []database.SearchSeriesPagedRow{}
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": view.Total, "page": page, "limit": limit, "next_cursor": view.NextCursor, "has_more": view.HasMore,
	})
}

func (c *ReaderHTTP) getRecentReadSeries(w http.ResponseWriter, r *http.Request) {
	libIDStr := r.URL.Query().Get("libraryId")
	if libIDStr == "" {
		jsonError(w, http.StatusBadRequest, "libraryId is required")
		return
	}
	libID, err := strconv.ParseInt(libIDStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid libraryId")
		return
	}

	// 与 /api/series/search 统一口径：limit 必须有上限，否则 limit=1000000 会让单请求物化整库。
	limit := int64(queryLimit(r, "limit", 10, maxSeriesPageLimit))

	ctx := r.Context()
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	view, err := c.catalogue.Recent(ctx, subject, libID, limit)
	if err != nil {
		slog.Error("GetRecentReadSeries Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch recent read series")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items": view.Items,
	})
}

func (c *ReaderHTTP) getSeriesInfo(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}
	jsonResponse(w, http.StatusOK, series)
}

func (c *ReaderHTTP) getSeriesTags(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	tags, err := c.store.GetTagsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get tags")
		return
	}
	jsonResponse(w, http.StatusOK, tags)
}

func (c *ReaderHTTP) getSeriesAuthors(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	authors, err := c.store.GetAuthorsForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get authors")
		return
	}
	jsonResponse(w, http.StatusOK, authors)
}

func (c *ReaderHTTP) getSeriesLinks(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	links, err := c.store.GetLinksForSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get links")
		return
	}
	if links == nil {
		links = []database.SeriesLink{}
	}
	jsonResponse(w, http.StatusOK, links)
}

func (c *ReaderHTTP) getSeriesContext(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	out, err := c.context.Load(r.Context(), subject, seriesID)
	if err != nil {
		if errors.Is(err, seriescontext.ErrMissing) {
			jsonError(w, http.StatusNotFound, "Series not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "Failed to load series context")
		}
		return
	}
	failedTasks, err := c.failedRuns(r.Context(), out.FailedRuns)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load series tasks")
		return
	}
	if failedTasks == nil {
		failedTasks = []RunSnapshot{}
	}
	metadataReview := metadataReviewResponseFrom(out.Proposals)
	jsonResponse(w, http.StatusOK, SeriesContextResponse{Series: out.Series, Books: readerBookResponses(out.Books.Books), Tags: out.Tags, Authors: out.Authors, Links: out.Links, Volumes: buildSeriesVolumeSummaries(out.Books.Books, false), Relations: out.Relations, MetadataReview: metadataReview, MetadataSummary: SeriesMetadataSummary{PendingReviewCount: out.PendingReviews, ProvenanceCount: out.ProvenanceCount}, FailedTasks: failedTasks, FailedTaskSummary: SeriesFailedTaskSummary{Count: len(out.FailedRuns), LatestAt: out.LatestFailure}, Continue: out.Books.Continue, MetadataVersion: out.MetadataVersion})
}

func (c *ReaderHTTP) getSeriesContinueEndpoint(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	ctx := r.Context()
	if _, err := c.store.GetSeries(ctx, seriesID); err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	view, err := c.books.SeriesBooks(ctx, subject, seriesID)
	if err != nil {
		slog.Error("Failed to fetch books for continue", "series_id", seriesID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to compute continue position")
		return
	}
	jsonResponse(w, http.StatusOK, view.Continue)
}

func (c *ReaderHTTP) getAllTags(w http.ResponseWriter, r *http.Request) {
	tags, err := c.store.GetAllTags(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch all tags")
		return
	}
	if tags == nil {
		tags = []database.Tag{}
	}
	jsonResponse(w, http.StatusOK, tags)
}

func (c *ReaderHTTP) getSeriesCustomFields(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	fields, err := c.store.ListSeriesCustomFields(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch custom fields")
		return
	}
	if fields == nil {
		fields = []database.SeriesCustomField{}
	}
	jsonResponse(w, http.StatusOK, fields)
}

func (c *ReaderHTTP) searchTags(w http.ResponseWriter, r *http.Request) {
	items, err := c.store.SearchTags(r.Context(), r.URL.Query().Get("q"), parseFacetSearchLimit(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to search tags")
		return
	}
	if items == nil {
		items = []database.Tag{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *ReaderHTTP) getAllAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := c.store.GetAllAuthors(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch all authors")
		return
	}
	if authors == nil {
		authors = []database.Author{}
	}
	jsonResponse(w, http.StatusOK, authors)
}

func (c *ReaderHTTP) searchAuthors(w http.ResponseWriter, r *http.Request) {
	items, err := c.store.SearchAuthors(r.Context(), r.URL.Query().Get("q"), parseFacetSearchLimit(r))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to search authors")
		return
	}
	if items == nil {
		items = []database.Author{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *ReaderHTTP) getDashboardStats(w http.ResponseWriter, r *http.Request) {
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	stats, err := c.stats.Load(r.Context(), c.store, subject)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to get dashboard stats")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

func (c *ReaderHTTP) getActivityHeatmap(w http.ResponseWriter, r *http.Request) {
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

	// 下界在 Go 侧按本地时区算好：表里的 date 现在是本地日历日（见 database.ActivityDayKey），
	// 交给 SQLite 的 DATE('now', ?) 会按 UTC 算，跨时区部署下窗口整体错开一天。
	rows, err := c.store.GetActivityHeatmap(r.Context(), database.HeatmapSinceDate(offset))
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

func (c *ReaderHTTP) getReadingStreak(w http.ResponseWriter, r *http.Request) {
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

func (c *ReaderHTTP) getReadingTimeStats(w http.ResponseWriter, r *http.Request) {
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

func (c *ReaderHTTP) getPeriodStats(w http.ResponseWriter, r *http.Request) {
	uid := c.currentUserID(r)
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	month, _ := strconv.Atoi(r.URL.Query().Get("month"))
	if year <= 0 {
		// 默认期取服务器本地日历（database.CurrentPeriod），与活动日期同口径：
		// 按 UTC 取年份的话，UTC+N 的元旦凌晨、UTC-N 的除夕深夜会拿到错的那一年。
		year, _ = database.CurrentPeriod()
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

func (c *ReaderHTTP) getRecentReadAll(w http.ResponseWriter, r *http.Request) {
	// 与 /api/series/search 统一口径：limit 必须有上限，避免单请求拉出全部阅读记录。
	limit := int64(queryLimit(r, "limit", 20, maxListLimit))

	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	recent, err := reading.RecentBooks(r.Context(), c.store, subject, limit)
	items := recent.Items
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

func (c *ReaderHTTP) getRecommendations(w http.ResponseWriter, r *http.Request) {
	subject, err := c.currentReadingSubject(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading subject")
		return
	}
	items, err := c.recommendations.Load(r.Context(), c.store, subject, requestLocale(r), r.URL.Query().Get("refresh") == "true", func() metadata.Recommender {
		cfg := c.currentConfig()
		return metadata.NewAIProvider(cfg.LLM.Provider, cfg.LLM.APIMode, cfg.LLM.BaseURL, cfg.LLM.RequestPath, cfg.LLM.Model, cfg.LLM.APIKey, cfg.LLM.Timeout)
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "AI inference failed: "+err.Error())
		return
	}
	var out []AIRecommendationResponse
	if items != nil {
		out = make([]AIRecommendationResponse, 0, len(items))
	}
	for _, item := range items {
		out = append(out, AIRecommendationResponse{SeriesID: item.SeriesID, Reason: item.Reason, Title: item.Title, CoverPath: item.CoverPath})
	}
	jsonResponse(w, http.StatusOK, out)
}

func (c *ReaderHTTP) getSeriesByLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	libID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	series, err := c.store.ListSeriesByLibrary(ctx, libID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.ListSeriesByLibraryRow{}
	}
	jsonResponse(w, http.StatusOK, series)
}
