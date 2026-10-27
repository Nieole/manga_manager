package api

import (
	"container/heap"
	"crypto/sha1"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"manga-manager/internal/bookcontent"
	"manga-manager/internal/config"
)

type pageCacheStatsResponse struct {
	Path      string `json:"path"`
	FileSize  int64  `json:"file_size"`
	FileCount int64  `json:"file_count"`
}

// pageImageCacheControl 是所有需要鉴权的图片端点（页图/封面/缩略图）的缓存策略。
//
// 必须是 private + 必须回源验证，不能放宽成 public 长 max-age：public 允许共享缓存
// （企业代理、CDN）替别的用户直接命中本该鉴权的页；这些 URL 又没有内容版本令牌，长
// max-age 会让换封面、重建缩略图后旧图继续顶很久。ETag 仍在，条件请求命中时返回 304，
// 省下的带宽与长 max-age 相同。
const pageImageCacheControl = "private, max-age=0, must-revalidate"

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
	release, ok := c.guardBookRead(w, r, bookID)
	if !ok {
		return
	}
	defer release()
	width, wErr := parsePositiveDimension(r.URL.Query().Get("w"))
	height, hErr := parsePositiveDimension(r.URL.Query().Get("h"))
	if wErr != nil || hErr != nil {
		jsonError(w, http.StatusBadRequest, "Invalid image dimension parameter")
		return
	}
	query := r.URL.Query()
	options := bookcontent.PageOptions{
		Width: width, Height: height, Quality: query.Get("q"), Format: query.Get("format"), Filter: query.Get("filter"),
		AutoCrop: query.Get("auto_crop") == "true", FitInside: query.Get("fit") == "inside",
		Waifu2xScale: query.Get("w2x_scale"), Waifu2xNoise: query.Get("w2x_noise"), Waifu2xFormat: query.Get("w2x_format"),
	}
	conditional := r.Header.Get("If-None-Match")
	if strings.HasPrefix(conditional, `W/"`) && strings.HasSuffix(conditional, `"`) {
		options.KnownVersion = strings.TrimSuffix(strings.TrimPrefix(conditional, `W/"`), `"`)
	}
	result, err := c.content.Page(r.Context(), bookID, pageNumber, options)
	if err != nil {
		switch {
		case errors.Is(err, bookcontent.ErrSourceMissing):
			jsonError(w, http.StatusNotFound, "Book entity not found")
		case errors.Is(err, bookcontent.ErrInvalidDimensions):
			jsonError(w, http.StatusBadRequest, "Requested image size exceeds limits")
		case errors.Is(err, bookcontent.ErrPageNotFound):
			jsonError(w, http.StatusNotFound, "Page not found")
		case errors.Is(err, bookcontent.ErrBusy):
			jsonError(w, http.StatusServiceUnavailable, "Storage is busy")
		default:
			writeStorageFailure(w, c.diagnoseStorageFailure(r.Context(), storageTargetFromSource(result.Source), err), "page_image")
		}
		return
	}
	cacheHit := result.CacheSource == "memory" || result.CacheSource == "disk" || result.CacheSource == "client"
	annotatePageImageRequest(r.Context(), bookID, pageNumber, cacheHit, result.CacheSource, result.Transform)
	if result.Data != nil {
		annotatePageImageDiagnostics(r.Context(), result.ArchiveOpen, result.ManifestCacheHit, !result.Derived, result.Derived)
	}
	annotatePageImageStorage(r.Context(), result.StorageProfile, result.VolumeKey, result.IOWait)
	w.Header().Del("Vary")
	w.Header().Set("ETag", `W/"`+result.Version+`"`)
	w.Header().Set("Cache-Control", pageImageCacheControl)
	if result.Fallback != "" {
		w.Header().Set("X-Image-Fallback", result.Fallback)
	}
	if result.NotModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(result.Data)))
	c.logPageImageServed(bookID, pageNumber, result.CacheSource, result.ContentType, len(result.Data), time.Since(started), options.Format, options.Filter, options.AutoCrop)
	_, _ = w.Write(result.Data)
}

func (c *Controller) processedImageCacheDir() string { return config.PageCacheDir(c.currentConfig()) }
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

// parsePositiveDimension 解析 w/h 这类尺寸查询参数。
// 空串表示「未指定」，返回 0；非数字或负数视为非法输入，由调用方回 400——负值不能只当
// 「没传」处理：转 uint 后会回绕成天文数字（如 uint(-1) = 18446744073709551615），
// 直接把 resize 的目标画布撑爆。
func parsePositiveDimension(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", raw)
	}
	if v < 0 {
		return 0, fmt.Errorf("negative dimension: %d", v)
	}
	return v, nil
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

