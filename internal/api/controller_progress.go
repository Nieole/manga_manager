package api

import (
	"database/sql"
	"encoding/json"
	"errors"

	"net/http"
	"time"

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

type UpdateProgressRequest struct {
	Page      int64      `json:"page"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	FactID    string     `json:"fact_id,omitempty"`
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
	items, err := reading.NewBookmarks(c.store).List(r.Context(), c.currentUserID(r), bookID)
	if err != nil {
		if errors.Is(err, reading.ErrBookmarkBookUnavailable) {
			jsonError(w, http.StatusNotFound, "Book not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "Failed to load reading bookmarks")
		}
		return
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

	item, err := reading.NewBookmarks(c.store).Save(r.Context(), reading.BookmarkInput{
		UserID: c.currentUserID(r),
		BookID: bookID,
		Page:   req.Page,
		Note:   req.Note,
	})
	if err != nil {
		if errors.Is(err, reading.ErrBookmarkBookUnavailable) {
			jsonError(w, http.StatusNotFound, "Book not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "Failed to save reading bookmark")
		}
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
	err = reading.NewBookmarks(c.store).Delete(r.Context(), c.currentUserID(r), bookID, bookmarkID)
	if err != nil {
		if errors.Is(err, reading.ErrBookmarkNotFound) {
			jsonError(w, http.StatusNotFound, "Bookmark not found")
		} else {
			jsonError(w, http.StatusInternalServerError, "Failed to delete reading bookmark")
		}
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "Bookmark deleted"})
}
