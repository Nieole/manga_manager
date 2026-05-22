package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0,
	0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99,
	0x3d, 0x1d, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
	0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestServeCoverImage(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	cacheDir := filepath.Join(rootDir, "thumb-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir failed: %v", err)
	}
	cfg := controller.currentConfig()
	cfg.Cache.Dir = cacheDir
	controller.config.Replace(&cfg)

	coverData := []byte("fake image bytes")
	coverName := "alpha-cover.jpg"
	coverPath := filepath.Join(cacheDir, coverName)
	if err := os.WriteFile(coverPath, coverData, 0o644); err != nil {
		t.Fatalf("write cover file failed: %v", err)
	}

	if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE books SET cover_path = ? WHERE id = ?`, coverName, book.ID); err != nil {
		t.Fatalf("update cover path failed: %v", err)
	}

	t.Run("serves cached cover", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.serveCoverImage(rec, requestWithRouteParam(http.MethodGet, "/api/covers/1", nil, "bookId", strconv.FormatInt(book.ID, 10)))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Cache-Control") != "public, max-age=31536000" {
			t.Fatalf("unexpected cache control header: %q", rec.Header().Get("Cache-Control"))
		}
		if rec.Body.String() != string(coverData) {
			t.Fatalf("unexpected cover body: %q", rec.Body.String())
		}
	})

	t.Run("returns 404 when cover file missing", func(t *testing.T) {
		if err := os.Remove(coverPath); err != nil {
			t.Fatalf("remove cover file failed: %v", err)
		}

		rec := httptest.NewRecorder()
		controller.serveCoverImage(rec, requestWithRouteParam(http.MethodGet, "/api/covers/1", nil, "bookId", strconv.FormatInt(book.ID, 10)))

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}

func TestDiskPageCacheDisabledForSameDiskExternalHDDPolicy(t *testing.T) {
	controller, _, _, rootDir := newTestController(t)
	cfg := controller.currentConfig()
	cfg.Cache.Dir = filepath.Join(rootDir, "cache")
	cfg.Cache.PageDiskCacheEnabled = true
	cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	config.NormalizeConfig(&cfg)
	controller.config.Replace(&cfg)

	source := bookPageSource{
		ID:        1,
		LibraryID: 1,
		Path:      filepath.Join(rootDir, "library", "Series", "Book.cbz"),
	}

	if controller.diskPageCacheEnabled(source) {
		t.Fatal("expected same-disk page cache to be disabled for external HDD policy")
	}
}

func TestServePageImage(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	t.Run("validates route params and missing book", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, requestWithRouteParam(http.MethodGet, "/api/books/page/invalid/1", nil, "bookId", "invalid"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid book id 400, got %d", rec.Code)
		}

		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/invalid", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "invalid")
		rec = httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid page number 400, got %d", rec.Code)
		}

		req = requestWithRouteParam(http.MethodGet, "/api/books/page/999/1", nil, "bookId", "999")
		req = withRouteParam(req, "pageNumber", "1")
		rec = httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected missing book 404, got %d", rec.Code)
		}
	})

	t.Run("returns archive read errors and page bounds errors", func(t *testing.T) {
		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected invalid archive 500, got %d", rec.Code)
		}

		archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
		if err := writeTestCBZ(archivePath, map[string][]byte{
			"001.png": png1x1,
			"002.png": png1x1,
		}); err != nil {
			t.Fatalf("write test cbz failed: %v", err)
		}
		t.Cleanup(func() {
			parser.EvictArchiveFromPool(archivePath)
		})
		if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE books SET path = ?, size = ? WHERE id = ?`, archivePath, int64(len(png1x1)*2), book.ID); err != nil {
			t.Fatalf("update book archive path failed: %v", err)
		}

		req = requestWithRouteParam(http.MethodGet, "/api/books/page/1/3", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "3")
		rec = httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected page out of range 404, got %d", rec.Code)
		}
	})

	t.Run("serves page image bytes", func(t *testing.T) {
		archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
		if _, err := os.Stat(archivePath); err != nil {
			t.Fatalf("expected archive path to exist: %v", err)
		}

		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected serve page 200, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("unexpected page content type: %q", rec.Header().Get("Content-Type"))
		}
		if rec.Header().Get("Cache-Control") != "public, max-age=31536000" {
			t.Fatalf("unexpected cache control header: %q", rec.Header().Get("Cache-Control"))
		}
		if len(rec.Body.Bytes()) == 0 {
			t.Fatal("expected non-empty page bytes")
		}
	})

	t.Run("passes browser-only filters through without processing", func(t *testing.T) {
		archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
		parser.EvictArchiveFromPool(archivePath)
		if err := writeTestCBZ(archivePath, map[string][]byte{
			"001.png": png1x1,
		}); err != nil {
			t.Fatalf("write test cbz failed: %v", err)
		}
		info, err := os.Stat(archivePath)
		if err != nil {
			t.Fatalf("stat archive failed: %v", err)
		}
		if _, err := controller.store.(*database.SqlStore).DB().Exec(
			`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
			archivePath,
			info.Size(),
			info.ModTime(),
			1,
			book.ID,
		); err != nil {
			t.Fatalf("update book archive metadata failed: %v", err)
		}

		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1?filter=bilinear", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected browser-only filter page request 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("unexpected content type: %q", rec.Header().Get("Content-Type"))
		}
		if string(rec.Body.Bytes()) != string(png1x1) {
			t.Fatal("expected browser-only filter to return original page bytes")
		}
	})

	t.Run("returns not modified for matching page image etag", func(t *testing.T) {
		archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
		parser.EvictArchiveFromPool(archivePath)
		if err := writeTestCBZ(archivePath, map[string][]byte{
			"001.png": png1x1,
		}); err != nil {
			t.Fatalf("write test cbz failed: %v", err)
		}
		info, err := os.Stat(archivePath)
		if err != nil {
			t.Fatalf("stat archive failed: %v", err)
		}
		if _, err := controller.store.(*database.SqlStore).DB().Exec(
			`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
			archivePath,
			info.Size(),
			info.ModTime(),
			1,
			book.ID,
		); err != nil {
			t.Fatalf("update book archive metadata failed: %v", err)
		}
		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1?format=png", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected first etag page request 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		etag := rec.Header().Get("ETag")
		if etag == "" {
			t.Fatal("expected page image response to include ETag")
		}

		controller.imageCache.Purge()
		parser.EvictArchiveFromPool(archivePath)
		missingPath := archivePath + ".etag-missing"
		if err := os.Rename(archivePath, missingPath); err != nil {
			t.Fatalf("rename archive failed: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Rename(missingPath, archivePath)
		})

		req = requestWithRouteParam(http.MethodGet, "/api/books/page/1/1?format=png", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		req.Header.Set("If-None-Match", etag)
		rec = httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusNotModified {
			t.Fatalf("expected matching etag 304, got %d body=%s", rec.Code, rec.Body.String())
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("expected 304 body to be empty, got %q", rec.Body.String())
		}
	})

	t.Run("serves processed page from disk cache before opening archive", func(t *testing.T) {
		cfg := controller.currentConfig()
		cfg.Cache.Dir = filepath.Join(rootDir, "processed-cache")
		cfg.Cache.PageDiskCacheEnabled = true
		controller.config.Replace(&cfg)

		archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
		parser.EvictArchiveFromPool(archivePath)
		if err := writeTestCBZ(archivePath, map[string][]byte{
			"001.png": png1x1,
		}); err != nil {
			t.Fatalf("write test cbz failed: %v", err)
		}
		info, err := os.Stat(archivePath)
		if err != nil {
			t.Fatalf("stat archive failed: %v", err)
		}
		if _, err := controller.store.(*database.SqlStore).DB().Exec(
			`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
			archivePath,
			info.Size(),
			info.ModTime(),
			1,
			book.ID,
		); err != nil {
			t.Fatalf("update book archive metadata failed: %v", err)
		}
		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1?format=png", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected first processed page request 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		cachedBody := append([]byte(nil), rec.Body.Bytes()...)
		if len(cachedBody) == 0 {
			t.Fatal("expected non-empty processed page body")
		}

		controller.imageCache.Purge()
		parser.EvictArchiveFromPool(archivePath)
		missingPath := archivePath + ".missing"
		if err := os.Rename(archivePath, missingPath); err != nil {
			t.Fatalf("rename archive failed: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Rename(missingPath, archivePath)
		})

		rec = httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected disk cached page 200 without archive, got %d body=%s", rec.Code, rec.Body.String())
		}
		if rec.Body.String() != string(cachedBody) {
			t.Fatalf("expected disk cached body to match original processed body")
		}
	})

	t.Run("skips processed page disk cache when disabled", func(t *testing.T) {
		cfg := controller.currentConfig()
		cfg.Cache.Dir = filepath.Join(rootDir, "processed-cache-disabled")
		cfg.Cache.PageDiskCacheEnabled = false
		controller.config.Replace(&cfg)

		archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
		parser.EvictArchiveFromPool(archivePath)
		if err := writeTestCBZ(archivePath, map[string][]byte{
			"001.png": png1x1,
		}); err != nil {
			t.Fatalf("write test cbz failed: %v", err)
		}
		info, err := os.Stat(archivePath)
		if err != nil {
			t.Fatalf("stat archive failed: %v", err)
		}
		if _, err := controller.store.(*database.SqlStore).DB().Exec(
			`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
			archivePath,
			info.Size(),
			info.ModTime(),
			1,
			book.ID,
		); err != nil {
			t.Fatalf("update book archive metadata failed: %v", err)
		}

		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1?format=png", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", "1")
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected processed page request 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		stats, err := controller.collectPageCacheStats()
		if err != nil {
			t.Fatalf("collect page cache stats failed: %v", err)
		}
		if stats.FileCount != 0 || stats.FileSize != 0 {
			t.Fatalf("expected disabled disk cache to write no files, got files=%d bytes=%d", stats.FileCount, stats.FileSize)
		}

		controller.imageCache.Purge()
		parser.EvictArchiveFromPool(archivePath)
		missingPath := archivePath + ".disabled-missing"
		if err := os.Rename(archivePath, missingPath); err != nil {
			t.Fatalf("rename archive failed: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Rename(missingPath, archivePath)
		})

		rec = httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("expected disabled disk cache to miss and require archive, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	_ = series
}

func TestBookArchivePageManifestCache(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
	if err := writeTestCBZ(archivePath, map[string][]byte{
		"001.png": png1x1,
		"002.png": png1x1,
	}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("stat archive failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(
		`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
		archivePath,
		info.Size(),
		info.ModTime(),
		2,
		book.ID,
	); err != nil {
		t.Fatalf("update book archive metadata failed: %v", err)
	}
	book, err = store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("get updated book failed: %v", err)
	}

	pages, err := controller.listBookArchivePages(context.Background(), book)
	if err != nil {
		t.Fatalf("first page manifest load failed: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}

	parser.EvictArchiveFromPool(archivePath)
	missingPath := archivePath + ".manifest-missing"
	if err := os.Rename(archivePath, missingPath); err != nil {
		t.Fatalf("rename archive failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Rename(missingPath, archivePath)
	})

	cachedPages, err := controller.listBookArchivePages(context.Background(), book)
	if err != nil {
		t.Fatalf("expected cached page manifest after archive rename, got %v", err)
	}
	if len(cachedPages) != 2 {
		t.Fatalf("expected cached 2 pages, got %d", len(cachedPages))
	}

	book.Size++
	if _, err := controller.listBookArchivePages(context.Background(), book); err == nil {
		t.Fatal("expected changed book source key to bypass manifest cache")
	}
}

func TestServePageImageUsesBookPageSourceCache(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 2)
	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
	if err := writeTestCBZ(archivePath, map[string][]byte{
		"001.png": png1x1,
		"002.png": png1x1,
	}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}
	info, err := os.Stat(archivePath)
	if err != nil {
		t.Fatalf("stat archive failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(
		`UPDATE books SET path = ?, size = ?, file_modified_at = ?, page_count = ? WHERE id = ?`,
		archivePath,
		info.Size(),
		info.ModTime(),
		2,
		book.ID,
	); err != nil {
		t.Fatalf("update book archive metadata failed: %v", err)
	}

	counting := &countingStore{Store: store}
	controller.store = counting

	for page := int64(1); page <= 2; page++ {
		req := requestWithRouteParam(http.MethodGet, "/api/books/page/1/1", nil, "bookId", strconv.FormatInt(book.ID, 10))
		req = withRouteParam(req, "pageNumber", strconv.FormatInt(page, 10))
		rec := httptest.NewRecorder()
		controller.servePageImage(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected page %d response 200, got %d body=%s", page, rec.Code, rec.Body.String())
		}
	}
	if counting.getBookCalls != 1 {
		t.Fatalf("expected one GetBook call for consecutive page image requests, got %d", counting.getBookCalls)
	}
}

func TestPageCacheStatsAndClear(t *testing.T) {
	controller, _, _, rootDir := newTestController(t)
	cfg := controller.currentConfig()
	cfg.Cache.Dir = filepath.Join(rootDir, "processed-cache")
	controller.config.Replace(&cfg)

	cacheDir := controller.processedImageCacheDir()
	if err := os.MkdirAll(filepath.Join(cacheDir, "aa"), 0o755); err != nil {
		t.Fatalf("mkdir page cache failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "aa", "one.webp"), []byte("12345"), 0o644); err != nil {
		t.Fatalf("write page cache file failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "two.jpg"), []byte("123"), 0o644); err != nil {
		t.Fatalf("write page cache file failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.getPageCacheStats(rec, httptest.NewRequest(http.MethodGet, "/api/system/page-cache", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected stats 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var stats pageCacheStatsResponse
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats failed: %v", err)
	}
	if stats.Path != filepath.Clean(cacheDir) {
		t.Fatalf("expected path %q, got %q", filepath.Clean(cacheDir), stats.Path)
	}
	if stats.FileCount != 2 {
		t.Fatalf("expected 2 cache files, got %d", stats.FileCount)
	}
	if stats.FileSize != 8 {
		t.Fatalf("expected 8 cache bytes, got %d", stats.FileSize)
	}

	controller.imageCache.Add("cached-page", []byte("cached"))
	rec = httptest.NewRecorder()
	controller.clearPageCache(rec, httptest.NewRequest(http.MethodDelete, "/api/system/page-cache", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected clear 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if _, ok := controller.imageCache.Get("cached-page"); ok {
		t.Fatal("expected in-memory page cache to be purged")
	}

	remaining, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read cache dir failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected cache dir to be empty after clear, found %d entries", len(remaining))
	}

	rec = httptest.NewRecorder()
	controller.getPageCacheStats(rec, httptest.NewRequest(http.MethodGet, "/api/system/page-cache", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected stats after clear 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode stats after clear failed: %v", err)
	}
	if stats.FileCount != 0 || stats.FileSize != 0 {
		t.Fatalf("expected empty cache stats after clear, got files=%d bytes=%d", stats.FileCount, stats.FileSize)
	}
}

func writeTestCBZ(path string, files map[string][]byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := w.Write(data); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}
