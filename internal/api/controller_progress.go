package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
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

type cachedProgressWrite struct {
	userID    int64
	page      int64
	updatedAt time.Time
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

// maxReadingTimeReportSeconds 单次上报封顶（防异常/伪造把时长刷爆）。心跳间隔约 30-60s，正常远小于此。
const maxReadingTimeReportSeconds = 3600

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
	if uid == 0 || req.Seconds <= 0 {
		jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if req.Seconds > maxReadingTimeReportSeconds {
		req.Seconds = maxReadingTimeReportSeconds
	}
	// 书可能在阅读期间被删除（管理员删/重扫）——此时 FK 会让写入失败。存在性缺失即静默接受（尽力而为的统计，
	// 且前端为 sendBeacon/心跳的 fire-and-forget），避免每次心跳刷 500 与错误日志。
	if _, err := c.store.GetBook(r.Context(), bookID); err != nil {
		jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if err := c.store.AddUserBookReadingTime(r.Context(), uid, bookID, req.Seconds); err != nil {
		slog.Error("Failed to record reading time", "user_id", uid, "book_id", bookID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to record reading time")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"status": "ok"})
}

// logReadingActivity 记录当日阅读活动：全局表始终写（向后兼容 / uid==0），uid>0 时同时写每用户表（供热力图/连续天数/回顾）。
func (c *Controller) logReadingActivity(ctx context.Context, uid, bookID, pages int64) {
	if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: pages, Date: database.ActivityDayKey(time.Now())}); err != nil {
		slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
	}
	if uid > 0 {
		if err := c.store.LogUserReadingActivity(ctx, uid, bookID, pages); err != nil {
			slog.Error("Failed to log per-user reading activity", "user_id", uid, "book_id", bookID, "error", err)
		}
	}
}

// bookProgressFor 返回某用户对某书的既有进度页与时间戳；uid==0 时退回该书的全局 books 列（旧行为）。
func (c *Controller) bookProgressFor(ctx context.Context, uid int64, book database.Book) (int64, sql.NullTime) {
	if uid > 0 {
		if p, found, err := c.store.GetUserBookProgress(ctx, uid, book.ID); err == nil && found {
			return p.LastReadPage.Int64, p.LastReadAt
		}
		return 0, sql.NullTime{}
	}
	if book.LastReadPage.Valid {
		return book.LastReadPage.Int64, book.LastReadAt
	}
	return 0, book.LastReadAt
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

	uid := c.currentUserID(r)
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

		validPage := clampPageToBook(item.Page, book.PageCount)

		// updated_at 冲突解决：若客户端时间戳 < 数据库 last_read_at，认为本地数据已陈旧，跳过。
		// 没有 updated_at 时按顺序覆盖（与单本 updateBookProgress 行为一致）。取当前用户的既有进度做冲突解决。
		previousPage, previousAt := c.bookProgressFor(ctx, uid, book)
		if item.UpdatedAt != nil && previousAt.Valid && item.UpdatedAt.Before(previousAt.Time) {
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "skipped_stale",
				Page:    previousPage,
				Message: "server has newer progress",
			})
			continue
		}

		// 与单本端点对齐的相同页节流。
		if previousAt.Valid && previousPage == validPage {
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
		var writeErr error
		if uid > 0 {
			writeErr = c.store.SetUserBookProgress(ctx, uid, item.BookID, validPage, readAt)
		} else {
			writeErr = c.store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
				LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
				LastReadAt:   sql.NullTime{Time: readAt, Valid: true},
				ID:           item.BookID,
			})
		}
		if writeErr != nil {
			slog.Error("bulk sync progress update failed", "book_id", item.BookID, "error", writeErr)
			results = append(results, BulkSyncProgressResultItem{
				BookID:  item.BookID,
				Status:  "invalid",
				Message: "update failed",
			})
			continue
		}
		updatedCount++

		if c.progressWriteCache != nil {
			c.progressWriteCache.Add(item.BookID, cachedProgressWrite{userID: uid, page: validPage, updatedAt: time.Now()})
		}

		// 仅前进的页码记录活动，与单本接口策略一致。全局 + 每用户双写。
		if validPage > previousPage || previousPage == 0 {
			c.logReadingActivity(ctx, uid, item.BookID, validPage)
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
