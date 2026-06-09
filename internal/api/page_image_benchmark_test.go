// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
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
