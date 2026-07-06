// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的系列/标签/作者子域，负责系列分页搜索、系列信息与上下文、标签与作者的查询/搜索接口。

package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"manga-manager/internal/booksort"
	"manga-manager/internal/database"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
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

	limitStr := r.URL.Query().Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit <= 0 {
		limit = 50
	}
	// 上限保护：limit 只有下限校验时，一个 limit=1000000 就能让本端点物化并 JSON 编码整库
	// (~24 列含 summary 长文本) 的全部系列，等同单请求 OOM/超时。UI 最大页大小为 100，200 留足余量。
	if limit > maxSeriesPageLimit {
		limit = maxSeriesPageLimit
	}

	pageStr := r.URL.Query().Get("page")
	page, err := strconv.Atoi(pageStr)
	if err != nil || page <= 0 {
		page = 1
	}
	offset := (page - 1) * limit

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
	filters := database.SeriesListFilters{
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
		UserID:          c.currentUserID(r),
	}

	if cursor != "" {
		series, nextCursor, hasMore, err := c.store.SearchSeriesCursor(ctx, libID, filters, int32(limit), sortBy, cursor)
		if err != nil {
			slog.Error("SearchSeriesCursor Failed", "error", err)
			jsonError(w, http.StatusBadRequest, "Invalid cursor")
			return
		}
		if series == nil {
			series = []database.SearchSeriesPagedRow{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"items":       series,
			"total":       0,
			"page":        page,
			"limit":       limit,
			"next_cursor": nextCursor,
			"has_more":    hasMore,
		})
		return
	}

	series, total, err := c.store.SearchSeriesPaged(ctx, libID, filters, int32(limit), int32(offset), sortBy)
	if err != nil {
		slog.Error("SearchSeriesPaged Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	if series == nil {
		series = []database.SearchSeriesPagedRow{}
	}
	hasMore := page*limit < total
	nextCursor := ""
	if hasMore && len(series) > 0 && database.SeriesSearchSortSupportsCursor(sortBy) {
		nextCursor = database.NextSeriesSearchCursor(sortBy, series[len(series)-1])
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items":       series,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"next_cursor": nextCursor,
		"has_more":    hasMore,
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

	limitStr := r.URL.Query().Get("limit")
	limit := int64(10) // 默认读取 10 条
	if limitStr != "" {
		if l, err := strconv.ParseInt(limitStr, 10, 64); err == nil && l > 0 {
			limit = l
		}
	}

	ctx := r.Context()
	var items []database.GetRecentReadSeriesRow
	if uid := c.currentUserID(r); uid > 0 {
		items, err = c.store.GetUserRecentReadSeries(ctx, uid, libID, limit)
	} else {
		items, err = c.store.GetRecentReadSeries(ctx, database.GetRecentReadSeriesParams{
			LibraryID:   libID,
			LibraryID_2: libID,
			Limit:       limit,
		})
	}
	if err != nil {
		slog.Error("GetRecentReadSeries Failed", "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to fetch recent read series")
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"items": items,
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

type UpdateAuthorRequest struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type UpdateLinkRequest struct {
	Name string `json:"name"`
	Url  string `json:"url"`
}

type UpdateSeriesRequest struct {
	Title        string                `json:"title"`
	Summary      string                `json:"summary"`
	Publisher    string                `json:"publisher"`
	Status       string                `json:"status"`
	Rating       float64               `json:"rating"`
	Language     string                `json:"language"`
	LockedFields string                `json:"locked_fields"`
	Tags         []string              `json:"tags"`
	Authors      []UpdateAuthorRequest `json:"authors"`
	Links        []UpdateLinkRequest   `json:"links"`
}

func (c *Controller) updateSeriesInfo(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	var req UpdateSeriesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	currentSeries, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	err = c.store.ExecTx(r.Context(), func(q *database.Queries) error {
		_, err := q.UpdateSeriesMetadata(r.Context(), database.UpdateSeriesMetadataParams{
			Title:        sql.NullString{String: req.Title, Valid: req.Title != ""},
			Summary:      sql.NullString{String: req.Summary, Valid: req.Summary != ""},
			Publisher:    sql.NullString{String: req.Publisher, Valid: req.Publisher != ""},
			Status:       sql.NullString{String: req.Status, Valid: req.Status != ""},
			Rating:       sql.NullFloat64{Float64: req.Rating, Valid: req.Rating > 0},
			Language:     sql.NullString{String: req.Language, Valid: req.Language != ""},
			LockedFields: sql.NullString{String: req.LockedFields, Valid: true},
			NameInitial:  database.SeriesInitial(req.Title, currentSeries.Name),
			ID:           seriesID,
		})
		if err != nil {
			return err
		}

		if req.Tags != nil {
			_ = q.ClearSeriesTags(r.Context(), seriesID)
			for _, t := range req.Tags {
				if strings.TrimSpace(t) == "" {
					continue
				}
				if inserted, err := q.UpsertTag(r.Context(), t); err == nil {
					_ = q.LinkSeriesTag(r.Context(), database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
				}
			}
		}

		if req.Authors != nil {
			_ = q.ClearSeriesAuthors(r.Context(), seriesID)
			for _, a := range req.Authors {
				if strings.TrimSpace(a.Name) == "" {
					continue
				}
				if inserted, err := q.UpsertAuthor(r.Context(), database.UpsertAuthorParams{Name: a.Name, Role: a.Role}); err == nil {
					_ = q.LinkSeriesAuthor(r.Context(), database.LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: inserted.ID})
				}
			}
		}

		if req.Links != nil {
			_ = q.ClearSeriesLinks(r.Context(), seriesID)
			for _, link := range req.Links {
				if strings.TrimSpace(link.Name) == "" || strings.TrimSpace(link.Url) == "" {
					continue
				}
				_, _ = q.LinkSeriesLink(r.Context(), database.LinkSeriesLinkParams{
					SeriesID: seriesID,
					Name:     link.Name,
					Url:      link.Url,
				})
			}
		}

		return q.RefreshSeriesStats(r.Context(), seriesID)
	})

	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update series metadata")
		return
	}

	// Fetch updated details for response
	updated, _ := c.store.GetSeries(r.Context(), seriesID)
	jsonResponse(w, http.StatusOK, updated)
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
	FailedTasks       []TaskStatus            `json:"failed_tasks"`
	FailedTaskSummary SeriesFailedTaskSummary `json:"failed_task_summary"`
	Continue          SeriesContinue          `json:"continue"`
}

// SeriesContinue 描述用户在某系列内的续读位置，用于资源库 / 详情页 CTA。
type SeriesContinue struct {
	NextUnreadBookID int64      `json:"next_unread_book_id,omitempty"`
	LastReadBookID   int64      `json:"last_read_book_id,omitempty"`
	LastReadPage     int64      `json:"last_read_page,omitempty"`
	LastReadAt       *time.Time `json:"last_read_at,omitempty"`
	TotalBooks       int        `json:"total_books"`
	ReadBooks        int        `json:"read_books"`
	TotalPages       int64      `json:"total_pages"`
	ReadPages        int64      `json:"read_pages"`
}

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

	ctx := r.Context()

	// 1. 获取系列基本信息
	series, err := c.store.GetSeries(ctx, seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	// 2. 获取书籍列表
	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch books for context", "series_id", seriesID, "error", err)
	}
	if books == nil {
		books = []database.Book{}
	}
	sortBooksForReading(books)
	c.overlayUserProgress(ctx, c.currentUserID(r), books)

	// 3. 标签
	tags, err := c.store.GetTagsForSeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch tags for context", "series_id", seriesID, "error", err)
	}
	if tags == nil {
		tags = []database.Tag{}
	}

	// 4. 作者
	authors, err := c.store.GetAuthorsForSeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch authors for context", "series_id", seriesID, "error", err)
	}
	if authors == nil {
		authors = []database.Author{}
	}

	// 5. 链接
	links, err := c.store.GetLinksForSeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch links for context", "series_id", seriesID, "error", err)
	}
	if links == nil {
		links = []database.SeriesLink{}
	}

	relations, err := c.loadSeriesRelations(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch relations for context", "series_id", seriesID, "error", err)
		relations = []SeriesRelation{}
	}

	metadataReview, err := c.loadSeriesMetadataReview(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch metadata review for context", "series_id", seriesID, "error", err)
		metadataReview = emptyMetadataReviewResponse()
	}

	failedTasks, err := c.listTaskStatuses(ctx, database.TaskFilters{
		Status:  "failed",
		Scope:   "series",
		ScopeID: &seriesID,
		Limit:   5,
	})
	if err != nil {
		slog.Error("Failed to fetch failed tasks for context", "series_id", seriesID, "error", err)
		failedTasks = []TaskStatus{}
	}
	if failedTasks == nil {
		failedTasks = []TaskStatus{}
	}

	jsonResponse(w, http.StatusOK, SeriesContextResponse{
		Series:            series,
		Books:             books,
		Tags:              tags,
		Authors:           authors,
		Links:             links,
		Volumes:           buildSeriesVolumeSummaries(books, false),
		Relations:         relations,
		MetadataReview:    metadataReview,
		MetadataSummary:   summarizeSeriesMetadata(metadataReview),
		FailedTasks:       failedTasks,
		FailedTaskSummary: summarizeFailedTasks(failedTasks),
		Continue:          buildSeriesContinue(books),
	})
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

	books, err := c.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		slog.Error("Failed to fetch books for continue", "series_id", seriesID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to compute continue position")
		return
	}
	sortBooksForReading(books)
	c.overlayUserProgress(ctx, c.currentUserID(r), books)
	jsonResponse(w, http.StatusOK, buildSeriesContinue(books))
}

