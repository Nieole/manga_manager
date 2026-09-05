package api

import (
	"container/heap"
	"context"
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

// pageImageCacheControl 是所有需要鉴权的图片端点（页图/封面/缩略图）的缓存策略。
//
// 必须是 private + 必须回源验证，不能放宽成 public 长 max-age：public 允许共享缓存
// （企业代理、CDN）替别的用户直接命中本该鉴权的页；这些 URL 又没有内容版本令牌，长
// max-age 会让换封面、重建缩略图后旧图继续顶很久。ETag 仍在，条件请求命中时返回 304，
// 省下的带宽与长 max-age 相同。
const pageImageCacheControl = "private, max-age=0, must-revalidate"

// pageImageCacheEpoch 是派生图缓存键里的转码结果版本号。
//
// 缓存键其余部分只描述**请求**（尺寸、格式、滤镜、归档 mtime 与大小），描述不了「同样的请求
// 这版代码会转出什么字节」。转码链一旦被修成产出不同字节，存量条目就是错的，而磁盘缓存、
// 内存 LRU 与浏览器（ETag 由缓存键导出）都不会自行失效。改动转码结果时必须递增本值：
// 新键一律未命中、重算一次，旧文件由 enforcePageCacheBudget 按最旧优先回收。
const pageImageCacheEpoch = "v4"

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
	// fit=inside 把 w/h 当作「框」：等比缩到框内，不拉伸也不放大。阅读器的适应模式发的是它，
	// 封面与缩略图不发，仍按画布精确缩放。
	fitInside := r.URL.Query().Get("fit") == "inside"

	// 目标尺寸必须在进入缓存/转码路径之前校验：这两个值最终会变成 resize 的目标画布，
	// 负值经 uint() 转换会回绕成天文数字、超大值直接申请数 GB 缓冲，任一都能让进程 OOM。
	// 解析失败按「未指定」处理（保持既有宽松语义），但一旦是合法数字就必须落在安全区间内。
	reqWidth, wErr := parsePositiveDimension(widthStr)
	reqHeight, hErr := parsePositiveDimension(heightStr)
	if wErr != nil || hErr != nil {
		jsonError(w, http.StatusBadRequest, "Invalid image dimension parameter")
		return
	}
	if err := images.ValidateTargetDimensions(reqWidth, reqHeight); err != nil {
		jsonError(w, http.StatusBadRequest, "Requested image size exceeds limits")
		return
	}
	// 档位化：客户端按容器尺寸算出的值仍是外部输入，逐像素照单全收会让同一页在每个窗口宽度上
	// 各留一份缓存条目。取整到档位后，缓存键与转码结果一起收敛到每本书有限几档。
	reqWidth = images.SnapTargetDimension(reqWidth)
	reqHeight = images.SnapTargetDimension(reqHeight)

	// 构建缓存 Key：包含所有会改变最终图像字节的处理参数，防止切换滤镜、画质、放大参数后复用旧图。
	// 同时引入文件修改时间和大小，避免归档被覆盖或 ID 复用时浏览器继续命中旧 ETag。
	w2xScaleStr := r.URL.Query().Get("w2x_scale")
	w2xNoiseStr := r.URL.Query().Get("w2x_noise")
	w2xFormatStr := r.URL.Query().Get("w2x_format")
	// 缓存键与转码 profile 都记档位化之后的尺寸：w=1100 与 w=1250 落到同一档，就该命中同一份缓存。
	widthKey, heightKey := strconv.Itoa(reqWidth), strconv.Itoa(reqHeight)
	transform := pageImageTransformProfile(format, widthKey, heightKey, filter, autoCrop, fitInside, w2xScaleStr, w2xNoiseStr, w2xFormatStr)
	cacheKey := fmt.Sprintf("%s-%d-%d-%d-%d-%s-%s-%s-%s-%s-%s-%s-%s-%t-%t",
		pageImageCacheEpoch, bookID, pageNumber, source.FileModifiedAt.UnixNano(), source.Size,
		widthKey, heightKey, format, qualityStr, filter, w2xScaleStr, w2xNoiseStr, w2xFormatStr, autoCrop, fitInside)
	// 图片资源不依赖 Origin，清除 CORS 中间件写入的 Vary: Origin，否则浏览器无法命中缓存。
	w.Header().Del("Vary")
	etag := weakETag(cacheKey)
	if r.Header.Get("If-None-Match") == etag {
		annotatePageImageRequest(ctx, bookID, pageNumber, true, "client", transform)
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", pageImageCacheControl)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// isThumbnailReq 判的是「这个请求值不值得去翻缓存」，只看参数、不看字节。真正能不能写缓存要等
	// 转码链回话（见下面的 derived）：参数要求了加工，不等于加工真的发生了。
	isThumbnailReq := reqWidth > 0 || reqHeight > 0 || format != "" || qualityStr != "" || images.FilterChangesPixels(filter, reqWidth, reqHeight) || autoCrop
	diskPageCacheEnabled := c.diskPageCacheEnabled(source)
	if isThumbnailReq {
		if cachedData, ok := c.imageCache.Get(cacheKey); ok {
			contentType := detectImageContentType(cachedData) // AVIF 感知，避免命中缓存时退化为 octet-stream
			annotatePageImageDiagnostics(ctx, false, false, false, true)
			annotatePageImageRequest(ctx, bookID, pageNumber, true, "memory", transform)
			c.logPageImageServed(bookID, pageNumber, "memory", contentType, len(cachedData), time.Since(started), format, filter, autoCrop)
			w.Header().Set("Content-Type", contentType) // 缓存命中时也以实际字节探测类型为准，避免前端按错误格式解码。
			w.Header().Set("Content-Length", strconv.Itoa(len(cachedData)))
			w.Header().Set("Cache-Control", pageImageCacheControl)
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
				w.Header().Set("Cache-Control", pageImageCacheControl)
				w.Header().Set("ETag", etag)
				w.Write(cachedData)
				return
			}
		}
	}

	// 冷缓存下用 single-flight 合并同一 cacheKey 的并发转码：只有 leader 真正取存储令牌、读页、转码、
	// 写缓存，其余等待者复用同一结果，避免重复 CPU 转码与归档读取。HTTP 错误编码进结果结构体，闭包本身
	// 恒返回 nil error（不让 singleflight 把某次错误缓存给其他 key）。
	type pageServeResult struct {
		data        []byte
		contentType string
		// derived 为真表示这份字节是真按请求参数加工出来的产物。只有它才可以进缓存、
		// 才可以拿请求参数导出的 ETag 当身份——透传与各种回退都不是。
		derived bool
		// fallback 非空表示交付的不是请求许诺的东西，值是原因（没上的 AI 滤镜名，或 process-error）。
		fallback string
		status   int // 0 表示成功，否则为要返回的 HTTP 错误码
		message  string
		// failure 非空时改由 writeStorageFailure 下发：读不到字节的失败要带上分类与资料库线索，
		// 而不是一句状态码加短语。它与 status/message 互斥。
		failure *storageFailure
	}
	resAny, _, _ := c.pageTranscodeGroup.Do(cacheKey, func() (any, error) {
		// 二次检查内存缓存：可能有另一个 leader 刚把它填好。
		if isThumbnailReq {
			if cached, ok := c.imageCache.Get(cacheKey); ok {
				return &pageServeResult{data: cached, contentType: detectImageContentType(cached), derived: true}, nil
			}
		}
		// 与发起请求的客户端取消解耦：single-flight 下某个客户端断开不应让所有等待者的转码失败。
		// WithoutCancel 保留 ctx 上的诊断/存储 value，仅去除取消信号。
		workCtx := context.WithoutCancel(ctx)

		storagePolicy := config.ResolveStoragePolicy(c.currentConfig(), source.Path)
		readerLease, err := storageio.Default.Acquire(workCtx, storageio.Request{
			VolumeKey:        storagePolicy.VolumeKey,
			Limit:            storagePolicy.IOPolicy.ArchiveOpenConcurrency,
			Kind:             storageio.WorkKindReader,
			PauseWhenReading: storagePolicy.IOPolicy.PauseBackgroundWhenReading,
		})
		if err != nil {
			return &pageServeResult{status: http.StatusServiceUnavailable, message: "Storage is busy"}, nil
		}
		annotatePageImageStorage(workCtx, storagePolicy.StorageProfile, storagePolicy.VolumeKey, readerLease.Wait)
		defer readerLease.Release()

		pageInfo, manifestCacheHit, err := c.getBookArchiveSourcePageWithStats(workCtx, source, pageNumber)
		annotatePageImageDiagnostics(workCtx, false, manifestCacheHit, false, false)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) || (pageNumber > source.PageCount && source.PageCount > 0) {
				return &pageServeResult{status: http.StatusNotFound, message: "Page not found"}, nil
			}
			failure := c.diagnoseStorageFailure(workCtx, storageTargetFromSource(source), err)
			return &pageServeResult{failure: &failure}, nil
		}

		archiver, releaseArchive, err := parser.GetArchiveFromPool(source.Path)
		if err != nil {
			failure := c.diagnoseStorageFailure(workCtx, storageTargetFromSource(source), err)
			return &pageServeResult{failure: &failure}, nil
		}
		// 借用期间池不会真正关闭该句柄，避免读到一半被 LRU 淘汰而抽走文件描述符。
		defer releaseArchive()
		annotatePageImageDiagnostics(workCtx, true, false, false, false)

		data, err := archiver.ReadPage(pageInfo.Name)
		if err != nil {
			failure := c.diagnoseStorageFailure(workCtx, storageTargetFromSource(source), err)
			return &pageServeResult{failure: &failure}, nil
		}
		readerLease.Release()

		opts := images.ProcessOptions{
			Format:        format,
			Filter:        filter,
			AutoCrop:      autoCrop,
			Waifu2xPath:   c.currentConfig().Scanner.Waifu2xPath,
			RealCuganPath: c.currentConfig().Scanner.RealCuganPath,
			Waifu2xScale:  2,      // 缺省使用引擎默认2倍
			Waifu2xNoise:  0,      // 缺省使用引擎默认0阶降噪
			Waifu2xFormat: "webp", // 控制引擎默认采用 webp 挤压体积
			FitInside:     fitInside,
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
		// 已在入口处解析并校验过，这里直接复用（0 表示未指定）。
		opts.Width = reqWidth
		opts.Height = reqHeight

		outcome, err := images.ProcessImageDetailed(data, pageInfo.MediaType, opts)
		if err != nil {
			// 处理失败时回退原始字节，且不写缓存：否则会把未处理的回退结果当已处理产物持久缓存，
			// 让后续请求（含临时错误恢复后）永远拿到未处理的图，形成缓存污染。
			slog.Warn("Image process error, fallback to raw source", "error", err)
			return &pageServeResult{data: data, contentType: pageInfo.MediaType, fallback: "process-error"}, nil
		}
		// 只有真按请求参数加工出来的产物才进缓存。透传存下来是整页原图的副本，白占内存与磁盘；
		// AI 引擎缺席时存下来的更是「按 AI 键存的非 AI 图」——用户后来把引擎装好也再换不掉。
		derived := isThumbnailReq && !outcome.Passthrough && outcome.SkippedFilter == ""
		if derived {
			c.cacheImageMemory(cacheKey, outcome.Data)
			if diskPageCacheEnabled {
				if werr := c.writeDiskImageCache(cacheKey, outcome.Data, outcome.ContentType); werr != nil {
					slog.Warn("Failed to write processed page disk cache", "error", werr)
				}
			}
		}
		if outcome.SkippedFilter != "" {
			slog.Warn("Page image delivered without the requested AI filter",
				"book_id", bookID, "page", pageNumber, "filter", outcome.SkippedFilter)
		}
		return &pageServeResult{
			data:        outcome.Data,
			contentType: outcome.ContentType,
			derived:     derived,
			fallback:    outcome.SkippedFilter,
		}, nil
	})

	res := resAny.(*pageServeResult)
	if res.failure != nil {
		writeStorageFailure(w, *res.failure, "page_image")
		return
	}
	if res.status != 0 {
		jsonError(w, res.status, res.message)
		return
	}
	finalData := res.data
	finalContentType := res.contentType

	// 身份跟着实际产物走，不跟着请求参数走。回退产物同样的请求下次可能给出别的字节（引擎装好了、
	// 临时错误恢复了），拿请求参数导出的 ETag 当身份会让浏览器一直 304，那一页永远换不掉。
	responseETag := etag
	if res.fallback != "" {
		responseETag = weakETag(cacheKey + "|fallback:" + res.fallback)
		w.Header().Set("X-Image-Fallback", res.fallback)
	}

	// Cache control for performant client-side static assets
	// In production read this from config or context
	w.Header().Set("Cache-Control", pageImageCacheControl)
	w.Header().Set("ETag", responseETag)

	cacheSource := "raw"
	if res.derived {
		cacheSource = "processed"
	}
	annotatePageImageDiagnostics(ctx, false, false, !res.derived, res.derived)

	// 回退分支的身份到这里才算得出来，因此条件请求要在出图之后再判一次：活已经干过，
	// 至少别把整页字节再发一遍。
	if r.Header.Get("If-None-Match") == responseETag {
		annotatePageImageRequest(ctx, bookID, pageNumber, true, "client", transform)
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", finalContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(finalData)))
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

func normalizeServerImageFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case "", "none", "nearest", "average", "bilinear":
		return ""
	default:
		return filter
	}
}