// enforcePageCacheBudget 在磁盘页缓存超过配置上限时，按最旧优先淘汰到低水位。
// 由单个后台清道夫 goroutine 串行调用，无锁竞争、零请求路径开销。上限 <=0 表示不限，直接返回。
//
// 分两趟走，而不是一趟把所有条目都装进切片：稳态下缓存**不超标**（这是常态），第一趟
// 只累加总大小、不留任何条目，直接早退——2 GiB 上限、每页几十到几百 KB 就是几千到几万
// 个条目，不能无条件先把完整列表建出来常驻内存。
//
// 超标时第二趟才收集淘汰候选，且只保留「最旧的那一批」而不是全部：
// 用一个按 mtime 的大顶堆，堆内累计大小一旦够删就把最新的挤出去。
// 于是常驻量与「需要删多少」成正比，与缓存里总共有多少文件无关。
//
// 淘汰用 mtime 而不是 atime：绝大多数生产文件系统挂载时带 noatime（或 relatime），
// atime 根本不更新，拿它做 LRU 会得到一个随机顺序。mtime 至少是「写入时间」这个确定语义。
func (c *Controller) enforcePageCacheBudget() {
	cfg := c.currentConfig()
	if !cfg.Cache.PageDiskCacheEnabled || cfg.Cache.PageDiskCacheMaxBytes <= 0 {
		return
	}
	maxBytes := cfg.Cache.PageDiskCacheMaxBytes
	dir := filepath.Clean(c.processedImageCacheDir())
	if dir == "." || dir == string(filepath.Separator) || strings.TrimSpace(dir) == "" {
		return // 根目录白名单，防误删
	}

	// 第一趟：只累加总大小。
	var total int64
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil // 最佳努力，忽略不可达项
		}
		if info, statErr := d.Info(); statErr == nil {
			total += info.Size()
		}
		return nil
	})
	if total <= maxBytes {
		return
	}

	lowWater := maxBytes * 9 / 10 // 降到 90% 低水位，滞回避免每轮抖动
	need := total - lowWater
	// 多收 25% 的候选做余量：删除可能失败（Windows 上正被读取的页文件 unlink 会失败），
	// 候选只是「最旧的一批」而非全部文件，删除失败时没有更多候选可补。
	need += need / 4

	victims := collectOldestFiles(dir, need)
	removed, freed := 0, int64(0)
	for _, v := range victims {
		if total <= lowWater {
			break
		}
		if err := os.Remove(v.path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				slog.Warn("Failed to evict page cache file", "path", v.path, "error", err)
			}
			continue
		}
		total -= v.size
		removed++
		freed += v.size
	}
	if removed > 0 {
		slog.Info("Page cache budget enforced", "removed_files", removed, "freed_bytes", freed, "remaining_bytes", total)
	}
	if total > lowWater {
		// 候选不够或删除大量失败。下一轮 ticker 会再来一次，这里只记一笔便于诊断。
		slog.Warn("Page cache still above low water mark after eviction",
			"remaining_bytes", total, "low_water", lowWater)
	}
}

// pageCacheVictim 是一个待淘汰的缓存文件。
type pageCacheVictim struct {
	path string
	size int64
	mod  time.Time
}

// victimHeap 是按 mtime 的**大顶堆**：堆顶是候选里最新的那个。
// 这样在累计大小已经够删时，可以把最新的挤出去、只留最旧的一批。
type victimHeap []pageCacheVictim

