package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

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
