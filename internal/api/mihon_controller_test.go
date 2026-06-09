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
	"time"

	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
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
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
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

	recentRec := httptest.NewRecorder()
	controller.mihonRecentlyAdded(recentRec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/recently-added?libraryId="+strconv.FormatInt(lib.ID, 10)+"&page=1&limit=10", nil))
	if recentRec.Code != http.StatusOK {
		t.Fatalf("expected recently added 200, got %d", recentRec.Code)
	}
	var recentPage MihonSeriesPageResponse
	if err := json.NewDecoder(recentRec.Body).Decode(&recentPage); err != nil {
		t.Fatalf("decode recently added page failed: %v", err)
	}
	if recentPage.Total != 1 || len(recentPage.Items) != 1 || recentPage.Items[0].ID != series.ID {
		t.Fatalf("unexpected recently added page: %+v", recentPage)
	}

	continueRec := httptest.NewRecorder()
	controller.mihonContinueReading(continueRec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/continue?limit=10", nil))
	if continueRec.Code != http.StatusOK {
		t.Fatalf("expected continue 200, got %d", continueRec.Code)
	}
	var continueItems []MihonContinueItemResponse
	if err := json.NewDecoder(continueRec.Body).Decode(&continueItems); err != nil {
		t.Fatalf("decode continue items failed: %v", err)
	}
	if len(continueItems) != 1 || continueItems[0].BookID != book.ID || continueItems[0].LastReadPage != 3 {
		t.Fatalf("unexpected continue payload: %+v", continueItems)
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

func TestMihonRoutesRespectProtocolToggle(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	router := chi.NewRouter()
	router.Route("/api", func(r chi.Router) {
		controller.setupMihonRoutes(r)
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/libraries", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected disabled Mihon route 404, got %d", rec.Code)
	}

	cfg := controller.currentConfig()
	cfg.Protocols.Mihon.Enabled = true
	controller.config.Replace(&cfg)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/libraries", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected enabled Mihon route 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMihonReadingListEndpoints(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
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
	controller.mihonReadingLists(listRec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/reading-lists", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected reading lists 200, got %d", listRec.Code)
	}
	var lists []MihonReadingListResponse
	if err := json.NewDecoder(listRec.Body).Decode(&lists); err != nil {
		t.Fatalf("decode reading lists failed: %v", err)
	}
	if len(lists) != 1 || lists[0].ID != list.ID || lists[0].ItemCount != 1 {
		t.Fatalf("unexpected reading lists payload: %+v", lists)
	}

	seriesRec := httptest.NewRecorder()
	controller.mihonReadingListSeries(seriesRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/reading-lists/1/series", nil, "listId", strconv.FormatInt(list.ID, 10)))
	if seriesRec.Code != http.StatusOK {
		t.Fatalf("expected reading list series 200, got %d body=%s", seriesRec.Code, seriesRec.Body.String())
	}
	var page MihonSeriesPageResponse
	if err := json.NewDecoder(seriesRec.Body).Decode(&page); err != nil {
		t.Fatalf("decode reading list series failed: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != series.ID {
		t.Fatalf("unexpected reading list series page: %+v", page)
	}
}

func TestMihonCollectionEndpoints(t *testing.T) {
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
	controller.mihonCollections(listRec, httptest.NewRequest(http.MethodGet, "/api/mihon/v1/collections", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected collections 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var collections []MihonCollectionResponse
	if err := json.NewDecoder(listRec.Body).Decode(&collections); err != nil {
		t.Fatalf("decode collections failed: %v", err)
	}
	if len(collections) != 2 {
		t.Fatalf("expected 2 collections, got %+v", collections)
	}
	if collections[0].ID != "collection:"+strconv.FormatInt(collectionID, 10) || collections[0].SeriesCount != 1 {
		t.Fatalf("unexpected static collection payload: %+v", collections[0])
	}
	if collections[1].ID != "smart:"+strconv.FormatInt(filterID, 10) || collections[1].SeriesCount != 1 || collections[1].LibraryID == nil || *collections[1].LibraryID != lib.ID {
		t.Fatalf("unexpected smart collection payload: %+v", collections[1])
	}

	staticRec := httptest.NewRecorder()
	controller.mihonCollectionSeries(staticRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/collections/1/series", nil, "collectionId", strconv.FormatInt(collectionID, 10)))
	if staticRec.Code != http.StatusOK {
		t.Fatalf("expected static collection series 200, got %d body=%s", staticRec.Code, staticRec.Body.String())
	}
	var staticPage MihonSeriesPageResponse
	if err := json.NewDecoder(staticRec.Body).Decode(&staticPage); err != nil {
		t.Fatalf("decode static collection series failed: %v", err)
	}
	if staticPage.Total != 1 || len(staticPage.Items) != 1 || staticPage.Items[0].ID != seriesA.ID {
		t.Fatalf("unexpected static collection series page: %+v", staticPage)
	}

	smartRec := httptest.NewRecorder()
	controller.mihonSmartCollectionSeries(smartRec, requestWithRouteParam(http.MethodGet, "/api/mihon/v1/smart-collections/1/series", nil, "filterId", strconv.FormatInt(filterID, 10)))
	if smartRec.Code != http.StatusOK {
		t.Fatalf("expected smart collection series 200, got %d body=%s", smartRec.Code, smartRec.Body.String())
	}
	var smartPage MihonSeriesPageResponse
	if err := json.NewDecoder(smartRec.Body).Decode(&smartPage); err != nil {
		t.Fatalf("decode smart collection series failed: %v", err)
	}
	if smartPage.Total != 1 || len(smartPage.Items) != 1 || smartPage.Items[0].ID != seriesA.ID {
		t.Fatalf("unexpected smart collection series page: %+v", smartPage)
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
