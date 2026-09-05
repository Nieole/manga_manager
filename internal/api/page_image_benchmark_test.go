// 本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func BenchmarkServePageImage_RawConsecutivePages(b *testing.B) {
	controller, store, _, rootDir := newTestController(b)
	_, _, book := seedBookFixture(b, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 50)
	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")

	pages := make(map[string][]byte, 50)
	for i := 1; i <= 50; i++ {
		pages[fmt.Sprintf("%03d.png", i)] = png1x1
	}
	if err := writeTestCBZ(archivePath, pages); err != nil {
		b.Fatalf("write test cbz failed: %v", err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		b.Fatalf("stat archive failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(
		`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
		archivePath,
		info.Size(),
		info.ModTime(),
		50,
		book.ID,
	); err != nil {
		b.Fatalf("update book archive metadata failed: %v", err)
	}

	req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
	req = withRouteParam(req, "pageNumber", "1")
	controller.servePageImage(httptest.NewRecorder(), req)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		page := int64(i%50 + 1)
		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", strconv.FormatInt(page, 10))
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	}
}

// benchmarkReaderPage 把同一页反复取出来，每轮先清掉内存 LRU，量的是冷缓存下的真实转码开销。
// 线上命中缓存的那些请求不付这笔钱，但每个新档位的第一次访问都要付。
func benchmarkReaderPage(b *testing.B, entryName string, source []byte, query string) {
	controller, bookID := seedReaderPageBook(b, entryName, source)
	fetchReaderPage(b, controller, bookID, query) // 预热归档句柄与页清单

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		controller.imageCache.Purge()
		b.StartTimer()
		fetchReaderPage(b, controller, bookID, query)
	}
}

// BenchmarkServePageImage_RawPassthrough 是基线：不带尺寸时整页原始字节直接透传。
func BenchmarkServePageImage_RawPassthrough(b *testing.B) {
	benchmarkReaderPage(b, "001.png", readerPagePNG(b, 1600, 2300), "")
}

// BenchmarkServePageImage_FilterWithoutSize 是修好之前那条路：有核无尺寸，同样透传。
func BenchmarkServePageImage_FilterWithoutSize(b *testing.B) {
	benchmarkReaderPage(b, "001.png", readerPagePNG(b, 1600, 2300), "?filter=lanczos3")
}

// BenchmarkServePageImage_ResampledPNG 是新增成本：解码 + Lanczos3 重采样 + PNG 重编码。
// PNG 是最贵的一档，阅读器默认「原始格式」时 PNG 页图走的就是这条。
func BenchmarkServePageImage_ResampledPNG(b *testing.B) {
	benchmarkReaderPage(b, "001.png", readerPagePNG(b, 1600, 2300), "?w=1280&fit=inside&filter=lanczos3")
}

// BenchmarkServePageImage_RawPassthroughJPEG 是 JPEG 页的基线，与下面那条配对读。
func BenchmarkServePageImage_RawPassthroughJPEG(b *testing.B) {
	benchmarkReaderPage(b, "001.jpg", readerPageJPEG(b, 1600, 2300), "")
}

// BenchmarkServePageImage_ResampledJPEG 是常见的另一档：JPEG 源，重编码比 PNG 便宜得多。
func BenchmarkServePageImage_ResampledJPEG(b *testing.B) {
	benchmarkReaderPage(b, "001.jpg", readerPageJPEG(b, 1600, 2300), "?w=1280&fit=inside&filter=lanczos3")
}

// BenchmarkServePageImage_TargetLargerThanSource 量的是「档位够不着源图」那一档：框装得下 1600 宽的
// 页，缩放库不放大，整条管线因此跳过、按原始字节透传。高分屏叠 DPR 后适应宽度常年落在这一档。
func BenchmarkServePageImage_TargetLargerThanSource(b *testing.B) {
	benchmarkReaderPage(b, "001.png", readerPagePNG(b, 1600, 2300), "?w=3072&fit=inside&filter=lanczos3")
}

// BenchmarkServePageImage_TargetLargerThanSourceJPEG 是同一档的 JPEG 源，与上面配对读。
func BenchmarkServePageImage_TargetLargerThanSourceJPEG(b *testing.B) {
	benchmarkReaderPage(b, "001.jpg", readerPageJPEG(b, 1600, 2300), "?w=3072&fit=inside&filter=lanczos3")
}

func BenchmarkGetPagesByBook_WithManifestCache(b *testing.B) {
	controller, store, _, rootDir := newTestController(b)
	_, _, book := seedBookFixture(b, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 50)
	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")

	pages := make(map[string][]byte, 50)
	for i := 1; i <= 50; i++ {
		pages[fmt.Sprintf("%03d.png", i)] = png1x1
	}
	if err := writeTestCBZ(archivePath, pages); err != nil {
		b.Fatalf("write test cbz failed: %v", err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		b.Fatalf("stat archive failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(
		`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
		archivePath,
		info.Size(),
		info.ModTime(),
		50,
		book.ID,
	); err != nil {
		b.Fatalf("update book archive metadata failed: %v", err)
	}

	req := requestWithRouteParam(http.MethodGet, "/api/books/page-list/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
	controller.getPagesByBook(httptest.NewRecorder(), req)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := requestWithRouteParam(http.MethodGet, "/api/books/page-list/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
		rec := httptest.NewRecorder()
		controller.getPagesByBook(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	}
}
