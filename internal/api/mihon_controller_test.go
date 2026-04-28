package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestMihonAPILifecycle(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	series, err := store.UpdateSeriesMetadata(context.Background(), database.UpdateSeriesMetadataParams{
		Title:       sql.NullString{String: "Display Alpha", Valid: true},
		Summary:     sql.NullString{String: "Summary Alpha", Valid: true},
		Status:      sql.NullString{String: "ONGOING", Valid: true},
		NameInitial: database.SeriesInitial("Display Alpha", series.Name),
		ID:          series.ID,
	})
	if err != nil {
		t.Fatalf("UpdateSeriesMetadata failed: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 3, Valid: true},
		ID:           book.ID,
	}); err != nil {
		t.Fatalf("UpdateBookProgress failed: %v", err)
	}
	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
	if err := writeTestCBZ(archivePath, map[string][]byte{
		"001.png": png1x1,
		"002.png": png1x1,
	}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}

	librariesRec := httptest.NewRecorder()
	controller.mihonLibraries(librariesRec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/libraries", nil))
	if librariesRec.Code != http.StatusOK {
		t.Fatalf("expected libraries 200, got %d", librariesRec.Code)
	}
	var libraries []MihonLibraryResponse
	if err := json.NewDecoder(librariesRec.Body).Decode(&libraries); err != nil {
		t.Fatalf("decode libraries failed: %v", err)
	}
	if len(libraries) != 1 || libraries[0].ID != lib.ID {
		t.Fatalf("unexpected libraries payload: %+v", libraries)
	}

	seriesRec := httptest.NewRecorder()
	controller.mihonSeries(seriesRec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/series?libraryId="+strconv.FormatInt(lib.ID, 10)+"&q=alpha&page=1&limit=10", nil))
	if seriesRec.Code != http.StatusOK {
		t.Fatalf("expected series 200, got %d", seriesRec.Code)
	}
	var seriesPage MihonSeriesPageResponse
	if err := json.NewDecoder(seriesRec.Body).Decode(&seriesPage); err != nil {
		t.Fatalf("decode series page failed: %v", err)
	}
	if seriesPage.Total != 1 || len(seriesPage.Items) != 1 || seriesPage.Items[0].Title != "Display Alpha" {
		t.Fatalf("unexpected series page: %+v", seriesPage)
	}

	detailRec := httptest.NewRecorder()
	controller.mihonSeriesDetail(detailRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/series/1", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if detailRec.Code != http.StatusOK {
		t.Fatalf("expected detail 200, got %d", detailRec.Code)
	}
	var detail MihonSeriesResponse
	if err := json.NewDecoder(detailRec.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail failed: %v", err)
	}
	if detail.ID != series.ID || detail.Summary != "Summary Alpha" || detail.Status != "ONGOING" {
		t.Fatalf("unexpected detail payload: %+v", detail)
	}

	booksRec := httptest.NewRecorder()
	controller.mihonSeriesBooks(booksRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/series/1/books", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected books 200, got %d", booksRec.Code)
	}
	var books []MihonBookResponse
	if err := json.NewDecoder(booksRec.Body).Decode(&books); err != nil {
		t.Fatalf("decode books failed: %v", err)
	}
	if len(books) != 1 || books[0].ID != book.ID || books[0].LastReadPage != 3 {
		t.Fatalf("unexpected books payload: %+v", books)
	}

	pagesRec := httptest.NewRecorder()
	controller.mihonBookPages(pagesRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/books/1/pages?format=webp&q=80", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if pagesRec.Code != http.StatusOK {
		t.Fatalf("expected pages 200, got %d", pagesRec.Code)
	}
	var pages []MihonPageResponse
	if err := json.NewDecoder(pagesRec.Body).Decode(&pages); err != nil {
		t.Fatalf("decode pages failed: %v", err)
	}
	if len(pages) != 2 || pages[0].ImageURL != "/api/mihon/v1/books/"+strconv.FormatInt(book.ID, 10)+"/pages/1?format=webp&q=80" || pages[0].MediaType != "image/png" {
		t.Fatalf("unexpected pages payload: %+v", pages)
	}
}

func TestMihonAPIRejectsInvalidIDs(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	seriesRec := httptest.NewRecorder()
	controller.mihonSeriesDetail(seriesRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/series/bad", nil, "seriesId", "bad"))
	if seriesRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid series 400, got %d", seriesRec.Code)
	}

	pagesRec := httptest.NewRecorder()
	controller.mihonBookPages(pagesRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/books/bad/pages", nil, "bookId", "bad"))
	if pagesRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid book 400, got %d", pagesRec.Code)
	}
}
