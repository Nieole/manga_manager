// 业务说明：本文件是业务实现，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它承载资料库浏览、阅读器取页、系列维护、任务进度、系统设置和静态资源缓存等对外业务契约。
// 维护时应重点关注请求参数校验、错误语义、缓存头、并发任务状态和前后端字段兼容性。

package api

import (
	"crypto/sha1"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/images"
	"manga-manager/internal/parser"
	"manga-manager/internal/storageio"

	"github.com/go-chi/chi/v5"
)

type pageCacheStatsResponse struct {
	Path      string `json:"path"`
	FileSize  int64  `json:"file_size"`
	FileCount int64  `json:"file_count"`
}

// maxCacheableImageBytes 是单张图片进入内存 LRU 的大小上限。AI 放大（waifu2x/realcugan）后的
// 整页可达数 MB，而 imageCache 只按条数（256）限制；无大小上限时可能被大图占满、内存膨胀到 GB 级。
// 超过上限的图不进内存缓存，按需重算即可。
const maxCacheableImageBytes = 4 << 20 // 4 MiB

// cacheImageMemory 仅在图片不超过 maxCacheableImageBytes 时写入内存缓存。
func (c *Controller) cacheImageMemory(key string, data []byte) {
	if len(data) <= maxCacheableImageBytes {
		c.imageCache.Add(key, data)
	}
}

func (c *Controller) servePageImage(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}
	pageNumberStr := chi.URLParam(r, "pageNumber")

	pageNumber, err := strconv.ParseInt(pageNumberStr, 10, 64)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid page number")
		return
	}
	c.servePageImageByNumber(w, r, bookID, pageNumber)
}

