// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"context"
	"database/sql"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
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
	if err := controller.store.(*database.SqlStore).RefreshSeriesStats(context.Background(), series.ID); err != nil {
		t.Fatalf("refresh series stats failed: %v", err)
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
		if feed.Title != "Manga Manager OPDS Catalog" || len(feed.Entries) != 5 {
			t.Fatalf("unexpected root feed payload: %+v", feed)
		}
		if feed.Entries[1].ID != "urn:manga-manager:opds:recent" {
			t.Fatalf("expected recent entry, got %+v", feed.Entries)
		}
		if feed.Entries[2].ID != "urn:manga-manager:opds:continue" {
			t.Fatalf("expected continue reading entry, got %+v", feed.Entries)
		}
		if feed.Entries[3].ID != "urn:manga-manager:opds:collections" {
			t.Fatalf("expected collections entry, got %+v", feed.Entries)
		}
		if feed.Entries[4].ID != "urn:manga-manager:opds:reading-lists" {
			t.Fatalf("expected reading lists entry, got %+v", feed.Entries)
		}
		if len(feed.Links) != 3 || feed.Links[2].Rel != "search" {
			t.Fatalf("expected root feed search link, got %+v", feed.Links)
		}
	})

	t.Run("open search descriptor", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsOpenSearch(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/opensearch.xml", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Content-Type") != "application/opensearchdescription+xml;charset=utf-8" {
			t.Fatalf("unexpected content type: %q", rec.Header().Get("Content-Type"))
		}

		var descriptor OpenSearchDescription
		if err := xml.Unmarshal(rec.Body.Bytes(), &descriptor); err != nil {
			t.Fatalf("decode open search descriptor failed: %v", err)
		}
		if descriptor.ShortName != "Manga Manager" || len(descriptor.URLs) != 1 {
			t.Fatalf("unexpected open search descriptor: %+v", descriptor)
		}
		if descriptor.URLs[0].Template != "/opds/v1.2/search?q={searchTerms}&page={startPage?}&limit={count?}" {
			t.Fatalf("unexpected search template: %+v", descriptor.URLs[0])
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

	t.Run("search feed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsSearch(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/search?q=Display&limit=10", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode search feed failed: %v", err)
		}
		if feed.ID != "urn:manga-manager:opds:search:Display" || len(feed.Entries) != 1 {
			t.Fatalf("unexpected search feed: %+v", feed)
		}
		entry := feed.Entries[0]
		if entry.Title != "Alpha Display" || entry.Content != "Alpha summary" {
			t.Fatalf("unexpected search entry: %+v", entry)
		}
		if len(entry.Links) != 2 || entry.Links[0].Href != "/opds/v1.2/series/"+strconv.FormatInt(series.ID, 10) {
			t.Fatalf("unexpected search links: %+v", entry.Links)
		}
	})

	t.Run("recent added feed", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsRecentAdded(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/recent?limit=5", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode recent feed failed: %v", err)
		}
		if feed.ID != "urn:manga-manager:opds:recent" || len(feed.Entries) != 1 {
			t.Fatalf("unexpected recent feed: %+v", feed)
		}
		if feed.Entries[0].Title != "Alpha Display" || feed.Entries[0].Links[0].Href != "/opds/v1.2/series/"+strconv.FormatInt(series.ID, 10) {
			t.Fatalf("unexpected recent entry: %+v", feed.Entries[0])
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
		if len(entry.Links) != 3 {
			t.Fatalf("expected acquisition + stream + thumbnail links, got %+v", entry.Links)
		}
		stream := findOPDSLink(entry.Links, opdsPSEStreamRel)
		if stream == nil {
			t.Fatalf("expected OPDS-PSE stream link, got %+v", entry.Links)
		}
		if stream.Href != "/opds/v1.2/books/"+strconv.FormatInt(book.ID, 10)+"/pages/{pageNumber}?format=jpeg&w={maxWidth}" {
			t.Fatalf("unexpected stream href: %+v", stream)
		}
		if stream.Type != "image/jpeg" || stream.Count != 12 || !strings.Contains(rec.Body.String(), `pse:count="12"`) {
			t.Fatalf("unexpected stream attributes: %+v", stream)
		}
	})

	t.Run("continue reading feed", func(t *testing.T) {
		if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
			ID:           book.ID,
			LastReadPage: sql.NullInt64{Int64: 6, Valid: true},
			LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		}); err != nil {
			t.Fatalf("UpdateBookProgress failed: %v", err)
		}

		rec := httptest.NewRecorder()
		controller.opdsContinueReading(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/continue?limit=5", nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode continue feed failed: %v", err)
		}
		if feed.ID != "urn:manga-manager:opds:continue" || len(feed.Entries) != 1 {
			t.Fatalf("unexpected continue feed: %+v", feed)
		}
		stream := findOPDSLink(feed.Entries[0].Links, opdsPSEStreamRel)
		if stream == nil || !strings.Contains(rec.Body.String(), `pse:lastRead="6"`) {
			t.Fatalf("expected continue entry stream link with lastRead, got %+v", feed.Entries[0].Links)
		}
	})

	t.Run("reading list feeds", func(t *testing.T) {
		list, err := store.CreateReadingList(context.Background(), database.CreateReadingListParams{
			Name:        "Weekend Queue",
			Description: "planned reading",
		})
		if err != nil {
			t.Fatalf("CreateReadingList failed: %v", err)
		}
		if _, err := store.AddReadingListItem(context.Background(), database.AddReadingListItemParams{
			ReadingListID: list.ID,
			SeriesID:      series.ID,
			Note:          "start here",
		}); err != nil {
			t.Fatalf("AddReadingListItem failed: %v", err)
		}

		listRec := httptest.NewRecorder()
		controller.opdsReadingLists(listRec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/reading-lists", nil))
		if listRec.Code != http.StatusOK {
			t.Fatalf("expected reading lists 200, got %d", listRec.Code)
		}
		var listFeed OPDSFeed
		if err := xml.Unmarshal(listRec.Body.Bytes(), &listFeed); err != nil {
			t.Fatalf("decode reading lists feed failed: %v", err)
		}
		if len(listFeed.Entries) != 1 || listFeed.Entries[0].Links[0].Href != "/opds/v1.2/reading-lists/"+strconv.FormatInt(list.ID, 10) {
			t.Fatalf("unexpected reading lists feed: %+v", listFeed)
		}

		seriesRec := httptest.NewRecorder()
		controller.opdsReadingListSeries(seriesRec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/reading-lists/1", nil, "listId", strconv.FormatInt(list.ID, 10)))
		if seriesRec.Code != http.StatusOK {
			t.Fatalf("expected reading list series 200, got %d body=%s", seriesRec.Code, seriesRec.Body.String())
		}
		var seriesFeed OPDSFeed
		if err := xml.Unmarshal(seriesRec.Body.Bytes(), &seriesFeed); err != nil {
			t.Fatalf("decode reading list series failed: %v", err)
		}
		if seriesFeed.Title != "Weekend Queue" || len(seriesFeed.Entries) != 1 || seriesFeed.Entries[0].Title != "Alpha Display" {
			t.Fatalf("unexpected reading list series feed: %+v", seriesFeed)
		}
	})
}

func TestOPDSRoutesRespectProtocolToggle(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	router := chi.NewRouter()
	controller.SetupOPDSRoutes(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected disabled OPDS route 404, got %d", rec.Code)
	}

	cfg := controller.currentConfig()
	cfg.Protocols.OPDS.Enabled = true
	controller.config.Replace(&cfg)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected enabled OPDS route 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func findOPDSLink(links []OPDSLink, rel string) *OPDSLink {
	for i := range links {
		if links[i].Rel == rel {
			return &links[i]
		}
	}
	return nil
}

func TestOPDSPageStreamingRoute(t *testing.T) {
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
	rec := httptest.NewRecorder()
	req := requestWithRouteParam(http.MethodGet, "/opds/v1.2/books/1/pages/0?format=jpeg&w=320", nil, "bookId", strconv.FormatInt(book.ID, 10))
	req = withRouteParam(req, "pageNumber", "0")
	controller.opdsStreamPageImage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected OPDS stream page 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("expected streamed page to reuse page image ETag")
	}

	rec = httptest.NewRecorder()
	req = requestWithRouteParam(http.MethodGet, "/opds/v1.2/books/1/pages/-1", nil, "bookId", strconv.FormatInt(book.ID, 10))
	req = withRouteParam(req, "pageNumber", "-1")
	controller.opdsStreamPageImage(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected negative OPDS page number 400, got %d", rec.Code)
	}
}

func TestOPDSCollectionFeeds(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, seriesA, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	_, seriesB, _ := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)

	db := controller.store.(*database.SqlStore).DB()
	collectionResult, err := db.ExecContext(context.Background(), `INSERT INTO collections (name, description, source_type) VALUES (?, ?, ?)`, "Manual Picks", "static", "manual")
	if err != nil {
		t.Fatalf("insert collection failed: %v", err)
	}
	collectionID, _ := collectionResult.LastInsertId()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO collection_series (collection_id, series_id) VALUES (?, ?)`, collectionID, seriesA.ID); err != nil {
		t.Fatalf("insert collection series failed: %v", err)
	}

	tag, err := store.UpsertTag(context.Background(), "Action")
	if err != nil {
		t.Fatalf("UpsertTag failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesA.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag A failed: %v", err)
	}
	if err := store.LinkSeriesTag(context.Background(), database.LinkSeriesTagParams{SeriesID: seriesB.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("LinkSeriesTag B failed: %v", err)
	}
	filterResult, err := db.ExecContext(context.Background(), `
		INSERT INTO smart_filters (library_id, name, active_tag, sort_by_field, sort_dir, page_size)
		VALUES (?, ?, ?, ?, ?, ?)
	`, lib.ID, "Action in A", "Action", "name", "asc", 30)
	if err != nil {
		t.Fatalf("insert smart filter failed: %v", err)
	}
	filterID, _ := filterResult.LastInsertId()

	listRec := httptest.NewRecorder()
	controller.opdsCollections(listRec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/collections", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected collections feed 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listFeed OPDSFeed
	if err := xml.Unmarshal(listRec.Body.Bytes(), &listFeed); err != nil {
		t.Fatalf("decode collections feed failed: %v", err)
	}
	if len(listFeed.Entries) != 2 {
		t.Fatalf("expected static and smart collection entries, got %+v", listFeed.Entries)
	}
	if listFeed.Entries[0].Title != "Manual Picks" || listFeed.Entries[0].Links[0].Href != "/opds/v1.2/collections/"+strconv.FormatInt(collectionID, 10) {
		t.Fatalf("unexpected static collection OPDS entry: %+v", listFeed.Entries[0])
	}
	if listFeed.Entries[1].Title != "Action in A" || listFeed.Entries[1].Links[0].Href != "/opds/v1.2/smart-collections/"+strconv.FormatInt(filterID, 10) {
		t.Fatalf("unexpected smart collection OPDS entry: %+v", listFeed.Entries[1])
	}

	staticRec := httptest.NewRecorder()
	controller.opdsStaticCollectionSeries(staticRec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/collections/1", nil, "collectionId", strconv.FormatInt(collectionID, 10)))
	if staticRec.Code != http.StatusOK {
		t.Fatalf("expected static collection series 200, got %d body=%s", staticRec.Code, staticRec.Body.String())
	}
	var staticFeed OPDSFeed
	if err := xml.Unmarshal(staticRec.Body.Bytes(), &staticFeed); err != nil {
		t.Fatalf("decode static collection series failed: %v", err)
	}
	if staticFeed.Title != "Manual Picks" || len(staticFeed.Entries) != 1 || staticFeed.Entries[0].Title != seriesA.Name {
		t.Fatalf("unexpected static collection series feed: %+v", staticFeed)
	}

	smartRec := httptest.NewRecorder()
	controller.opdsSmartCollectionSeries(smartRec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/smart-collections/1", nil, "filterId", strconv.FormatInt(filterID, 10)))
	if smartRec.Code != http.StatusOK {
		t.Fatalf("expected smart collection series 200, got %d body=%s", smartRec.Code, smartRec.Body.String())
	}
	var smartFeed OPDSFeed
	if err := xml.Unmarshal(smartRec.Body.Bytes(), &smartFeed); err != nil {
		t.Fatalf("decode smart collection series failed: %v", err)
	}
	if smartFeed.Title != "Action in A" || len(smartFeed.Entries) != 1 || smartFeed.Entries[0].Title != seriesA.Name {
		t.Fatalf("unexpected smart collection series feed: %+v", smartFeed)
	}
}

func TestOPDSValidationAndEmptyFeeds(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	t.Run("library and series feeds validate route ids", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsLibrarySeries(rec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/libraries/bad", nil, "libraryId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid library id 400, got %d", rec.Code)
		}

		rec = httptest.NewRecorder()
		controller.opdsSeriesBooks(rec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/series/bad", nil, "seriesId", "bad"))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected invalid series id 400, got %d", rec.Code)
		}
	})

	t.Run("library feed can be empty", func(t *testing.T) {
		secondLibPath := filepath.Join(rootDir, "Library Empty")
		if err := os.MkdirAll(secondLibPath, 0o755); err != nil {
			t.Fatalf("mkdir empty library failed: %v", err)
		}
		emptyLib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
			Name:                "Library Empty",
			Path:                secondLibPath,
			ScanMode:            "none",
			KoreaderSyncEnabled: true,
			ScanInterval:        60,
			ScanFormats:         config.DefaultScanFormatsCSV,
		})
		if err != nil {
			t.Fatalf("CreateLibrary empty failed: %v", err)
		}

		rec := httptest.NewRecorder()
		controller.opdsLibrarySeries(rec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/libraries/2", nil, "libraryId", strconv.FormatInt(emptyLib.ID, 10)))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected empty library feed 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode empty library feed failed: %v", err)
		}
		if len(feed.Entries) != 0 {
			t.Fatalf("expected no entries, got %+v", feed.Entries)
		}
	})

	t.Run("series books feed can be empty", func(t *testing.T) {
		db := controller.store.(*database.SqlStore).DB()
		if _, err := db.Exec(`DELETE FROM books WHERE series_id = ?`, series.ID); err != nil {
			t.Fatalf("delete series books failed: %v", err)
		}

		rec := httptest.NewRecorder()
		controller.opdsSeriesBooks(rec, requestWithRouteParam(http.MethodGet, "/opds/v1.2/series/1", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected empty series books feed 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode empty series books feed failed: %v", err)
		}
		if len(feed.Entries) != 0 {
			t.Fatalf("expected no book entries, got %+v", feed.Entries)
		}
		if feed.Title != series.Name {
			t.Fatalf("expected fallback series title %q, got %q", series.Name, feed.Title)
		}
	})

	t.Run("search feed accepts empty query", func(t *testing.T) {
		rec := httptest.NewRecorder()
		controller.opdsSearch(rec, httptest.NewRequest(http.MethodGet, "/opds/v1.2/search?q=", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected empty search 200, got %d", rec.Code)
		}

		var feed OPDSFeed
		if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
			t.Fatalf("decode empty search feed failed: %v", err)
		}
		if len(feed.Entries) != 0 {
			t.Fatalf("expected no empty-query entries, got %+v", feed.Entries)
		}
	})

	_ = lib
}
