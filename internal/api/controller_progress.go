package api

import (
	"database/sql"
	"encoding/json"
	"errors"

	"net/http"
	"strings"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func (c *Controller) getNextBook(w http.ResponseWriter, r *http.Request) {
	c.getAdjacentBook(w, r, true)
}
func (c *Controller) getPrevBook(w http.ResponseWriter, r *http.Request) {
	c.getAdjacentBook(w, r, false)
}

func (c *Controller) getAdjacentBook(w http.ResponseWriter, r *http.Request, next bool) {
	id, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	message := "No previous book"
	if next {
		message = "No next book"
	}
	book, err := reading.NewQueries(c.store).Adjacent(r.Context(), c.currentUserID(r), id, next)
	if err != nil {
		jsonError(w, http.StatusNotFound, message)
		return
	}
	jsonResponse(w, http.StatusOK, book)
}

func sortBooksForReading(books []database.Book) { reading.SortBooks(books) }

type UpdateProgressRequest struct {
	Page      int64      `json:"page"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	FactID    string     `json:"fact_id,omitempty"`
}

// clampPageToBook 把用户报上来的页码夹进这本书的合法范围。page_count 为 0 表示页数未知
// （快速扫描不开归档，首扫即得 0），此时只保证下界，不把页码夹成 1。
func clampPageToBook(page, pageCount int64) int64 {
	if page < 1 {
		return 1
	}
	if pageCount > 0 && page > pageCount {
		return pageCount
	}
	return page
}

func (c *Controller) updateBookProgress(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	var req UpdateProgressRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	service := reading.New(c.store, func() { c.invalidateVolatileStatsCache("book_progress") })
	result, err := service.RecordPosition(r.Context(), reading.Position{UserID: c.currentUserID(r), BookID: bookID, Page: req.Page, OccurredAt: req.UpdatedAt, FactID: req.FactID})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Book not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "Failed to update progress")
		}
		return
	}
	status := "Progress unchanged"
	if result.Applied {
		status = "Progress updated"
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": status})
}

// ReadingTimeRequest 是阅读时长增量上报体（活跃阅读秒数）。
type ReadingTimeRequest struct {
	Seconds int64 `json:"seconds"`
}

// addBookReadingTime 累加当前用户在某书的活跃阅读秒数。经 navigator.sendBeacon 上报（CSRF 豁免，见 isCsrfExempt），
// 仍要求有效会话；未登录（首启/单用户）静默接受不落库。
func (c *Controller) addBookReadingTime(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	uid := c.currentUserID(r)
	var req ReadingTimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if err := reading.New(c.store, nil).RecordTime(r.Context(), reading.TimeReport{UserID: uid, BookID: bookID, Seconds: req.Seconds}); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to record reading time")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

type BulkSyncProgressItem struct {
	BookID    int64      `json:"book_id"`
	Page      int64      `json:"page"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	FactID    string     `json:"fact_id,omitempty"`
}

type BulkSyncProgressRequest struct {
	Items []BulkSyncProgressItem `json:"items"`
}

type BulkSyncProgressResultItem struct {
	BookID  int64  `json:"book_id"`
	Status  string `json:"status"` // updated | skipped_stale | skipped_unchanged | not_found | invalid
	Page    int64  `json:"page,omitempty"`
	Message string `json:"message,omitempty"`
}

// bulkSyncBookProgress translates the ordered offline reports into the reading use case.
func (c *Controller) bulkSyncBookProgress(w http.ResponseWriter, r *http.Request) {
	var req BulkSyncProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	positions := make([]reading.Position, 0, len(req.Items))
	for _, item := range req.Items {
		positions = append(positions, reading.Position{UserID: c.currentUserID(r), BookID: item.BookID, Page: item.Page, OccurredAt: item.UpdatedAt, FactID: item.FactID})
	}
	service := reading.New(c.store, func() { c.invalidateVolatileStatsCache("bulk_progress_sync") })
	synced := service.SyncPositions(r.Context(), positions)
	results := make([]BulkSyncProgressResultItem, 0, len(synced))
	updated := 0
	for _, item := range synced {
		if item.Status == "updated" {
			updated++
		}
		results = append(results, BulkSyncProgressResultItem{BookID: item.BookID, Status: item.Status, Page: item.Page, Message: item.Message})
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{"updated": updated, "results": results})
}

type UpsertReadingBookmarkRequest struct {
	Page int64  `json:"page"`
	Note string `json:"note"`
}

func (c *Controller) listReadingBookmarks(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	if _, err := c.store.GetBook(r.Context(), bookID); err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	// 书签必须按用户隔离（携带 user_id），否则多用户之间可互读、覆盖、删除对方的书签与私人笔记。
	items, err := c.store.ListReadingBookmarks(r.Context(), database.ListReadingBookmarksParams{
		UserID: c.currentUserID(r),
		BookID: bookID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to load reading bookmarks")
		return
	}
	if items == nil {
		items = []database.ReadingBookmark{}
	}
	jsonResponse(w, http.StatusOK, items)
}

func (c *Controller) upsertReadingBookmark(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	var req UpsertReadingBookmarkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	book, err := c.store.GetBook(r.Context(), bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}
	page := clampPageToBook(req.Page, book.PageCount)

	item, err := c.store.UpsertReadingBookmark(r.Context(), database.UpsertReadingBookmarkParams{
		UserID: c.currentUserID(r),
		BookID: bookID,
		Page:   page,
		Note:   strings.TrimSpace(req.Note),
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save reading bookmark")
		return
	}
	jsonResponse(w, http.StatusOK, item)
}

func (c *Controller) deleteReadingBookmark(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	bookmarkID, err := parseID(r, "bookmarkId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid bookmark ID")
		return
	}
	affected, err := c.store.DeleteReadingBookmark(r.Context(), database.DeleteReadingBookmarkParams{
		ID:     bookmarkID,
		UserID: c.currentUserID(r),
		BookID: bookID,
	})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to delete reading bookmark")
		return
	}
	if affected == 0 {
		jsonError(w, http.StatusNotFound, "Bookmark not found")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "Bookmark deleted"})
}
