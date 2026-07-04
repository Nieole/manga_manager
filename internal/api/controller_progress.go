// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的阅读进度子域，负责上一本/下一本导航、单本与批量进度更新、KOReader 风格批量同步、阅读书签的增删查。

package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"manga-manager/internal/booksort"
	"manga-manager/internal/database"
	"net/http"
	"sort"
	"strings"
	"time"
)

func (c *Controller) getNextBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	currentBook, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No next book")
		return
	}
	books, err := c.store.ListBooksBySeries(ctx, currentBook.SeriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No next book")
		return
	}
	sortBooksForReading(books)
	for i := range books {
		if books[i].ID == currentBook.ID && i+1 < len(books) {
			jsonResponse(w, http.StatusOK, books[i+1])
			return
		}
	}

	jsonError(w, http.StatusNotFound, "No next book")
}

func (c *Controller) getPrevBook(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	currentBook, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No previous book")
		return
	}
	books, err := c.store.ListBooksBySeries(ctx, currentBook.SeriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "No previous book")
		return
	}
	sortBooksForReading(books)
	for i := range books {
		if books[i].ID == currentBook.ID && i > 0 {
			jsonResponse(w, http.StatusOK, books[i-1])
			return
		}
	}

	jsonError(w, http.StatusNotFound, "No previous book")
}

func sortBooksForReading(books []database.Book) {
	sort.SliceStable(books, func(i, j int) bool {
		return booksort.CompareBooks(books[i], books[j]) < 0
	})
}

type UpdateProgressRequest struct {
	Page int64 `json:"page"`
}

const progressWriteThrottleWindow = 2 * time.Second

type cachedProgressWrite struct {
	page      int64
	updatedAt time.Time
}

func (c *Controller) updateBookProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	var req UpdateProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if req.Page <= 0 {
		req.Page = 1
	}

	if c.progressWriteCache != nil {
		if cached, ok := c.progressWriteCache.Get(bookID); ok && cached.page == req.Page && time.Since(cached.updatedAt) < progressWriteThrottleWindow {
			jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress unchanged"})
			return
		}
	}

	// 校验页码合法性
	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}

	validPage := req.Page
	if validPage > book.PageCount {
		validPage = book.PageCount
	}
	if validPage < 1 {
		validPage = 1
	}

	previousPage := int64(0)
	if book.LastReadPage.Valid {
		previousPage = book.LastReadPage.Int64
	}
	if book.LastReadPage.Valid && previousPage == validPage && book.LastReadAt.Valid && time.Since(book.LastReadAt.Time) < progressWriteThrottleWindow {
		if c.progressWriteCache != nil {
			c.progressWriteCache.Add(bookID, cachedProgressWrite{page: validPage, updatedAt: time.Now()})
		}
		jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress unchanged"})
		return
	}

	params := database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           bookID,
	}

	if err := c.store.UpdateBookProgress(ctx, params); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to update progress")
		return
	}
	c.invalidateVolatileStatsCache("book_progress")
	if c.progressWriteCache != nil {
		c.progressWriteCache.Add(bookID, cachedProgressWrite{page: validPage, updatedAt: time.Now()})
	}

	// 阅读活动只记录前进页，避免 Webtoon 滚动和重复上报刷高活动写入。
	if validPage > previousPage {
		if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: validPage}); err != nil {
			slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
		}
	} else if !book.LastReadPage.Valid {
		if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: validPage}); err != nil {
			slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
		}
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress updated"})
}

type BulkSyncProgressItem struct {
	BookID    int64      `json:"book_id"`
	Page      int64      `json:"page"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
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

// bulkSyncBookProgress 接受多本书的离线进度并按 updated_at 解决冲突。
// 离线 / 在线恢复时 useReaderOffline 调用，避免逐条 POST 的峰值写入。
func (c *Controller) bulkSyncBookProgress(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req BulkSyncProgressRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	if len(req.Items) == 0 {
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"updated": 0,
			"results": []BulkSyncProgressResultItem{},
		})
		return
	}

	results := make([]BulkSyncProgressResultItem, 0, len(req.Items))
	updatedCount := 0

	for _, item := range req.Items {
		if item.BookID <= 0 {
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "invalid",
				Message: "book_id is required",
			})
			continue
		}

		book, err := c.store.GetBook(ctx, item.BookID)
		if err != nil {
			results = append(results, BulkSyncProgressResultItem{
				BookID: item.BookID,
				Status: "not_found",
			})
			continue
		}

		validPage := item.Page
		if validPage <= 0 {
			validPage = 1
		}
		if book.PageCount > 0 && validPage > book.PageCount {
			validPage = book.PageCount
		}

		// updated_at 冲突解决：若客户端时间戳 < 数据库 last_read_at，认为本地数据已陈旧，跳过。
		// 没有 updated_at 时按顺序覆盖（与单本 updateBookProgress 行为一致）。
		if item.UpdatedAt != nil && book.LastReadAt.Valid && item.UpdatedAt.Before(book.LastReadAt.Time) {
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "skipped_stale",
				Page:    book.LastReadPage.Int64,
				Message: "server has newer progress",
			})
			continue
		}

		// 与单本端点对齐的相同页节流。
		previousPage := int64(0)
		if book.LastReadPage.Valid {
			previousPage = book.LastReadPage.Int64
		}
		if book.LastReadPage.Valid && previousPage == validPage {
			results = append(results, BulkSyncProgressResultItem{
				BookID: item.BookID,
				Status: "skipped_unchanged",
				Page:   validPage,
			})
			continue
		}

		readAt := time.Now()
		if item.UpdatedAt != nil {
			readAt = *item.UpdatedAt
		}
		params := database.UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
			LastReadAt:   sql.NullTime{Time: readAt, Valid: true},
			ID:           item.BookID,
		}
		if err := c.store.UpdateBookProgress(ctx, params); err != nil {
			slog.Error("bulk sync progress update failed", "book_id", item.BookID, "error", err)
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "invalid",
				Message: "update failed",
			})
			continue
		}
		updatedCount++

		if c.progressWriteCache != nil {
			c.progressWriteCache.Add(item.BookID, cachedProgressWrite{page: validPage, updatedAt: time.Now()})
		}

		// 仅前进的页码记录活动，与单本接口策略一致。
		if validPage > previousPage {
			if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: item.BookID, PagesRead: validPage}); err != nil {
				slog.Error("Failed to log reading activity", "book_id", item.BookID, "error", err)
			}
		} else if !book.LastReadPage.Valid {
			if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: item.BookID, PagesRead: validPage}); err != nil {
				slog.Error("Failed to log reading activity", "book_id", item.BookID, "error", err)
			}
		}

		results = append(results, BulkSyncProgressResultItem{
			BookID: item.BookID,
			Status: "updated",
			Page:   validPage,
		})
	}

	if updatedCount > 0 {
		c.invalidateVolatileStatsCache("bulk_progress_sync")
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"updated": updatedCount,
		"results": results,
	})
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

	items, err := c.store.ListReadingBookmarks(r.Context(), bookID)
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
	page := req.Page
	if page < 1 {
		page = 1
	}
	if book.PageCount > 0 && page > book.PageCount {
		page = book.PageCount
	}

	item, err := c.store.UpsertReadingBookmark(r.Context(), database.UpsertReadingBookmarkParams{
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