func (h victimHeap) Len() int            { return len(h) }
func (h victimHeap) Less(i, j int) bool  { return h[i].mod.After(h[j].mod) }
func (h victimHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *victimHeap) Push(x interface{}) { *h = append(*h, x.(pageCacheVictim)) }
func (h *victimHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// collectOldestFiles 返回按 mtime 从旧到新排列的一批文件，其累计大小刚好覆盖 need。
// 常驻内存与「需要删多少」成正比，与目录里总共有多少文件无关。
func collectOldestFiles(dir string, need int64) []pageCacheVictim {
	h := &victimHeap{}
	heap.Init(h)
	var heldBytes int64

	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		candidate := pageCacheVictim{path: path, size: info.Size(), mod: info.ModTime()}
		heap.Push(h, candidate)
		heldBytes += candidate.size

		// 只要挤掉堆顶（最新的那个）之后仍然够删，就挤掉它。
		for h.Len() > 1 {
			newest := (*h)[0]
			if heldBytes-newest.size < need {
				break
			}
			heap.Pop(h)
			heldBytes -= newest.size
		}
		return nil
	})

	victims := make([]pageCacheVictim, 0, h.Len())
	for h.Len() > 0 {
		victims = append(victims, heap.Pop(h).(pageCacheVictim))
	}
	// heap.Pop 按「最新优先」出堆，反转成「最旧优先」。
	for i, j := 0, len(victims)-1; i < j; i, j = i+1, j-1 {
		victims[i], victims[j] = victims[j], victims[i]
	}
	return victims
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
		thumbDir := config.ThumbnailDir(c.currentConfig())

		fullPath := filepath.Join(thumbDir, coverPath)
		if info, err := os.Stat(fullPath); err == nil {
			// 基于封面路径 + 文件修改时间 + 大小生成弱 ETag：缩略图重建覆盖文件时 ETag 必变、
			// 不会复读旧封面；内容不变则客户端可凭 If-None-Match 命中 304，省去整图重传。
			// http.ServeFile 仍会提供 Last-Modified 作为兜底条件请求。
			etag := weakETag(fmt.Sprintf("cover-%s-%d-%d", coverPath, info.ModTime().UnixNano(), info.Size()))
			w.Header().Set("Cache-Control", pageImageCacheControl)
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

// bookDownloadContentType 按归档扩展名推导整卷下载的 MIME。非 PSE 的 OPDS 客户端需要凭正确的
// MIME 识别 CBZ/CBR 整卷文件；未知扩展名回退 application/octet-stream。
// 只声明扫描器真正会摄入并能分页的格式（config.SupportedScanFormats = zip/cbz/rar/cbr）——
// 不要为 cb7/7z/pdf 返回专属 MIME：这些格式既不会入库、解析层也无法分页，向 OPDS 宣告它们
// 会让客户端拿到无法阅读的“伪支持”下载链接。
func bookDownloadContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cbz", ".zip":
		return "application/vnd.comicbook+zip"
	case ".cbr", ".rar":
		return "application/vnd.comicbook-rar"
	default:
		return "application/octet-stream"
	}
}

// serveBookFile 下发整卷归档原始文件，供非 PSE 的 OPDS 阅读器整卷下载。
// PSE 页面流只逐页取 JPEG；桌面/传统 OPDS 客户端需要带正确 MIME 的整包 CBZ/CBR/PDF。
// 通过 http.ServeContent 下发以支持 Range 断点续传；鉴权与既有 /api 图片链接一致（server.auth
// 开启时同样需要令牌）。文件路径取自可信的 books 行，而非用户输入。
func (c *Controller) serveBookFile(w http.ResponseWriter, r *http.Request) {
	bookID, err := parseID(r, "bookId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid book ID")
		return
	}

	handle, err := c.content.Open(r.Context(), bookID, c.taskEngine.engine)
	if err != nil {
		switch {
		case errors.Is(err, bookcontent.ErrSourceMissing):
			jsonError(w, http.StatusNotFound, "Book entity not found")
		case errors.Is(err, bookcontent.ErrBusy):
			writeLibraryUnavailable(w)
		default:
			var fileErr *bookcontent.FileError
			if errors.As(err, &fileErr) {
				writeStorageFailure(w, c.diagnoseStorageFailure(r.Context(), storageTargetFromSource(fileErr.Source), err), "download_book")
			} else {
				jsonError(w, http.StatusInternalServerError, "Failed to open book")
			}
		}
		return
	}
	defer handle.Close()
	source := handle.Source
	displayName := filepath.Base(source.Path)
	if strings.TrimSpace(displayName) == "" || displayName == "." || displayName == string(filepath.Separator) {
		displayName = strconv.FormatInt(bookID, 10)
	}
	// filename= 提供纯 ASCII 兜底（book ID + 扩展名，永远可解析），filename*= 用 UTF-8 百分号编码
	// 携带真实（可能含中文）卷名，兼容不识别 RFC 5987 的老客户端。
	asciiName := strconv.FormatInt(bookID, 10) + strings.ToLower(filepath.Ext(source.Path))
	w.Header().Set("Content-Type", bookDownloadContentType(source.Path))
	w.Header().Set("Content-Disposition", contentDispositionAttachment(asciiName, displayName))
	// 原始归档不依赖 Origin，清除 CORS 中间件写入的 Vary: Origin，便于客户端缓存与断点续传。
	w.Header().Del("Vary")

	// http.ServeContent 处理 Range、If-Modified-Since、Content-Length 并设置 Accept-Ranges，
	// 且在 Content-Type 已预设时保留我们给出的归档 MIME（不做字节嗅探）。
	http.ServeContent(w, r, displayName, handle.Info.ModTime(), handle)
}