// buildSeriesContinue 假设 books 已按阅读顺序排序。
// 规则：
//   - next_unread_book_id：第一本未完成的书（last_read_page < page_count，含完全未读）。
//   - last_read_book_id：last_read_at 最大的书，用作"上次读到这里"。
//   - 全部读完时 next_unread 为 0；用户可前端落到 first 或 last。
func buildSeriesContinue(books []database.Book) SeriesContinue {
	out := SeriesContinue{TotalBooks: len(books)}
	var latestAt *time.Time
	for i := range books {
		book := books[i]
		out.TotalPages += book.PageCount
		readPages := int64(0)
		if book.LastReadPage.Valid {
			readPages = book.LastReadPage.Int64
			if book.PageCount > 0 && readPages > book.PageCount {
				readPages = book.PageCount
			}
		}
		out.ReadPages += readPages
		isFinished := book.PageCount > 0 && readPages >= book.PageCount
		if isFinished {
			out.ReadBooks++
		}
		if out.NextUnreadBookID == 0 && !isFinished {
			out.NextUnreadBookID = book.ID
		}
		if book.LastReadAt.Valid {
			at := book.LastReadAt.Time
			if latestAt == nil || at.After(*latestAt) {
				captured := at
				latestAt = &captured
				out.LastReadBookID = book.ID
				out.LastReadPage = readPages
			}
		}
	}
	if latestAt != nil {
		out.LastReadAt = latestAt
	}
	return out
}

