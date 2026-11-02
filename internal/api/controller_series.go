package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"manga-manager/internal/seriescontext"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

// parseReadState 归一化阅读状态查询参数，未识别值按“不筛选”处理。
func parseReadState(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "unread", "reading", "completed":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

// parseOptionalFloat 把可选的浮点查询参数解析为 *float64；空串或非法值返回 nil（不筛选）。
func parseOptionalFloat(v string) *float64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

// parseNonNegativeInt 解析非负整数查询参数；空串或非法/负值返回 0（不筛选）。
func parseNonNegativeInt(v string) int {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// maxSeriesPageLimit 是 /api/series/search 单页返回的硬上限，防止超大 limit 拉全库导致 OOM/超时。
const maxSeriesPageLimit = 200

func (c *Controller) searchSeriesPaged(w http.ResponseWriter, r *http.Request) {
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
	view, err := reading.NewCatalogueQueries(c.store).Search(ctx, subject, reading.CatalogueSearch{
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

// getRecentReadSeries 返回该资源库下含有书籍最新阅读记录的系列
func (c *Controller) getRecentReadSeries(w http.ResponseWriter, r *http.Request) {
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
	view, err := reading.NewCatalogueQueries(c.store).Recent(ctx, subject, libID, limit)
	if err != nil {
		slog.Error("GetRecentReadSeries Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch recent read series")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items": view.Items,
	})
}

func (c *Controller) getSeriesInfo(w http.ResponseWriter, r *http.Request) {
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

func (c *Controller) openSeriesDirectory(w http.ResponseWriter, r *http.Request) {
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

	release, ok := c.guardLibraryRead(w, series.LibraryID)
	if !ok {
		return
	}
	defer release()
	series, err = c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	path := strings.TrimSpace(series.Path)
	if path == "" {
		jsonError(w, http.StatusBadRequest, "Series directory is not available")
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Series directory does not exist")
		return
	}
	if !info.IsDir() {
		jsonError(w, http.StatusBadRequest, "Series path is not a directory")
		return
	}

	opener := c.openPath
	if opener == nil {
		opener = openPathInDefaultFileManager
	}
	if err := opener(path); err != nil {
		slog.Error("OpenSeriesDirectory Failed", "series_id", seriesID, "path", path, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to open series directory")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]any{"success": true})
}

func (c *Controller) getSeriesTags(w http.ResponseWriter, r *http.Request) {
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

func (c *Controller) getSeriesAuthors(w http.ResponseWriter, r *http.Request) {
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

func (c *Controller) getSeriesLinks(w http.ResponseWriter, r *http.Request) {
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

type SeriesContextResponse struct {
	Series            database.Series         `json:"series"`
	Books             []database.Book         `json:"books"`
	Tags              []database.Tag          `json:"tags"`
	Authors           []database.Author       `json:"authors"`
	Links             []database.SeriesLink   `json:"links"`
	Volumes           []SeriesVolumeSummary   `json:"volumes"`
	Relations         []SeriesRelation        `json:"relations"`
	MetadataReview    metadataReviewResponse  `json:"metadata_review"`
	MetadataSummary   SeriesMetadataSummary   `json:"metadata_summary"`
	FailedTasks       []RunSnapshot           `json:"failed_tasks"`
	FailedTaskSummary SeriesFailedTaskSummary `json:"failed_task_summary"`
	Continue          SeriesContinue          `json:"continue"`
	// MetadataVersion 是本次下发的元数据版本，用户按下保存时原样带回（见 seriesmeta.Version）。
	MetadataVersion string `json:"metadata_version"`
}

// SeriesContinue is the shared subject reading summary exposed over HTTP.
type SeriesContinue = reading.Continue

type SeriesVolumeSummary struct {
	Name        string          `json:"name"`
	BookCount   int             `json:"book_count"`
	TotalPages  int64           `json:"total_pages"`
	ReadPages   int64           `json:"read_pages"`
	CoverBookID int64           `json:"cover_book_id,omitempty"`
	CoverPath   sql.NullString  `json:"cover_path"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Books       []database.Book `json:"books,omitempty"`
}

type SeriesFailedTaskSummary struct {
	Count    int        `json:"count"`
	LatestAt *time.Time `json:"latest_at,omitempty"`
}

type SeriesMetadataSummary struct {
	PendingReviewCount int `json:"pending_review_count"`
	ProvenanceCount    int `json:"provenance_count"`
}

func (c *Controller) getSeriesContext(w http.ResponseWriter, r *http.Request) {
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
	out, err := seriescontext.New(c.store, c.relations, c.proposals, c.taskEngine.engine).Load(r.Context(), subject, seriesID)
	if err != nil {
		if errors.Is(err, seriescontext.ErrMissing) {
			jsonError(w, http.StatusNotFound, "Series not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "Failed to load series context")
		}
		return
	}
	failedTasks, err := c.taskEngine.statusesFrom(r.Context(), out.FailedRuns)
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

func (c *Controller) getSeriesContinueEndpoint(w http.ResponseWriter, r *http.Request) {
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
	view, err := reading.NewQueries(c.store).SeriesBooks(ctx, subject, seriesID)
	if err != nil {
		slog.Error("Failed to fetch books for continue", "series_id", seriesID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to compute continue position")
		return
	}
	jsonResponse(w, http.StatusOK, view.Continue)
}

func buildSeriesVolumeSummaries(books []reading.BookView, includeBooks bool) []SeriesVolumeSummary {
	volumes := seriescontext.Volumes(books, includeBooks)
	out := make([]SeriesVolumeSummary, 0, len(volumes))
	for _, v := range volumes {
		out = append(out, SeriesVolumeSummary{Name: v.Name, BookCount: v.BookCount, TotalPages: v.TotalPages, ReadPages: v.ReadPages, CoverBookID: v.CoverBookID, CoverPath: v.CoverPath, UpdatedAt: v.UpdatedAt, Books: readerBookResponses(v.Books)})
	}
	return out
}

func (c *Controller) getAllTags(w http.ResponseWriter, r *http.Request) {
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

// getSeriesCustomFields 返回某系列的自定义字段列表。
func (c *Controller) getSeriesCustomFields(w http.ResponseWriter, r *http.Request) {
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

// replaceSeriesCustomFields 整体替换某系列的自定义字段。
func (c *Controller) replaceSeriesCustomFields(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	var req struct {
		Fields []database.SeriesCustomField `json:"fields"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if err := c.store.ReplaceSeriesCustomFields(r.Context(), seriesID, req.Fields); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save custom fields")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// renameTag 重命名标签；与已有标签重名会因 UNIQUE 约束失败，前端据此提示改用合并。
func (c *Controller) renameTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseID(r, "tagId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid tag ID")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		jsonError(w, http.StatusBadRequest, "Tag name cannot be empty")
		return
	}
	if err := c.store.RenameTag(r.Context(), tagID, req.Name); err != nil {
		jsonError(w, http.StatusConflict, apiText(requestLocale(r), "tag.rename.conflict"))
		return
	}
	c.invalidateDashboardStatsCache("tag_rename")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// mergeTag 把 {tagId} 标签并入 target 标签（迁移全部系列关联后删除源标签）。
func (c *Controller) mergeTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseID(r, "tagId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid tag ID")
		return
	}
	var req struct {
		TargetID int64 `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.TargetID <= 0 {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if err := c.store.MergeTags(r.Context(), tagID, req.TargetID); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to merge tags")
		return
	}
	c.invalidateDashboardStatsCache("tag_merge")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (c *Controller) deleteTag(w http.ResponseWriter, r *http.Request) {
	tagID, err := parseID(r, "tagId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid tag ID")
		return
	}
	if err := c.store.DeleteTag(r.Context(), tagID); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete tag")
		return
	}
	c.invalidateDashboardStatsCache("tag_delete")
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseFacetSearchLimit(r *http.Request) int {
	limit := 30
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		return 30
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func (c *Controller) searchTags(w http.ResponseWriter, r *http.Request) {
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

func (c *Controller) getAllAuthors(w http.ResponseWriter, r *http.Request) {
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

func (c *Controller) searchAuthors(w http.ResponseWriter, r *http.Request) {
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