func (c *Controller) servePageImageByNumber(w http.ResponseWriter, r *http.Request, bookID, pageNumber int64) {
	started := time.Now()
	ctx := r.Context()

	source, err := c.getBookPageSource(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book entity not found")
		return
	}

	// 读取前端声明的图像变体参数：这些参数会改变最终字节内容，因此必须全部进入缓存键和 ETag。
	qualityStr := r.URL.Query().Get("q")
	format := r.URL.Query().Get("format") // 支持前端主动请求 webp/jpeg 降低带宽高负载
	widthStr := r.URL.Query().Get("w")
	heightStr := r.URL.Query().Get("h")
	filter := normalizeServerImageFilter(r.URL.Query().Get("filter"))
	autoCrop := r.URL.Query().Get("auto_crop") == "true"

	// 构建缓存 Key：包含所有会改变最终图像字节的处理参数，防止切换滤镜、画质、放大参数后复用旧图。
	// 同时引入文件修改时间和大小，避免归档被覆盖或 ID 复用时浏览器继续命中旧 ETag。
	w2xScaleStr := r.URL.Query().Get("w2x_scale")
	w2xNoiseStr := r.URL.Query().Get("w2x_noise")
	w2xFormatStr := r.URL.Query().Get("w2x_format")
	transform := pageImageTransformProfile(format, widthStr, heightStr, filter, autoCrop, w2xScaleStr, w2xNoiseStr, w2xFormatStr)
	cacheKey := fmt.Sprintf("%d-%d-%d-%d-%s-%s-%s-%s-%s-%s-%s-%s-%t",
		bookID, pageNumber, source.FileModifiedAt.UnixNano(), source.Size,
		widthStr, heightStr, format, qualityStr, filter, w2xScaleStr, w2xNoiseStr, w2xFormatStr, autoCrop)
	// 图片资源不依赖 Origin，清除 CORS 中间件写入的 Vary: Origin，否则浏览器无法命中缓存。
	w.Header().Del("Vary")
	etag := weakETag(cacheKey)
	if r.Header.Get("If-None-Match") == etag {
		annotatePageImageRequest(ctx, bookID, pageNumber, true, "client", transform)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// 只有派生图像才进入内存/磁盘缓存；原始页图可能很大，直接缓存会挤占阅读器长会话内存。
	isThumbnailReq := widthStr != "" || heightStr != "" || format != "" || qualityStr != "" || (filter != "" && filter != "nearest" && filter != "average" && filter != "bilinear") || autoCrop
	rawPassthrough := !isThumbnailReq && w2xScaleStr == "" && w2xNoiseStr == "" && w2xFormatStr == ""
	diskPageCacheEnabled := c.diskPageCacheEnabled(source)
	if isThumbnailReq {
		if cachedData, ok := c.imageCache.Get(cacheKey); ok {
			contentType := detectImageContentType(cachedData) // AVIF 感知，避免命中缓存时退化为 octet-stream
			annotatePageImageDiagnostics(ctx, false, false, false, true)
			annotatePageImageRequest(ctx, bookID, pageNumber, true, "memory", transform)
			c.logPageImageServed(bookID, pageNumber, "memory", contentType, len(cachedData), time.Since(started), format, filter, autoCrop)
			w.Header().Set("Content-Type", contentType) // 缓存命中时也以实际字节探测类型为准，避免前端按错误格式解码。
			w.Header().Set("Content-Length", strconv.Itoa(len(cachedData)))
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			w.Header().Set("ETag", etag)
			w.Write(cachedData)
			return
		}
		if diskPageCacheEnabled {
			if cachedData, cachedContentType, ok := c.readDiskImageCache(cacheKey); ok {
				c.cacheImageMemory(cacheKey, cachedData)
				annotatePageImageDiagnostics(ctx, false, false, false, true)
				annotatePageImageRequest(ctx, bookID, pageNumber, true, "disk", transform)
				c.logPageImageServed(bookID, pageNumber, "disk", cachedContentType, len(cachedData), time.Since(started), format, filter, autoCrop)
				w.Header().Set("Content-Type", cachedContentType)
				w.Header().Set("Content-Length", strconv.Itoa(len(cachedData)))
				w.Header().Set("Cache-Control", "public, max-age=31536000")
				w.Header().Set("ETag", etag)
				w.Write(cachedData)
				return
			}
		}
	}

	storagePolicy := config.ResolveStoragePolicy(c.currentConfig(), source.Path)
	readerLease, err := storageio.Default.Acquire(ctx, storageio.Request{
		VolumeKey:        storagePolicy.VolumeKey,
		Limit:            storagePolicy.IOPolicy.ArchiveOpenConcurrency,
		Kind:             storageio.WorkKindReader,
		PauseWhenReading: storagePolicy.IOPolicy.PauseBackgroundWhenReading,
	})
	if err != nil {
		jsonError(w, http.StatusServiceUnavailable, "Storage is busy")
		return
	}
	annotatePageImageStorage(ctx, storagePolicy.StorageProfile, storagePolicy.VolumeKey, readerLease.Wait)
	defer readerLease.Release()

	pageInfo, manifestCacheHit, err := c.getBookArchiveSourcePageWithStats(ctx, source, pageNumber)
	annotatePageImageDiagnostics(ctx, false, manifestCacheHit, false, false)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			jsonError(w, http.StatusNotFound, "Page not found")
			return
		}
		if pageNumber > source.PageCount && source.PageCount > 0 {
			jsonError(w, http.StatusNotFound, "Page not found")
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to read pages")
		return
	}

	targetPage := pageInfo.Name
	targetMediaType := pageInfo.MediaType

	archiver, err := parser.GetArchiveFromPool(source.Path)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read internal archive")
		return
	}
	annotatePageImageDiagnostics(ctx, true, false, false, false)

	data, err := archiver.ReadPage(targetPage)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read physical page data")
		return
	}
	readerLease.Release()

	// 准备处理并发送响应头
	opts := images.ProcessOptions{
		Format:        format,
		Filter:        filter,
		AutoCrop:      autoCrop,
		Waifu2xPath:   c.currentConfig().Scanner.Waifu2xPath,
		RealCuganPath: c.currentConfig().Scanner.RealCuganPath,
		Waifu2xScale:  2,      // 缺省使用引擎默认2倍
		Waifu2xNoise:  0,      // 缺省使用引擎默认0阶降噪
		Waifu2xFormat: "webp", // 控制引擎默认采用 webp 挤压体积
	}

	// 读取前端传入的 Waifu2x 参数；这些值已经进入缓存键，处理结果可以被安全复用。
	if w2xScaleStr != "" {
		if v, err := strconv.Atoi(w2xScaleStr); err == nil {
			opts.Waifu2xScale = v
		}
	}
	if w2xNoiseStr != "" {
		if v, err := strconv.Atoi(w2xNoiseStr); err == nil {
			opts.Waifu2xNoise = v
		}
	}
	if w2xFormatStr != "" {
		opts.Waifu2xFormat = w2xFormatStr
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
	processOK := err == nil
	if err != nil {
		// Log and fallback to raw data
		slog.Warn("Image process error, fallback to raw source", "error", err)
		finalData = data
		finalContentType = targetMediaType
	}

	// 仅在处理成功时写入处理缓存键：否则会把原始回退结果当作已处理产物持久缓存，
	// 让后续请求（含临时错误恢复后）永远拿到未处理的图，形成缓存污染。
	if isThumbnailReq && processOK {
		c.cacheImageMemory(cacheKey, finalData)
		if diskPageCacheEnabled {
			if err := c.writeDiskImageCache(cacheKey, finalData, finalContentType); err != nil {
				slog.Warn("Failed to write processed page disk cache", "error", err)
			}
		}
	}

	w.Header().Set("Content-Type", finalContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(finalData)))

	// Cache control for performant client-side static assets
	// In production read this from config or context
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("ETag", etag)

	cacheSource := "raw"
	if isThumbnailReq {
		cacheSource = "processed"
	}
	annotatePageImageDiagnostics(ctx, false, false, rawPassthrough, isThumbnailReq)
	annotatePageImageRequest(ctx, bookID, pageNumber, false, cacheSource, transform)
	c.logPageImageServed(bookID, pageNumber, cacheSource, finalContentType, len(finalData), time.Since(started), format, filter, autoCrop)
	w.Write(finalData)
}

