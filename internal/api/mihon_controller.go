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
	"manga-manager/internal/parser"

	"github.com/go-chi/chi/v5"
)

type MihonLibraryResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
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
		r.Get("/libraries", c.mihonLibraries)
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
		query,  // keyword
		"",     // letter
		status, // status
		tags,
		authors,
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
		items = append(items, MihonSeriesResponse{
			ID:          row.ID,
			LibraryID:   row.LibraryID,
			Name:        row.Name,
			Title:       firstNonEmpty(row.Title.String, row.Name),
			Summary:     row.Summary.String,
			Status:      row.Status.String,
			BookCount:   int64(row.ActualBookCount),
			TotalPages:  int64(row.TotalPages.Float64),
			CoverBookID: 0, // Not needed, CoverURL handles it
			CoverURL:    row.CoverPath.String,
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
	arc, err := parser.OpenArchive(book.Path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to open archive")
		return
	}
	defer arc.Close()

	pages, err := arc.GetPages()
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