func pageImageTransformProfile(format, width, height, filter string, autoCrop, fitInside bool, w2xScale, w2xNoise, w2xFormat string) string {
	parts := make([]string, 0, 6)
	if format != "" {
		parts = append(parts, "format:"+format)
	}
	if width != "0" || height != "0" {
		resize := "resize:" + width + "x" + height
		if fitInside {
			resize += ":inside"
		}
		parts = append(parts, resize)
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
	return config.PageCacheDir(c.currentConfig())
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
	// 原子写：直接 WriteFile 时，磁盘写满（ENOSPC）或进程被杀会留下半截文件，
	// 而读取侧只检查「存在且非空」，于是这个截断的字节流会被当成有效缓存长期下发——
	// 用户看到的是某一页永远显示不出来，清缓存才好。先写临时文件再 rename 即可避免。
	target := filepath.Join(dir, fileName)
	tmp, err := os.CreateTemp(dir, "."+fileName+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	committed = true
	return nil
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

	ctx := r.Context()
	book, err := c.store.GetBook(ctx, bookID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Book entity not found")
		return
	}

	file, err := os.Open(book.Path)
	if err != nil {
		writeStorageFailure(w, c.diagnoseStorageFailure(ctx, storageTargetFromBook(book), err), "download_book")
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		if err == nil {
			err = fmt.Errorf("book path is a directory: %s", book.Path)
		}
		writeStorageFailure(w, c.diagnoseStorageFailure(ctx, storageTargetFromBook(book), err), "download_book")
		return
	}

	displayName := filepath.Base(book.Path)
	if strings.TrimSpace(displayName) == "" || displayName == "." || displayName == string(filepath.Separator) {
		displayName = book.Name
	}
	// filename= 提供纯 ASCII 兜底（book ID + 扩展名，永远可解析），filename*= 用 UTF-8 百分号编码
	// 携带真实（可能含中文）卷名，兼容不识别 RFC 5987 的老客户端。
	asciiName := strconv.FormatInt(bookID, 10) + strings.ToLower(filepath.Ext(book.Path))
	w.Header().Set("Content-Type", bookDownloadContentType(book.Path))
	w.Header().Set("Content-Disposition", contentDispositionAttachment(asciiName, displayName))
	// 原始归档不依赖 Origin，清除 CORS 中间件写入的 Vary: Origin，便于客户端缓存与断点续传。
	w.Header().Del("Vary")

	// http.ServeContent 处理 Range、If-Modified-Since、Content-Length 并设置 Accept-Ranges，
	// 且在 Content-Type 已预设时保留我们给出的归档 MIME（不做字节嗅探）。
	http.ServeContent(w, r, displayName, info.ModTime(), file)
}