func (c *Controller) logPageImageServed(bookID, pageNumber int64, source, contentType string, size int, duration time.Duration, format, filter string, autoCrop bool) {
	if duration < 250*time.Millisecond && source != "processed" {
		return
	}
	slog.Info("Served page image",
		"book_id", bookID,
		"page", pageNumber,
		"source", source,
		"content_type", contentType,
		"bytes", size,
		"duration_ms", duration.Milliseconds(),
		"format", format,
		"filter", filter,
		"auto_crop", autoCrop,
	)
}

func weakETag(value string) string {
	return `W/"` + fmt.Sprintf("%x", sha1.Sum([]byte(value))) + `"`
}

func normalizeServerImageFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "", "none", "nearest", "average", "bilinear":
		return ""
	default:
		return filter
	}
}

func pageImageTransformProfile(format, width, height, filter string, autoCrop bool, w2xScale, w2xNoise, w2xFormat string) string {
	parts := make([]string, 0, 6)
	if format != "" {
		parts = append(parts, "format:"+format)
	}
	if width != "" || height != "" {
		parts = append(parts, "resize:"+width+"x"+height)
	}
	if filter != "" {
		parts = append(parts, "filter:"+filter)
	}
	if autoCrop {
		parts = append(parts, "auto_crop")
	}
	if w2xScale != "" || w2xNoise != "" || w2xFormat != "" {
		parts = append(parts, "ai:"+w2xScale+":"+w2xNoise+":"+w2xFormat)
	}
	if len(parts) == 0 {
		return "raw"
	}
	return strings.Join(parts, "|")
}

func (c *Controller) processedImageCacheDir() string {
	baseDir := filepath.Join(".", "data", "page-cache")
	cfg := c.currentConfig()
	if cfg.Cache.Dir != "" {
		baseDir = filepath.Join(cfg.Cache.Dir, "pages")
	}
	return baseDir
}

func (c *Controller) diskPageCacheEnabled(source bookPageSource) bool {
	cfg := c.currentConfig()
	if !cfg.Cache.PageDiskCacheEnabled {
		return false
	}
	policy := config.ResolveStoragePolicy(cfg, source.Path)
	if policy.IOPolicy.DisableSameDiskPageCache && config.SameVolume(cfg.Cache.Dir, source.Path) {
		return false
	}
	return true
}

func processedImageCacheFileName(cacheKey, contentType string) string {
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(cacheKey)))
	return sum + extensionFromContentType(contentType)
}

func extensionFromContentType(contentType string) string {
	switch {
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "avif"):
		return ".avif"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	default:
		return ".bin"
	}
}

// contentTypeFromExtension 是 extensionFromContentType 的逆：由缓存文件扩展名精确复原写入时的 MIME。
func contentTypeFromExtension(ext string) string {
	switch ext {
	case ".webp":
		return "image/webp"
	case ".png":
		return "image/png"
	case ".avif":
		return "image/avif"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return ""
	}
}

