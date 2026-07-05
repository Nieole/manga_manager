// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的阅读进度子域，负责上一本/下一本导航、单本与批量进度更新、KOReader 风格批量同步、阅读书签的增删查。

package api

import (
	"context"
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
	// UpdatedAt 可选：离线队列逐本回退时携带本地记录时间，服务端据此做「已有更新进度则跳过」的陈旧判定，
	// 与 bulk 同步端点口径一致，避免回退覆盖较新的跨设备进度。不带时按顺序覆盖（历史行为不变）。
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

const progressWriteThrottleWindow = 2 * time.Second

type cachedProgressWrite struct {
	userID    int64
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

	uid := c.currentUserID(r)

	if c.progressWriteCache != nil {
		if cached, ok := c.progressWriteCache.Get(bookID); ok && cached.userID == uid && cached.page == req.Page && time.Since(cached.updatedAt) < progressWriteThrottleWindow {
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

	// 取当前用户对本书的既有进度（uid==0 时退回全局 books 列，保持旧行为与既有测试）。
	previousPage, previousAt := c.bookProgressFor(ctx, uid, book)
	// 陈旧判定：客户端带了 updated_at 且早于服务端已记录时间，说明本地进度更旧，跳过（与 bulk 同步一致）。
	if req.UpdatedAt != nil && previousAt.Valid && req.UpdatedAt.Before(previousAt.Time) {
		jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress unchanged"})
		return
	}
	if previousAt.Valid && previousPage == validPage && time.Since(previousAt.Time) < progressWriteThrottleWindow {
		if c.progressWriteCache != nil {
			c.progressWriteCache.Add(bookID, cachedProgressWrite{userID: uid, page: validPage, updatedAt: time.Now()})
		}
		jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress unchanged"})
		return
	}

	now := time.Now()
	if uid > 0 {
		if err := c.store.SetUserBookProgress(ctx, uid, bookID, validPage, now); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to update progress")
			return
		}
	} else {
		if err := c.store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: validPage, Valid: true},
			LastReadAt:   sql.NullTime{Time: now, Valid: true},
			ID:           bookID,
		}); err != nil {
			jsonError(w, http.StatusInternalServerError, "Failed to update progress")
			return
		}
	}
	c.invalidateVolatileStatsCache("book_progress")
	if c.progressWriteCache != nil {
		c.progressWriteCache.Add(bookID, cachedProgressWrite{userID: uid, page: validPage, updatedAt: now})
	}

	// 阅读活动只记录前进页，避免 Webtoon 滚动和重复上报刷高活动写入。全局表 + 每用户表双写。
	if validPage > previousPage || previousPage == 0 {
		c.logReadingActivity(ctx, uid, bookID, validPage)
	}

	jsonResponse(w, http.StatusOK, map[string]string{"status": "Progress updated"})
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
	if err := c.store.LogReadingActivity(ctx, database.LogReadingActivityParams{BookID: bookID, PagesRead: pages}); err != nil {
		slog.Error("Failed to log reading activity", "book_id", bookID, "error", err)
	}
	if uid > 0 {
		if err := c.store.LogUserReadingActivity(ctx, uid, bookID, pages); err != nil {
			slog.Error("Failed to log per-user reading activity", "user_id", uid, "book_id", bookID, "error", err)
		}
	}
}

// overlayUserProgress 用当前用户的每用户进度覆盖一组书的 LastReadPage/LastReadAt。
// uid==0（旧全局路径 / 首启 / 单元测试）时不改动，保留 books 自带的全局列；uid>0 时按用户隔离：
// 有每用户记录则覆盖，无记录则清零，避免看到他人或全局遗留的进度。
func (c *Controller) overlayUserProgress(ctx context.Context, uid int64, books []database.Book) {
	if uid == 0 || len(books) == 0 {
		return
	}
	ids := make([]int64, len(books))
	for i := range books {
		ids[i] = books[i].ID
	}
	m, err := c.store.GetUserBookProgressMap(ctx, uid, ids)
	if err != nil {
		slog.Error("overlay user progress failed", "user_id", uid, "error", err)
		return
	}
	for i := range books {
		if p, ok := m[books[i].ID]; ok {
			books[i].LastReadPage = p.LastReadPage
			books[i].LastReadAt = p.LastReadAt
		} else {
			books[i].LastReadPage = sql.NullInt64{}
			books[i].LastReadAt = sql.NullTime{}
		}
	}
}

// overlayUserProgressOne 覆盖单本书的每用户进度（语义同 overlayUserProgress）。
func (c *Controller) overlayUserProgressOne(ctx context.Context, uid int64, book *database.Book) {
	if uid == 0 || book == nil {
		return
	}
	p, found, err := c.store.GetUserBookProgress(ctx, uid, book.ID)
	if err != nil {
		slog.Error("overlay user progress failed", "user_id", uid, "book_id", book.ID, "error", err)
		return
	}
	if found {
		book.LastReadPage = p.LastReadPage
		book.LastReadAt = p.LastReadAt
	} else {
		book.LastReadPage = sql.NullInt64{}
		book.LastReadAt = sql.NullTime{}
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

		validPage := item.Page
		if validPage <= 0 {
			validPage = 1
		}
		if book.PageCount > 0 && validPage > book.PageCount {
			validPage = book.PageCount
		}

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
