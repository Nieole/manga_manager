package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"manga-manager/internal/images"
	"manga-manager/internal/parser"

	"github.com/go-chi/chi/v5"
)

func (c *Controller) servePageImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID := chi.URLParam(r, "bookId")
	pageNumberStr := chi.URLParam(r, "pageNumber")

	pageNumber, err := strconv.ParseInt(pageNumberStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid page number")
		return
	}

	pages, err := c.store.ListBookPages(ctx, bookID)
	if err != nil || len(pages) == 0 {
		jsonError(w, http.StatusNotFound, "Book not found or empty")
		return
	}

	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book entity not found")
		return
	}

	// Find requested page details
	var targetPage string
	var targetMediaType string
	for _, p := range pages {
		if p.Number == pageNumber {
			targetPage = p.FileName
			targetMediaType = p.MediaType
			break
		}
	}

	if targetPage == "" {
		jsonError(w, http.StatusNotFound, "Page not found")
		return
	}

	// 图片参数判断
	qualityStr := r.URL.Query().Get("q")
	format := r.URL.Query().Get("format") // 支持前端主动请求 webp/jpeg 降低带宽高负载
	widthStr := r.URL.Query().Get("w")
	heightStr := r.URL.Query().Get("h")

	// 构建缓存 Key：bookId-pageNumber-width-height-format-q
	cacheKey := fmt.Sprintf("%s-%d-%s-%s-%s-%s", bookID, pageNumber, widthStr, heightStr, format, qualityStr)

	// 如果是请求特定画幅或经过缩放的，则进行缓存查找以极度加速。原始图片则不查内存以防 OOM。
	isThumbnailReq := widthStr != "" || heightStr != ""
	if isThumbnailReq {
		if cachedData, ok := c.imageCache.Get(cacheKey); ok {
			w.Header().Set("Content-Type", "image/jpeg") // fallback to jpeg standard cache behavior for simplicity
			w.Header().Set("Content-Length", strconv.Itoa(len(cachedData)))
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			w.Write(cachedData)
			return
		}
	}

	archiver, err := parser.OpenArchive(book.Path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read internal archive")
		return
	}
	defer archiver.Close()

	data, err := archiver.ReadPage(targetPage)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read physical page data")
		return
	}

	// 准备处理并发送响应头
	opts := images.ProcessOptions{
		Format: format,
	}
	if q, err := strconv.Atoi(qualityStr); err == nil {
		opts.Quality = q
	}
	if w, err := strconv.Atoi(widthStr); err == nil {
		opts.Width = w
	}
	if h, err := strconv.Atoi(heightStr); err == nil {
		opts.Height = h
	}

	finalData, finalContentType, err := images.ProcessImage(data, targetMediaType, opts)
	if err != nil {
		// Log and fallback to raw data
		fmt.Printf("Image process error, fallback to raw source: %v", err)
		finalData = data
		finalContentType = targetMediaType
	}

	if isThumbnailReq {
		c.imageCache.Add(cacheKey, finalData)
	}

	w.Header().Set("Content-Type", finalContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(finalData)))

	// Cache control for performant client-side static assets
	// In production read this from config or context
	w.Header().Set("Cache-Control", "public, max-age=31536000")

	w.Write(finalData)
}

func (c *Controller) serveCoverImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID := chi.URLParam(r, "bookId")

	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book entity not found")
		return
	}

	if book.CoverPath.Valid && book.CoverPath.String != "" {
		thumbDir := filepath.Join(".", "data", "thumbnails")
		fullPath := filepath.Join(thumbDir, book.CoverPath.String)
		if _, err := os.Stat(fullPath); err == nil {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			http.ServeFile(w, r, fullPath)
			return
		}
	}

	jsonError(w, http.StatusNotFound, "Cover cache missing or invalid")
}
