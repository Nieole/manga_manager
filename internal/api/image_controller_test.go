package api

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/database"
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

	_ = series
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
