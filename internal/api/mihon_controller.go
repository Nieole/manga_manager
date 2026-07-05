// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

type MihonLibraryResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type MihonCollectionResponse struct {
	ID             string    `json:"id"`
	NumericID      int64     `json:"numeric_id"`
	Kind           string    `json:"kind"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	LibraryID      *int64    `json:"library_id,omitempty"`
	LibraryName    string    `json:"library_name,omitempty"`
	SeriesCount    int       `json:"series_count"`
	SourceType     string    `json:"source_type"`
	SourceReviewID *int64    `json:"source_review_id,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type MihonReadingListResponse struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ItemCount   int64     `json:"item_count"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MihonSeriesResponse struct {
	ID          int64     `json:"id"`
	LibraryID   int64     `json:"library_id"`
	Name        string    `json:"name"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Status      string    `json:"status"`
	BookCount   int64     `json:"book_count"`
	TotalPages  int64     `json:"total_pages"`
	CoverBookID int64     `json:"cover_book_id,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type MihonContinueItemResponse struct {
	SeriesID     int64     `json:"series_id"`
	SeriesName   string    `json:"series_name"`
	BookID       int64     `json:"book_id"`
	BookName     string    `json:"book_name"`
	BookTitle    string    `json:"book_title"`
	CoverURL     string    `json:"cover_url,omitempty"`
	LastReadPage int64     `json:"last_read_page,omitempty"`
	PageCount    int64     `json:"page_count"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type MihonSeriesPageResponse struct {
	Items   []MihonSeriesResponse `json:"items"`
	Total   int64                 `json:"total"`
	Page    int                   `json:"page"`
	Limit   int                   `json:"limit"`
	HasNext bool                  `json:"has_next"`
}

type MihonBookResponse struct {
	ID           int64     `json:"id"`
	SeriesID     int64     `json:"series_id"`
	Name         string    `json:"name"`
	Title        string    `json:"title"`
	Volume       string    `json:"volume"`
	Number       string    `json:"number"`
	SortNumber   float64   `json:"sort_number"`
	PageCount    int64     `json:"page_count"`
	LastReadPage int64     `json:"last_read_page,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type MihonPageResponse struct {
	Index     int64  `json:"index"`
	MediaType string `json:"media_type"`
	ImageURL  string `json:"image_url"`
}

func (c *Controller) setupMihonRoutes(r chi.Router) {
	r.Route("/mihon/v1", func(r chi.Router) {
		r.Use(c.requireProtocolEnabled("mihon"))
		// 阅读协议按站点用户 HTTP Basic 鉴权（多用户）；进度读写随之按当前用户。
		// authGate 已放行 /api/mihon/ 前缀，故此处的 Basic 鉴权是 Mihon 的唯一鉴权。
		r.Use(c.requireBasicAuth)
		r.Get("/libraries", c.mihonLibraries)
		r.Get("/recently-added", c.mihonRecentlyAdded)
		r.Get("/continue", c.mihonContinueReading)
		r.Get("/collections", c.mihonCollections)
		r.Get("/collections/{collectionId}/series", c.mihonCollectionSeries)
		r.Get("/reading-lists", c.mihonReadingLists)
		r.Get("/reading-lists/{listId}/series", c.mihonReadingListSeries)
		r.Get("/smart-collections/{filterId}/series", c.mihonSmartCollectionSeries)
		r.Get("/tags", c.mihonTags)
		r.Get("/authors", c.mihonAuthors)
		r.Get("/series", c.mihonSeries)
		r.Get("/series/{seriesId}", c.mihonSeriesDetail)
		r.Get("/series/{seriesId}/books", c.mihonSeriesBooks)
		r.Get("/books/{bookId}/pages", c.mihonBookPages)
		r.Get("/books/{bookId}/pages/{pageNumber}", c.servePageImage)
		r.Post("/books/{bookId}/progress", c.updateBookProgress)
	})
}