func buildSeriesVolumeSummaries(books []database.Book, includeBooks bool) []SeriesVolumeSummary {
	type volumeAccumulator struct {
		summary SeriesVolumeSummary
		books   []database.Book
	}
	volumeMap := make(map[string]*volumeAccumulator)
	for _, book := range books {
		volumeName := strings.TrimSpace(book.Volume)
		if volumeName == "" {
			continue
		}
		acc, ok := volumeMap[volumeName]
		if !ok {
			acc = &volumeAccumulator{summary: SeriesVolumeSummary{Name: volumeName}}
			volumeMap[volumeName] = acc
		}
		acc.summary.BookCount++
		acc.summary.TotalPages += book.PageCount
		if book.LastReadPage.Valid {
			readPages := book.LastReadPage.Int64
			if book.PageCount > 0 && readPages > book.PageCount {
				readPages = book.PageCount
			}
			acc.summary.ReadPages += readPages
		}
		if acc.summary.CoverBookID == 0 && book.CoverPath.Valid && strings.TrimSpace(book.CoverPath.String) != "" {
			acc.summary.CoverBookID = book.ID
			acc.summary.CoverPath = book.CoverPath
			acc.summary.UpdatedAt = book.UpdatedAt
		}
		if includeBooks {
			acc.books = append(acc.books, book)
		}
	}
	items := make([]SeriesVolumeSummary, 0, len(volumeMap))
	for _, acc := range volumeMap {
		if includeBooks {
			acc.summary.Books = acc.books
		}
		items = append(items, acc.summary)
	}
	sort.Slice(items, func(i, j int) bool {
		return booksort.CompareLabels(items[i].Name, items[j].Name) < 0
	})
	return items
}

func summarizeFailedTasks(tasks []TaskStatus) SeriesFailedTaskSummary {
	summary := SeriesFailedTaskSummary{Count: len(tasks)}
	for _, task := range tasks {
		if summary.LatestAt == nil || task.UpdatedAt.After(*summary.LatestAt) {
			updatedAt := task.UpdatedAt
			summary.LatestAt = &updatedAt
		}
	}
	return summary
}

func summarizeSeriesMetadata(review metadataReviewResponse) SeriesMetadataSummary {
	return SeriesMetadataSummary{
		PendingReviewCount: len(review.Reviews),
		ProvenanceCount:    len(review.Provenance),
	}
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