// detectImageContentType 探测图片字节 MIME。标准库 http.DetectContentType 无 AVIF 签名，会把
// AVIF 误判为 application/octet-stream；此处补一条 AVIF ftyp 探测，保证缓存命中与首次响应一致。
func detectImageContentType(data []byte) string {
	ct := http.DetectContentType(data)
	if ct == "application/octet-stream" && len(data) >= 12 && string(data[4:8]) == "ftyp" {
		switch string(data[8:12]) {
		case "avif", "avis":
			return "image/avif"
		}
	}
	return ct
}

func (c *Controller) readDiskImageCache(cacheKey string) ([]byte, string, bool) {
	sum := fmt.Sprintf("%x", sha1.Sum([]byte(cacheKey)))
	dir := c.processedImageCacheDir()
	for _, ext := range []string{".webp", ".png", ".avif", ".jpg", ".bin"} {
		path := filepath.Join(dir, sum[:2], sum+ext)
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			// 优先按扩展名精确复原 MIME（写入时扩展名即由权威 finalContentType 推导），
			// 仅 .bin 未知格式才回退字节探测。
			ct := contentTypeFromExtension(ext)
			if ct == "" {
				ct = detectImageContentType(data)
			}
			return data, ct, true
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
			slog.Warn("Failed to read processed page disk cache", "path", path, "error", err)
		}
	}
	return nil, "", false
}

func (c *Controller) writeDiskImageCache(cacheKey string, data []byte, contentType string) error {
	if len(data) == 0 {
		return nil
	}
	fileName := processedImageCacheFileName(cacheKey, contentType)
	subDir := fileName[:2]
	dir := filepath.Join(c.processedImageCacheDir(), subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, fileName), data, 0o644)
}

func (c *Controller) getPageCacheStats(w http.ResponseWriter, r *http.Request) {
	stats, err := c.collectPageCacheStats()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to inspect page cache")
		return
	}
	jsonResponse(w, http.StatusOK, stats)
}

func (c *Controller) clearPageCache(w http.ResponseWriter, r *http.Request) {
	dir := filepath.Clean(c.processedImageCacheDir())
	if dir == "." || dir == string(filepath.Separator) || strings.TrimSpace(dir) == "" {
		jsonError(w, http.StatusInternalServerError, "Invalid page cache directory")
		return
	}

	before, err := c.collectPageCacheStats()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to inspect page cache")
		return
	}
	if err := removeDirectoryContents(dir); err != nil {
		slog.Warn("Failed to clear processed page cache", "path", dir, "error", err)
		jsonError(w, http.StatusInternalServerError, "Failed to clear page cache")
		return
	}
	c.imageCache.Purge()

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":       "Page cache cleared",
		"path":          before.Path,
		"cleared_files": before.FileCount,
		"cleared_bytes": before.FileSize,
	})
}

func (c *Controller) collectPageCacheStats() (pageCacheStatsResponse, error) {
	dir := filepath.Clean(c.processedImageCacheDir())
	stats := pageCacheStatsResponse{Path: dir}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stats.FileCount++
		stats.FileSize += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return stats, nil
	}
	return stats, err
}

func removeDirectoryContents(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}
	return nil
}

func (c *Controller) serveCoverImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	// 只取 cover_path 一列（ETag/304 仅依赖它），避免每次封面请求都 Scan 整行 books（20+ 无用列）。
	coverPath, err := c.store.GetBookCoverPath(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book entity not found")
		return
	}

	if coverPath != "" {
		thumbDir := filepath.Join(".", "data", "thumbnails")
		cfg := c.currentConfig()
		if cfg.Cache.Dir != "" {
			thumbDir = cfg.Cache.Dir
		}

		fullPath := filepath.Join(thumbDir, coverPath)
		if info, err := os.Stat(fullPath); err == nil {
			// 基于封面路径 + 文件修改时间 + 大小生成弱 ETag：缩略图重建覆盖文件时 ETag 必变、
			// 不会复读旧封面；内容不变则客户端可凭 If-None-Match 命中 304，省去整图重传。
			// http.ServeFile 仍会提供 Last-Modified 作为兜底条件请求。
			etag := weakETag(fmt.Sprintf("cover-%s-%d-%d", coverPath, info.ModTime().UnixNano(), info.Size()))
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			w.Header().Del("Vary")
			w.Header().Set("ETag", etag)
			if r.Header.Get("If-None-Match") == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			http.ServeFile(w, r, fullPath)
			return
		}
	}

	jsonError(w, http.StatusNotFound, "Cover cache missing or invalid")
}
