// 业务说明：本文件属于后端 HTTP API 层，负责“自定义封面”——把书内某一页设为封面，或上传一张图片作为封面。
// 它复用扫描器的封面缩略图管线（Scanner.SetBookCoverFromPage / SetBookCoverFromImage），无条件覆盖 books.cover_path。
// 维护时应关注：上传体积/类型校验（首个 multipart 处理器）、内容寻址缓存、错误语义与派生系列封面的刷新。

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// maxCoverUploadBytes 限制上传封面体积，防止占用过多内存/磁盘。
const maxCoverUploadBytes = 16 << 20 // 16 MiB

// setBookCoverFromPage 把书内指定页(1-based)设为该书封面。
func (c *Controller) setBookCoverFromPage(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	var req struct {
		Page int `json:"page"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Page < 1 {
		jsonError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	book, err := c.store.GetBook(r.Context(), bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book not found")
		return
	}
	coverPath, err := c.scanner.SetBookCoverFromPage(r.Context(), book, req.Page)
	if err != nil {
		slog.Error("set book cover from page failed", "book_id", bookID, "page", req.Page, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to set cover")
		return
	}
	c.invalidateDashboardStatsCache("cover_set")
	jsonResponse(w, http.StatusOK, map[string]string{"cover_path": coverPath})
}

// uploadBookCover 用上传的图片作为书封面（首个 multipart 处理器）。
func (c *Controller) uploadBookCover(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseMultipartForm(maxCoverUploadBytes); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid upload")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Missing file")
		return
	}
	defer file.Close()
	if header.Size > maxCoverUploadBytes {
		jsonError(w, http.StatusRequestEntityTooLarge, apiText(requestLocale(r), "cover.upload.too_large"))
		return
	}
	data, err := io.ReadAll(io.LimitReader(file, maxCoverUploadBytes+1))
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read upload")
		return
	}
	if len(data) > maxCoverUploadBytes {
		jsonError(w, http.StatusRequestEntityTooLarge, apiText(requestLocale(r), "cover.upload.too_large"))
		return
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		jsonError(w, http.StatusUnsupportedMediaType, apiText(requestLocale(r), "cover.upload.not_image"))
		return
	}
	coverPath, err := c.scanner.SetBookCoverFromImage(r.Context(), book, data, mediaType)
	if err != nil {
		slog.Error("upload book cover failed", "book_id", bookID, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to set cover")
		return
	}
	c.invalidateDashboardStatsCache("cover_upload")
	jsonResponse(w, http.StatusOK, map[string]string{"cover_path": coverPath})
}