func (c *Controller) mihonLibraries(w http.ResponseWriter, r *http.Request) {
	libs, err := c.store.ListLibraries(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch libraries")
		return
	}
	items := make([]MihonLibraryResponse, 0, len(libs))
	for _, lib := range libs {
		items = append(items, MihonLibraryResponse{ID: lib.ID, Name: lib.Name})
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonRecentlyAdded(w http.ResponseWriter, r *http.Request) {
	page := positiveQueryInt(r, "page", 1, 0)
	limit := positiveQueryInt(r, "limit", 30, 100)
	libraryID := int64(positiveQueryInt(r, "libraryId", 0, 0))

	total, err := c.store.CountRecentAddedSeries(r.Context(), libraryID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count recent series")
		return
	}
	rows, err := c.store.ListRecentAddedSeries(r.Context(), database.ListRecentAddedSeriesParams{
		LibraryID: libraryID,
		Limit:     int64(limit),
		Offset:    int64((page - 1) * limit),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch recent series")
		return
	}
	items := make([]MihonSeriesResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, mihonSeriesFromRecentAddedRow(row))
	}
	jsonResponse(w, http.StatusOK, MihonSeriesPageResponse{
		Items:   items,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasNext: int64(page*limit) < total,
	})
}

func (c *Controller) mihonContinueReading(w http.ResponseWriter, r *http.Request) {
	limit := int64(positiveQueryInt(r, "limit", 30, 100))
	var (
		rows []database.GetRecentReadAllRow
		err  error
	)
	if uid := c.currentUserID(r); uid > 0 {
		rows, err = c.store.GetUserRecentReadAll(r.Context(), uid, limit)
	} else {
		rows, err = c.store.GetRecentReadAll(r.Context(), limit)
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch continue reading")
		return
	}
	items := make([]MihonContinueItemResponse, 0, len(rows))
	for _, row := range rows {
		lastReadPage := int64(0)
		if row.LastReadPage.Valid {
			lastReadPage = row.LastReadPage.Int64
		}
		updatedAt := time.Time{}
		if row.LastReadAt.Valid {
			updatedAt = row.LastReadAt.Time
		}
		coverURL := ""
		if row.CoverPath != "" {
			coverURL = "/api/thumbnails/" + row.CoverPath
		}
		items = append(items, MihonContinueItemResponse{
			SeriesID:     row.SeriesID,
			SeriesName:   row.SeriesName,
			BookID:       row.BookID,
			BookName:     row.BookName,
			BookTitle:    firstNonEmpty(row.BookTitle.String, row.BookName),
			CoverURL:     coverURL,
			LastReadPage: lastReadPage,
			PageCount:    row.PageCount,
			UpdatedAt:    updatedAt,
		})
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonCollections(w http.ResponseWriter, r *http.Request) {
	views, err := c.loadCollectionViews(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch collections")
		return
	}
	items := make([]MihonCollectionResponse, 0, len(views))
	for _, view := range views {
		items = append(items, mihonCollectionFromView(view))
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonReadingLists(w http.ResponseWriter, r *http.Request) {
	lists, err := c.store.ListReadingLists(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch reading lists")
		return
	}
	items := make([]MihonReadingListResponse, 0, len(lists))
	for _, list := range lists {
		items = append(items, MihonReadingListResponse{
			ID:          list.ID,
			Name:        list.Name,
			Description: list.Description,
			ItemCount:   list.ItemCount,
			UpdatedAt:   list.UpdatedAt,
		})
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonReadingListSeries(w http.ResponseWriter, r *http.Request) {
	listID, err := parseID(r, "listId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid reading list ID")
		return
	}
	if _, err := c.store.GetReadingList(r.Context(), listID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Reading list not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to fetch reading list")
		return
	}
	page := positiveQueryInt(r, "page", 1, 0)
	limit := positiveQueryInt(r, "limit", 30, 100)
	total, err := c.store.CountReadingListSeries(r.Context(), listID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to count reading list series")
		return
	}
	rows, err := c.store.ListReadingListSeriesPage(r.Context(), database.ListReadingListSeriesPageParams{
		ReadingListID: listID,
		Limit:         int64(limit),
		Offset:        int64((page - 1) * limit),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch reading list series")
		return
	}
	items := make([]MihonSeriesResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, mihonSeriesFromReadingListRow(row))
	}
	jsonResponse(w, http.StatusOK, MihonSeriesPageResponse{
		Items:   items,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasNext: int64(page*limit) < total,
	})
}

func (c *Controller) mihonCollectionSeries(w http.ResponseWriter, r *http.Request) {
	collectionID, err := parseID(r, "collectionId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid collection ID")
		return
	}
	page := positiveQueryInt(r, "page", 1, 0)
	limit := positiveQueryInt(r, "limit", 30, 100)
	_, rows, total, err := c.loadStaticCollectionSeries(r.Context(), collectionID, limit, (page-1)*limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Collection not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to fetch collection series")
		return
	}
	items := make([]MihonSeriesResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, mihonSeriesFromCollectionRow(row))
	}
	jsonResponse(w, http.StatusOK, MihonSeriesPageResponse{
		Items:   items,
		Total:   int64(total),
		Page:    page,
		Limit:   limit,
		HasNext: page*limit < total,
	})
}

func (c *Controller) mihonSmartCollectionSeries(w http.ResponseWriter, r *http.Request) {
	filterID, err := parseID(r, "filterId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid smart collection ID")
		return
	}
	filter, err := c.getSmartFilterByID(r, filterID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Smart collection not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to fetch smart collection")
		return
	}
	page := positiveQueryInt(r, "page", 1, 0)
	limit := positiveQueryInt(r, "limit", filter.PageSize, 100)
	rows, total, err := c.loadSmartCollectionSeries(r.Context(), filter, limit, (page-1)*limit, 0)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch smart collection series")
		return
	}
	items := make([]MihonSeriesResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, mihonSeriesFromSearchRow(row))
	}
	jsonResponse(w, http.StatusOK, MihonSeriesPageResponse{
		Items:   items,
		Total:   int64(total),
		Page:    page,
		Limit:   limit,
		HasNext: page*limit < total,
	})
}

func (c *Controller) mihonTags(w http.ResponseWriter, r *http.Request) {
	tags, err := c.store.GetAllTags(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch tags")
		return
	}
	items := make([]string, 0, len(tags))
	for _, tag := range tags {
		items = append(items, tag.Name)
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := c.store.GetAllAuthors(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch authors")
		return
	}
	items := make([]string, 0, len(authors))
	for _, a := range authors {
		items = append(items, a.Name)
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonSeries(w http.ResponseWriter, r *http.Request) {
	page := positiveQueryInt(r, "page", 1, 0)
	limit := positiveQueryInt(r, "limit", 30, 100)
	libraryID := int64(positiveQueryInt(r, "libraryId", 0, 0))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	offset := int64((page - 1) * limit)

	var tags []string
	if tagsParam := r.URL.Query().Get("tag"); tagsParam != "" {
		tags = strings.Split(tagsParam, ",")
	}

	var authors []string
	if authorsParam := r.URL.Query().Get("author"); authorsParam != "" {
		authors = strings.Split(authorsParam, ",")
	}

	status := strings.TrimSpace(r.URL.Query().Get("status"))
	sortBy := strings.TrimSpace(r.URL.Query().Get("sort"))

	// FTS 快路径仅用于「跨库搜索」（libraryID==0）。若叠加了 libraryID 过滤，FTS 返回的是
	// 跨库分页结果，再按库做内存过滤会丢结果、把 total 算成当前页过滤后的条数，导致分页彻底失效。
	// 因此库内搜索直接回落到 SearchSeriesPaged —— 它原生支持 libraryID + 关键字 + 正确的 LIMIT/OFFSET。
	if query != "" && libraryID == 0 && len(tags) == 0 && len(authors) == 0 && status == "" {
		if rows, searchTotal, usedEngine, err := c.searchProtocolSeries(r.Context(), query, page, limit); usedEngine {
			if err != nil {
				jsonError(w, http.StatusInternalServerError, "Failed to search series")
				return
			}
			items := make([]MihonSeriesResponse, 0, len(rows))
			for _, row := range rows {
				items = append(items, mihonSeriesFromProtocolRow(row))
			}
			total := int64(searchTotal)
			jsonResponse(w, http.StatusOK, MihonSeriesPageResponse{
				Items:   items,
				Total:   total,
				Page:    page,
				Limit:   limit,
				HasNext: int64(page*limit) < total,
			})
			return
		}
	}

	// Map Mihon sort parameters to our SearchSeriesPaged sort parameters
	// Our backend uses format: field_dir (e.g. updated_DESC)
	searchSortBy := ""
	switch sortBy {
	case "updated_desc":
		searchSortBy = "updated_DESC"
	case "books_desc":
		searchSortBy = "books_DESC"
	default:
		searchSortBy = "name_ASC"
	}

	rows, total, err := c.store.SearchSeriesPaged(
		r.Context(),
		libraryID,
		database.SeriesListFilters{
			Keyword: query,
			Status:  status,
			Tags:    tags,
			Authors: authors,
		},
		int32(limit),
		int32(offset),
		searchSortBy,
	)

	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}

	items := make([]MihonSeriesResponse, 0, len(rows))
	for _, row := range rows {
		coverURL := ""
		if row.CoverPath.Valid && row.CoverPath.String != "" {
			coverURL = "/api/thumbnails/" + row.CoverPath.String
		}
		items = append(items, MihonSeriesResponse{
			ID:          row.ID,
			LibraryID:   row.LibraryID,
			Name:        row.Name,
			Title:       firstNonEmpty(row.Title.String, row.Name),
			Summary:     row.Summary.String,
			Status:      row.Status.String,
			BookCount:   int64(row.ActualBookCount),
			TotalPages:  int64(row.TotalPages.Float64),
			CoverBookID: 0,
			CoverURL:    coverURL,
			UpdatedAt:   row.UpdatedAt,
		})
	}

	jsonResponse(w, http.StatusOK, MihonSeriesPageResponse{
		Items:   items,
		Total:   int64(total),
		Page:    page,
		Limit:   limit,
		HasNext: int64(page*limit) < int64(total),
	})
}

func (c *Controller) mihonSeriesDetail(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	row, err := c.store.GetMihonSeries(r.Context(), seriesID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Series not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to fetch series")
		return
	}
	jsonResponse(w, http.StatusOK, mihonSeriesFromDetailRow(row))
}

func (c *Controller) mihonSeriesBooks(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}
	books, err := c.store.ListBooksBySeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to fetch books")
		return
	}
	// 与普通阅读接口保持一致：SQL 的 ORDER BY volume, sort_number, name 只是字典序，
	// 无法处理卷号数值排序与从文件名/标题中提取的「第N话/第N卷」序数，需在代码层用
	// booksort 重排，否则 mihon 客户端里章节顺序会乱（如 10 排在 2 之前）。
	sortBooksForReading(books)
	c.overlayUserProgress(r.Context(), c.currentUserID(r), books)
	items := make([]MihonBookResponse, 0, len(books))
	for _, book := range books {
		items = append(items, mihonBookFromModel(book))
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) mihonBookPages(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	book, err := c.store.GetBook(r.Context(), bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}
	pages, err := c.listBookArchivePages(r.Context(), book)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read pages")
		return
	}

	query := mihonImageQuery(r)
	items := make([]MihonPageResponse, 0, len(pages))
	for i, page := range pages {
		pageNumber := int64(i + 1)
		imageURL := fmt.Sprintf("/api/mihon/v1/books/%d/pages/%d%s", bookID, pageNumber, query)
		items = append(items, MihonPageResponse{
			Index:     pageNumber,
			MediaType: page.MediaType,
			ImageURL:  imageURL,
		})
	}
	jsonResponse(w, http.StatusOK, items)
}

func positiveQueryInt(r *http.Request, key string, fallback, max int) int {
	raw := r.URL.Query().Get(key)
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	if value == 0 && fallback > 0 {
		return fallback
	}
	if max > 0 && value > max {
		return max
	}
	return value
}

func mihonImageQuery(r *http.Request) string {
	values := r.URL.Query()
	query := make([]string, 0, 2)
	if format := values.Get("format"); format == "webp" || format == "jpeg" || format == "jpg" {
		query = append(query, "format="+format)
	}
	if quality := values.Get("q"); quality != "" {
		if q, err := strconv.Atoi(quality); err == nil && q >= 1 && q <= 100 {
			query = append(query, "q="+strconv.Itoa(q))
		}
	}
	if len(query) == 0 {
		return ""
	}
	return "?" + strings.Join(query, "&")
}

func mihonSeriesFromDetailRow(row database.GetMihonSeriesRow) MihonSeriesResponse {
	return MihonSeriesResponse{
		ID:          row.ID,
		LibraryID:   row.LibraryID,
		Name:        row.Name,
		Title:       firstNonEmpty(row.Title, row.Name),
		Summary:     row.Summary,
		Status:      row.Status,
		BookCount:   row.BookCount,
		TotalPages:  row.TotalPages,
		CoverBookID: row.CoverBookID,
		CoverURL:    mihonCoverURL(row.CoverBookID),
		UpdatedAt:   row.UpdatedAt,
	}
}

func mihonCollectionFromView(view CollectionView) MihonCollectionResponse {
	return MihonCollectionResponse{
		ID:             view.ID,
		NumericID:      view.NumericID,
		Kind:           view.Kind,
		Name:           view.Name,
		Description:    view.Description,
		LibraryID:      view.LibraryID,
		LibraryName:    view.LibraryName,
		SeriesCount:    view.SeriesCount,
		SourceType:     view.SourceType,
		SourceReviewID: view.SourceReviewID,
		UpdatedAt:      view.UpdatedAt,
	}
}

func mihonSeriesFromCollectionRow(row collectionSeriesListItem) MihonSeriesResponse {
	coverURL := ""
	if row.CoverPath != "" {
		coverURL = "/api/thumbnails/" + row.CoverPath
	}
	return MihonSeriesResponse{
		ID:         row.ID,
		LibraryID:  row.LibraryID,
		Name:       row.Name,
		Title:      firstNonEmpty(row.Title, row.Name),
		Summary:    row.Summary,
		Status:     row.Status,
		BookCount:  row.BookCount,
		TotalPages: row.TotalPages,
		CoverURL:   coverURL,
		UpdatedAt:  row.UpdatedAt,
	}
}

func mihonSeriesFromRecentAddedRow(row database.ListRecentAddedSeriesRow) MihonSeriesResponse {
	coverURL := ""
	if row.CoverPath != "" {
		coverURL = "/api/thumbnails/" + row.CoverPath
	} else {
		coverURL = mihonCoverURL(row.CoverBookID)
	}
	return MihonSeriesResponse{
		ID:          row.ID,
		LibraryID:   row.LibraryID,
		Name:        row.Name,
		Title:       firstNonEmpty(row.Title, row.Name),
		Summary:     row.Summary,
		Status:      row.Status,
		BookCount:   row.BookCount,
		TotalPages:  row.TotalPages,
		CoverBookID: row.CoverBookID,
		CoverURL:    coverURL,
		UpdatedAt:   row.UpdatedAt,
	}
}

func mihonSeriesFromReadingListRow(row database.ListReadingListSeriesPageRow) MihonSeriesResponse {
	coverURL := ""
	if row.CoverPath != "" {
		coverURL = "/api/thumbnails/" + row.CoverPath
	} else {
		coverURL = mihonCoverURL(row.CoverBookID)
	}
	return MihonSeriesResponse{
		ID:          row.SeriesID,
		LibraryID:   row.LibraryID,
		Name:        row.Name,
		Title:       firstNonEmpty(row.Title, row.Name),
		Summary:     row.Summary,
		Status:      row.Status,
		BookCount:   row.BookCount,
		TotalPages:  row.TotalPages,
		CoverBookID: row.CoverBookID,
		CoverURL:    coverURL,
		UpdatedAt:   row.UpdatedAt,
	}
}

func mihonSeriesFromProtocolRow(row database.ProtocolSeriesRow) MihonSeriesResponse {
	coverURL := ""
	if row.CoverPath != "" {
		coverURL = "/api/thumbnails/" + row.CoverPath
	} else {
		coverURL = mihonCoverURL(row.CoverBookID)
	}
	return MihonSeriesResponse{
		ID:          row.ID,
		LibraryID:   row.LibraryID,
		Name:        row.Name,
		Title:       firstNonEmpty(row.Title, row.Name),
		Summary:     row.Summary,
		Status:      row.Status,
		BookCount:   row.BookCount,
		TotalPages:  row.TotalPages,
		CoverBookID: row.CoverBookID,
		CoverURL:    coverURL,
		UpdatedAt:   row.UpdatedAt,
	}
}

func mihonSeriesFromSearchRow(row database.SearchSeriesPagedRow) MihonSeriesResponse {
	coverURL := ""
	if row.CoverPath.Valid && row.CoverPath.String != "" {
		coverURL = "/api/thumbnails/" + row.CoverPath.String
	}
	totalPages := int64(0)
	if row.TotalPages.Valid {
		totalPages = int64(row.TotalPages.Float64)
	}
	return MihonSeriesResponse{
		ID:         row.ID,
		LibraryID:  row.LibraryID,
		Name:       row.Name,
		Title:      firstNonEmpty(row.Title.String, row.Name),
		Summary:    row.Summary.String,
		Status:     row.Status.String,
		BookCount:  int64(row.ActualBookCount),
		TotalPages: totalPages,
		CoverURL:   coverURL,
		UpdatedAt:  row.UpdatedAt,
	}
}

func mihonBookFromModel(book database.Book) MihonBookResponse {
	lastReadPage := int64(0)
	if book.LastReadPage.Valid {
		lastReadPage = book.LastReadPage.Int64
	}
	sortNumber := float64(0)
	if book.SortNumber.Valid {
		sortNumber = book.SortNumber.Float64
	}
	return MihonBookResponse{
		ID:           book.ID,
		SeriesID:     book.SeriesID,
		Name:         book.Name,
		Title:        firstNonEmpty(nullString(book.Title), book.Name),
		Volume:       book.Volume,
		Number:       nullString(book.Number),
		SortNumber:   sortNumber,
		PageCount:    book.PageCount,
		LastReadPage: lastReadPage,
		UpdatedAt:    book.UpdatedAt,
	}
}

func mihonCoverURL(bookID int64) string {
	if bookID <= 0 {
		return ""
	}
	return fmt.Sprintf("/api/covers/%d", bookID)
}
