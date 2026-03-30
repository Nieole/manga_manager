package api

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestOPDSFeeds(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	if _, err := controller.store.(*database.SqlStore).DB().Exec(`
		UPDATE series SET title = ?, summary = ? WHERE id = ?;
	`, "Alpha Display", "Alpha summary", series.ID); err != nil {
		t.Fatalf("update series metadata failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(`
		UPDATE books SET title = ?, cover_path = ? WHERE id = ?;
	`, "Alpha Book Display", "covers/alpha.jpg", book.ID); err != nil {
		t.Fatalf("update book metadata failed: %v", err)
	}

	t.Run("root feed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsRoot(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/atom+xml;charset=utf-8" {
			t.Fatalf("unexpected content type: %q", rec.Header().Get("Content-Type"))
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode root feed failed: %v", err)
		}
		if feed.Title != "Manga Manager OPDS Catalog" || len(feed.Entries) != 1 {
			t.Fatalf("unexpected root feed payload: %+v", feed)
		}
	})

	t.Run("libraries feed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsLibraries(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/libraries", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode libraries feed failed: %v", err)
		}
		if len(feed.Entries) != 1 || feed.Entries[0].Title != lib.Name {
			t.Fatalf("unexpected libraries feed: %+v", feed.Entries)
		}
	})

	t.Run("library series feed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsLibrarySeries(rec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode library series feed failed: %v", err)
		}
		if len(feed.Entries) != 1 {
			t.Fatalf("expected 1 series entry, got %d", len(feed.Entries))
		}
		entry := feed.Entries[0]
		if entry.Title != "Alpha Display" || entry.Content != "Alpha summary" {
			t.Fatalf("unexpected series entry: %+v", entry)
		}
		if len(entry.Links) != 2 || entry.Links[0].Href != "/opds/v1.2/series/"+strconv.FormatInt(series.ID, 10) {
			t.Fatalf("unexpected series links: %+v", entry.Links)
		}
	})

	t.Run("series books feed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsSeriesBooks(rec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/series/1", nil, "seriesId", strconv.FormatInt(series.ID, 10)))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode series books feed failed: %v", err)
		}
		if feed.Title != "Alpha Display" || len(feed.Entries) != 1 {
			t.Fatalf("unexpected books feed: %+v", feed)
		}
		entry := feed.Entries[0]
		if entry.Title != "Alpha Book Display" {
			t.Fatalf("unexpected book title: %+v", entry)
		}
		if len(entry.Links) != 2 {
			t.Fatalf("expected acquisition + thumbnail links, got %+v", entry.Links)
		}
	})
}
