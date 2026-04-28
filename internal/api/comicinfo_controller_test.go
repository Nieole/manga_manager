package api

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
)

func TestExportBookComicInfo(t *testing.T) {
	controller, _, book := seedComicInfoFixture(t)

	rec := httptest.NewRecorder()
	controller.exportBookComicInfo(rec, requestWithRouteParam(http.MethodGet, "/api/books/1/comicinfo.xml", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if contentDisposition := rec.Header().Get("Content-Disposition"); !strings.Contains(contentDisposition, "Vol-01--ComicInfo.xml") {
		t.Fatalf("expected sanitized download filename, got %q", contentDisposition)
	}

	var info parser.ComicInfo
	if err := xml.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("unmarshal exported comicinfo failed: %v", err)
	}
	if info.Title != "Book Title" || info.Series != "Display Series" || info.Summary != "Book summary" {
		t.Fatalf("unexpected title/series/summary: %+v", info)
	}
	if info.Number != "1" || info.Volume != "1" || info.Count != 2 || info.PageCount != 188 {
		t.Fatalf("unexpected book fields: %+v", info)
	}
	if info.Publisher != "Publisher" || info.Genre != "冒险" || info.Writer != "Writer A" || info.LanguageISO != "zh" {
		t.Fatalf("unexpected metadata fields: %+v", info)
	}
	if info.CommunityRating != 4.5 {
		t.Fatalf("expected rating 4.5, got %v", info.CommunityRating)
	}
}

func TestExportSeriesComicInfoArchive(t *testing.T) {
	controller, series, _ := seedComicInfoFixture(t)

	rec := httptest.NewRecorder()
	controller.exportSeriesComicInfoArchive(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/comicinfo.zip", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if contentDisposition := rec.Header().Get("Content-Disposition"); !strings.Contains(contentDisposition, "Display Series-ComicInfo.zip") {
		t.Fatalf("expected series archive filename, got %q", contentDisposition)
	}

	reader, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("open zip failed: %v", err)
	}
	if len(reader.File) != 2 {
		t.Fatalf("expected 2 ComicInfo entries, got %d", len(reader.File))
	}
	if reader.File[0].Name != "Vol-01-/ComicInfo.xml" || reader.File[1].Name != "Vol-01--2/ComicInfo.xml" {
		t.Fatalf("unexpected archive entry names: %q %q", reader.File[0].Name, reader.File[1].Name)
	}

	file, err := reader.File[0].Open()
	if err != nil {
		t.Fatalf("open zip entry failed: %v", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read zip entry failed: %v", err)
	}

	var info parser.ComicInfo
	if err := xml.Unmarshal(payload, &info); err != nil {
		t.Fatalf("unmarshal zipped ComicInfo failed: %v", err)
	}
	if info.Series != "Display Series" || info.Count != 2 || info.Writer != "Writer A" {
		t.Fatalf("unexpected zipped ComicInfo: %+v", info)
	}
}

func TestExportBookComicInfoRejectsInvalidBookID(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.exportBookComicInfo(rec, requestWithRouteParam(http.MethodGet, "/api/books/bad/comicinfo.xml", nil, "bookId", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestExportSeriesComicInfoArchiveRejectsInvalidSeriesID(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.exportSeriesComicInfoArchive(rec, requestWithRouteParam(http.MethodGet, "/api/series/bad/comicinfo.zip", nil, "seriesId", "bad"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func seedComicInfoFixture(t *testing.T) (*Controller, database.Series, database.Book) {
	t.Helper()

	controller, store, _, tempDir := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name:                "Library",
		Path:                filepath.Join(tempDir, "Library"),
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "cbz",
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Raw Series",
		Path:        filepath.Join(tempDir, "Library", "Raw Series"),
		NameInitial: database.SeriesInitial("", "Raw Series"),
	})
	if err != nil {
		t.Fatalf("CreateSeries failed: %v", err)
	}
	series, err = store.UpdateSeriesMetadata(ctx, database.UpdateSeriesMetadataParams{
		Title:       sql.NullString{String: "Display Series", Valid: true},
		Summary:     sql.NullString{String: "Series summary", Valid: true},
		Publisher:   sql.NullString{String: "Publisher", Valid: true},
		Rating:      sql.NullFloat64{Float64: 4.5, Valid: true},
		Language:    sql.NullString{String: "zh", Valid: true},
		NameInitial: database.SeriesInitial("Display Series", "Raw Series"),
		ID:          series.ID,
	})
	if err != nil {
		t.Fatalf("UpdateSeriesMetadata failed: %v", err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           `Vol:01?.cbz`,
		Path:           filepath.Join(tempDir, "Library", "Raw Series", "Vol01.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "1",
		Title:          sql.NullString{String: "Book Title", Valid: true},
		Summary:        sql.NullString{String: "Book summary", Valid: true},
		Number:         sql.NullString{String: "1", Valid: true},
		PageCount:      188,
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}
	if _, err := store.CreateBook(ctx, database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           `Vol:01?.cbz`,
		Path:           filepath.Join(tempDir, "Library", "Raw Series", "Vol01-duplicate.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "2",
		PageCount:      166,
	}); err != nil {
		t.Fatalf("CreateBook second failed: %v", err)
	}

	tag, err := store.UpsertTag(ctx, "冒险")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	if err := store.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag failed: %v", err)
	}
	author, err := store.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: "Writer A", Role: "writer"})
	if err != nil {
		t.Fatalf("UpsertAuthor failed: %v", err)
	}
	if err := store.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: author.ID}); err != nil {
		t.Fatalf("LinkSeriesAuthor failed: %v", err)
	}
	return controller, series, book
}
