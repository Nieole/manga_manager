// 本文件是业务回归测试，属于后端 HTTP API 层：守阅读器的六个重采样滤镜真的落到字节上。
// 判据是同一页在不同插值核下输出不同——界面给了选项而画面逐字节相同，就等于没这个功能。

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

// readerPagePNG 造一张带高频花纹的页图。平滑渐变经不同插值核可能落到同一批像素上，
// 分不出滤镜有没有生效；细网格才逼得出各个核的差异。
func readerPagePNG(t testing.TB, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0)
			if (x/3+y/2)%2 == 0 {
				shade = 245
			}
			if (x+y)%7 == 0 {
				shade = uint8((x * y) % 251)
			}
			img.SetRGBA(x, y, color.RGBA{R: shade, G: uint8(255 - int(shade)), B: shade / 2, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode reader page png: %v", err)
	}
	return buf.Bytes()
}

// readerPageJPEG 把同一张花纹页编成 JPEG，用于量常见格式的转码开销。
func readerPageJPEG(t testing.TB, width, height int) []byte {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(readerPagePNG(t, width, height)))
	if err != nil {
		t.Fatalf("decode reader page png: %v", err)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, decoded, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode reader page jpeg: %v", err)
	}
	return buf.Bytes()
}

// seedReaderPageBook 落一本单页归档，并把书表的 path/size/mtime/page_count 对齐到磁盘现状。
// 归档内的条目名决定这一页的 MediaType，进而决定转码链继承哪个编码器——扩展名必须与字节相符。
func seedReaderPageBook(t testing.TB, entryName string, pageData []byte) (*Controller, int64) {
	t.Helper()
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 1)

	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
	if err := writeTestCBZ(archivePath, map[string][]byte{entryName: pageData}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}
	t.Cleanup(func() { parser.EvictArchiveFromPool(archivePath) })

	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("stat archive failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(
		`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
		archivePath, info.Size(), info.ModTime(), 1, book.ID,
	); err != nil {
		t.Fatalf("update book archive metadata failed: %v", err)
	}
	return controller, book.ID
}

// fetchReaderPage 按给定查询串取第一页，返回响应体。
func fetchReaderPage(t testing.TB, controller *Controller, bookID int64, query string) []byte {
	t.Helper()
	req := requestWithRouteParam(http.MethodGet, "/api/pages/1/1"+query, nil, "bookId", strconv.FormatInt(bookID, 10))
	req = withRouteParam(req, "pageNumber", "1")
	rec := httptest.NewRecorder()
	controller.servePageImage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("query %q: expected 200, got %d body=%s", query, rec.Code, rec.Body.String())
	}
	if rec.Body.Len() == 0 {
		t.Fatalf("query %q: empty body", query)
	}
	return append([]byte(nil), rec.Body.Bytes()...)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TestReaderResamplingFiltersChangeBytes 是这个功能的验收判据。
//
// 阅读器下拉里的六个重采样滤镜是缩放用的插值核，本身不是一次独立操作：不下发目标尺寸时
// 服务端无事可做，六项选下来画面逐字节相同。带上尺寸之后，每个核必须给出各自的字节。
func TestReaderResamplingFiltersChangeBytes(t *testing.T) {
	source := readerPagePNG(t, 600, 840)
	controller, bookID := seedReaderPageBook(t, "001.png", source)

	raw := fetchReaderPage(t, controller, bookID, "")
	if !bytes.Equal(raw, source) {
		t.Fatal("不带任何参数时应透传原始页字节")
	}
	t.Logf("filter=%-11s %dx%d %7d bytes sha256=%s", "(none)", 600, 840, len(raw), sha256Hex(raw))

	filters := []string{"lanczos3", "bicubic", "mitchell", "lanczos2", "bspline", "catmullrom"}
	digests := make(map[string]string, len(filters))
	for _, filter := range filters {
		body := fetchReaderPage(t, controller, bookID, "?w=256&fit=inside&filter="+filter)
		if bytes.Equal(body, raw) {
			t.Fatalf("滤镜 %s 带尺寸时仍交出原始字节——重采样没有发生", filter)
		}
		cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("滤镜 %s 的输出解不开：%v", filter, err)
		}
		if cfg.Width != 256 {
			t.Fatalf("滤镜 %s 期望缩到宽 256，实际 %dx%d", filter, cfg.Width, cfg.Height)
		}
		// imaging.Fit 对任一边 <= 0 直接返回空图：只给宽度时 bspline / catmullrom 会交出整页空白。
		if cfg.Height <= 1 {
			t.Fatalf("滤镜 %s 只给宽度时交出了空图（%dx%d）", filter, cfg.Width, cfg.Height)
		}
		digests[filter] = sha256Hex(body)
		t.Logf("filter=%-11s %dx%d %7d bytes sha256=%s", filter, cfg.Width, cfg.Height, len(body), digests[filter])
	}

	for i := 0; i < len(filters); i++ {
		for j := i + 1; j < len(filters); j++ {
			a, b := filters[i], filters[j]
			if digests[a] == digests[b] {
				t.Fatalf("滤镜 %s 与 %s 输出同一份字节（sha256 %s）——用户选了也看不出区别", a, b, digests[a])
			}
		}
	}
}

// TestReaderFilterWithoutSizeStaysIdentical 守另一半：没有目标尺寸时六个滤镜本就无事可做，
// 服务端不该假装重采样一趟，而应原样透传。
func TestReaderFilterWithoutSizeStaysIdentical(t *testing.T) {
	source := readerPagePNG(t, 320, 448)
	controller, bookID := seedReaderPageBook(t, "001.png", source)

	for _, filter := range []string{"lanczos3", "bicubic", "bspline"} {
		if body := fetchReaderPage(t, controller, bookID, "?filter="+filter); !bytes.Equal(body, source) {
			t.Fatalf("滤镜 %s 无尺寸时应透传原始字节，实际拿到 %d 字节（源 %d 字节）", filter, len(body), len(source))
		}
	}
}

// TestReaderTargetSizeSnapsToBucket 守服务端那道档位闸：目标尺寸来自客户端，逐像素照单全收
// 会让同一页在每个窗口宽度上各留一份缓存。落在同一档的请求必须交出同一份字节。
func TestReaderTargetSizeSnapsToBucket(t *testing.T) {
	// 源图要比最大档还宽，否则 fit=inside 不放大，各档都退化成原图、分不出档来。
	source := readerPagePNG(t, 3300, 2000)
	controller, bookID := seedReaderPageBook(t, "001.png", source)

	sameBucket := []string{"?w=1100&fit=inside&filter=lanczos3", "?w=1250&fit=inside&filter=lanczos3", "?w=1280&fit=inside&filter=lanczos3"}
	want := sha256Hex(fetchReaderPage(t, controller, bookID, sameBucket[0]))
	for _, query := range sameBucket[1:] {
		if got := sha256Hex(fetchReaderPage(t, controller, bookID, query)); got != want {
			t.Fatalf("%s 与 %s 应落在同一档（w=1280），却给出不同字节", query, sameBucket[0])
		}
	}

	// 上一档与下一档必须分开，否则档位化就退化成「所有尺寸一个样」。
	other := sha256Hex(fetchReaderPage(t, controller, bookID, "?w=1400&fit=inside&filter=lanczos3"))
	if other == want {
		t.Fatal("w=1400 应落到 1536 档，不该与 1280 档同字节")
	}

	// 单边超过阅读器上限时夹到上限，不按请求值放大。
	huge := fetchReaderPage(t, controller, bookID, "?w=7000&fit=inside&filter=lanczos3")
	cfg, _, err := image.DecodeConfig(bytes.NewReader(huge))
	if err != nil {
		t.Fatalf("decode clamped output: %v", err)
	}
	// 请求 7000 会被夹到阅读器上限 3072，而不是照单缩到 7000。
	if cfg.Width != 3072 {
		t.Fatalf("w=7000 应被夹到 3072，实际 %d", cfg.Width)
	}
}

// TestReaderFitInsidePreservesAspect 守 fit-screen 这一档：同时给宽高时必须等比缩进框内，
// 而不是把页拉伸成框的形状。
func TestReaderFitInsidePreservesAspect(t *testing.T) {
	source := readerPagePNG(t, 600, 900) // 2:3
	controller, bookID := seedReaderPageBook(t, "001.png", source)

	body := fetchReaderPage(t, controller, bookID, "?w=512&h=512&fit=inside&filter=lanczos3")
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode fit output: %v", err)
	}
	if cfg.Width != 341 || cfg.Height != 512 {
		t.Fatalf("expected aspect-preserving 341x512 inside a 512x512 box, got %dx%d", cfg.Width, cfg.Height)
	}
	if fmt.Sprintf("%dx%d", cfg.Width, cfg.Height) == "512x512" {
		t.Fatal("页被拉伸成了正方形")
	}
}

// callReaderPage 取第一页并把整个响应交出来：第一条要看的是状态码、ETag 与回退头，不只是响应体。
func callReaderPage(t testing.TB, controller *Controller, bookID int64, query, ifNoneMatch string) *httptest.ResponseRecorder {
	t.Helper()
	req := requestWithRouteParam(http.MethodGet, "/api/pages/1/1"+query, nil, "bookId", strconv.FormatInt(bookID, 10))
	req = withRouteParam(req, "pageNumber", "1")
	if ifNoneMatch != "" {
		req.Header.Set("If-None-Match", ifNoneMatch)
	}
	rec := httptest.NewRecorder()
	controller.servePageImage(rec, req)
	return rec
}

// writeFakeAIEngine 造一个假的 ncnn 家族引擎。execWaifu2x 只要求「绝对路径 + 常规文件」，
// 因此一个可执行脚本足以走完真实的子进程链路：engineBody 决定它是跑挂还是产出放大图。
func writeFakeAIEngine(t *testing.T, dir, name, engineBody string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(engineBody), 0o755); err != nil {
		t.Fatalf("write fake engine %s: %v", name, err)
	}
	return path
}

// TestReaderAIFallbackDoesNotPinPageToUnupscaled 是第一条的验收判据，跑的就是用户那条时间线：
// 引擎不可用时先看一页，把引擎装好之后，同一页必须真的换成放大过的图。
//
// 破了的表现是服务端把「没放大却又被重新有损编码过」的结果按 AI 的缓存键写进内存 LRU 与磁盘缓存，
// ETag 又由同一个键导出——浏览器直接 304，引擎装好也再换不掉那一张。
func TestReaderAIFallbackDoesNotPinPageToUnupscaled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("假引擎是 sh 脚本")
	}
	source := readerPageJPEG(t, 120, 168)
	controller, bookID := seedReaderPageBook(t, "001.jpg", source)
	// 磁盘缓存必须开着：第一条的污染正是落在它上面。
	cfg := controller.currentConfig()
	cfg.Cache.PageDiskCacheEnabled = true
	controller.config.Replace(&cfg)

	engineDir := t.TempDir()
	upscaledPath := filepath.Join(engineDir, "upscaled.png")
	upscaled := readerPagePNG(t, 240, 336)
	if err := os.WriteFile(upscaledPath, upscaled, 0o644); err != nil {
		t.Fatalf("write fake engine output: %v", err)
	}
	broken := writeFakeAIEngine(t, engineDir, "broken-engine", "#!/bin/sh\nexit 1\n")
	working := writeFakeAIEngine(t, engineDir, "working-engine",
		"#!/bin/sh\nout=\"\"\nwhile [ \"$#\" -gt 0 ]; do\n  if [ \"$1\" = \"-o\" ]; then out=\"$2\"; fi\n  shift\ndone\ncp '"+upscaledPath+"' \"$out\"\n")

	const query = "?filter=waifu2x&w2x_scale=2&w2x_noise=0&w2x_format=png"

	cfg = controller.currentConfig()
	cfg.Scanner.Waifu2xPath = broken
	controller.config.Replace(&cfg)

	degraded := callReaderPage(t, controller, bookID, query, "")
	if degraded.Code != http.StatusOK {
		t.Fatalf("引擎缺席不该让整页失败，got %d body=%s", degraded.Code, degraded.Body.String())
	}
	if !bytes.Equal(degraded.Body.Bytes(), source) {
		t.Fatalf("没放大成就该交出源字节，实际拿到 %d 字节（源 %d 字节）——白丢了一次有损重编码",
			degraded.Body.Len(), len(source))
	}
	if got := degraded.Header().Get("X-Image-Fallback"); got != "waifu2x" {
		t.Fatalf("交付的不是用户点的那张图，响应必须说出来，X-Image-Fallback=%q", got)
	}
	stats, err := controller.collectPageCacheStats()
	if err != nil {
		t.Fatalf("collect page cache stats: %v", err)
	}
	if stats.FileCount != 0 {
		t.Fatalf("没放大的结果不得按 AI 缓存键落盘，磁盘缓存里出现了 %d 个文件", stats.FileCount)
	}
	degradedETag := degraded.Header().Get("ETag")
	if degradedETag == "" {
		t.Fatal("expected ETag on degraded response")
	}

	// 用户把引擎装好了：同一个地址、带上上一次的 ETag，必须重新取一次并换成放大后的图。
	cfg = controller.currentConfig()
	cfg.Scanner.Waifu2xPath = working
	controller.config.Replace(&cfg)

	recovered := callReaderPage(t, controller, bookID, query, degradedETag)
	if recovered.Code != http.StatusOK {
		t.Fatalf("引擎装好之后必须重新出图，却拿到 %d——旧 ETag 把这一页钉死在没放大的那张上", recovered.Code)
	}
	if !bytes.Equal(recovered.Body.Bytes(), upscaled) {
		t.Fatalf("引擎装好之后应拿到放大后的图，实际 %d 字节（放大图 %d 字节，源 %d 字节）",
			recovered.Body.Len(), len(upscaled), len(source))
	}
	if got := recovered.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("w2x_format=png 时 Content-Type 应为 image/png，got %q", got)
	}
	if recovered.Header().Get("X-Image-Fallback") != "" {
		t.Fatal("这一次是真放大，不该再报回退")
	}
}

// TestReaderTargetBiggerThanSourceKeepsSourceBytes 是第二条的验收判据。
//
// 高分屏 + 适应宽度会取到 2048 或 3072 档，而漫画页原宽通常小得多。这一档下缩放库不放大，
// 六个插值核本就无从生效；破了的表现是服务端仍按 format 重编码一次，用户挨个选一遍逐字节相同，
// 却每页白丢一次画质、白付一次完整解码 + 编码，还把整页大小的文件写进磁盘缓存。
func TestReaderTargetBiggerThanSourceKeepsSourceBytes(t *testing.T) {
	source := readerPageJPEG(t, 300, 420)
	controller, bookID := seedReaderPageBook(t, "001.jpg", source)
	cfg := controller.currentConfig()
	cfg.Cache.PageDiskCacheEnabled = true
	controller.config.Replace(&cfg)

	// 三种适应模式各下发哪几条边：适应宽度只发宽、适应高度只发高、适应屏幕两条边一起发。
	boxes := []string{"?w=3072&fit=inside", "?h=3072&fit=inside", "?w=2048&h=2048&fit=inside"}
	for _, filter := range []string{"lanczos3", "bicubic", "mitchell", "lanczos2", "bspline", "catmullrom"} {
		for _, box := range boxes {
			body := fetchReaderPage(t, controller, bookID, box+"&filter="+filter)
			if !bytes.Equal(body, source) {
				t.Fatalf("%s&filter=%s：框装得下源图，重采样是恒等操作，必须透传源字节，实际拿到 %d 字节（源 %d 字节）",
					box, filter, len(body), len(source))
			}
		}
	}

	stats, err := controller.collectPageCacheStats()
	if err != nil {
		t.Fatalf("collect page cache stats: %v", err)
	}
	if stats.FileCount != 0 {
		t.Fatalf("透传的整页原图不该写进磁盘缓存，实际 %d 个文件 %d 字节", stats.FileCount, stats.FileSize)
	}

	// 反面：框缩得进源图时六个核仍要各干各的活，这条短路不能把 f9141d9 的成果吃回去。
	if body := fetchReaderPage(t, controller, bookID, "?w=256&fit=inside&filter=lanczos3"); bytes.Equal(body, source) {
		t.Fatal("w=256 缩得进 300 宽的源图，必须真的重采样")
	}
}
