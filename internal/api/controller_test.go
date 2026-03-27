package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"manga-manager/internal/search"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/v2"
)

func newTestController(t *testing.T) (*Controller, database.Store, *search.Engine, string) {
	t.Helper()

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	configPath := filepath.Join(tempDir, "config.yaml")

	if err := database.Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	engine, err := search.NewEngine(tempDir)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })

	cfg := &config.Config{}
	cfg.Database.Path = dbPath
	cfg.Cache.Dir = filepath.Join(tempDir, "cache")
	cfg.Scanner.ThumbnailFormat = "webp"
	cfg.LLM.Timeout = 30

	cfgManager := config.NewManager(cfg)
	imageCache, _ := lru.New[string, []byte](8)
	scan := scanner.NewScanner(store, engine, cfgManager)

	controller := &Controller{
		store:      store,
		imageCache: imageCache,
		scanner:    scan,
		engine:     engine,
		config:     cfgManager,
		configPath: configPath,
		tasks:      make(map[string]TaskStatus),
		messages:   make(chan string, 32),
	}

	return controller, store, engine, configPath
}

func requestWithRouteParam(method, path string, body []byte, key, value string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if key == "" {
		return req
	}

	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func seedBookFixture(t *testing.T, store database.Store, rootDir, libName, seriesName, bookName string, pageCount int64) (database.Library, database.Series, database.Book) {
	t.Helper()

	libPath := filepath.Join(rootDir, libName)
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir lib path failed: %v", err)
	}

	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:         libName,
		Path:         libPath,
		AutoScan:     false,
		ScanInterval: 60,
		ScanFormats:  "zip,cbz,rar,cbr,pdf",
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}

	seriesPath := filepath.Join(libPath, seriesName)
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series path failed: %v", err)
	}

	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID: lib.ID,
		Name:      seriesName,
		Path:      seriesPath,
	})
	if err != nil {
		t.Fatalf("CreateSeries failed: %v", err)
	}

	book, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           bookName,
		Path:           filepath.Join(seriesPath, bookName),
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "",
		Title:          sql.NullString{String: "Book Title", Valid: true},
		PageCount:      pageCount,
	})
	if err != nil {
		t.Fatalf("CreateBook failed: %v", err)
	}

	return lib, series, book
}

func TestGetAndUpdateSystemConfig(t *testing.T) {
	controller, _, _, configPath := newTestController(t)

	getReq := httptest.NewRequest(http.MethodGet, "/api/system/config", nil)
	getRec := httptest.NewRecorder()
	controller.getSystemConfig(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getRec.Code)
	}

	var got config.Config
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode getSystemConfig failed: %v", err)
	}
	if got.Database.Path == "" {
		t.Fatal("expected database path in config response")
	}

	updated := got
	updated.Server.Port = 9090
	updated.Cache.Dir = "./custom-cache"

	body, err := json.Marshal(updated)
	if err != nil {
		t.Fatalf("marshal config failed: %v", err)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/system/config", bytes.NewReader(body))
	postRec := httptest.NewRecorder()
	controller.updateSystemConfig(postRec, postReq)

	if postRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", postRec.Code)
	}

	snapshot := controller.currentConfig()
	if snapshot.Server.Port != 9090 {
		t.Fatalf("expected updated port 9090, got %d", snapshot.Server.Port)
	}
	if snapshot.Cache.Dir != "./custom-cache" {
		t.Fatalf("expected updated cache dir, got %q", snapshot.Cache.Dir)
	}

	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
}

func TestSearchBooksHandler(t *testing.T) {
	controller, _, engine, _ := newTestController(t)

	t.Run("engine missing", func(t *testing.T) {
		controller.engine = nil
		req := httptest.NewRequest(http.MethodGet, "/api/search?q=test", nil)
		rec := httptest.NewRecorder()

		controller.searchBooks(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rec.Code)
		}
		controller.engine = engine
	})

	t.Run("returns indexed result", func(t *testing.T) {
		book := database.Book{ID: 1, Name: "Alpha Volume 01"}
		if err := controller.engine.IndexBook(book, "Alpha"); err != nil {
			t.Fatalf("IndexBook failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/search?q=Alpha&target=book", nil)
		rec := httptest.NewRecorder()
		controller.searchBooks(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var response struct {
			Hits []any `json:"hits"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode search response failed: %v", err)
		}
		if len(response.Hits) == 0 {
			t.Fatal("expected at least one search hit")
		}
	})
}

func TestCreateAndUpdateLibraryDefaults(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	libPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}

	createPayload := []byte(`{"name":"Main","path":"` + libPath + `"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/libraries", bytes.NewReader(createPayload))
	createRec := httptest.NewRecorder()
	controller.createLibrary(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createRec.Code)
	}

	var created database.Library
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created library failed: %v", err)
	}
	if created.ScanInterval != 60 {
		t.Fatalf("expected default scan interval 60, got %d", created.ScanInterval)
	}
	if created.ScanFormats != "zip,cbz,rar,cbr,pdf" {
		t.Fatalf("unexpected default scan formats: %q", created.ScanFormats)
	}

	updatedPath := filepath.Join(t.TempDir(), "library-updated")
	if err := os.MkdirAll(updatedPath, 0o755); err != nil {
		t.Fatalf("mkdir updated library failed: %v", err)
	}

	updatePayload := []byte(`{"name":"Updated","path":"` + updatedPath + `"}`)
	updateReq := requestWithRouteParam(http.MethodPut, "/api/libraries/1", updatePayload, "libraryId", "1")
	updateRec := httptest.NewRecorder()
	controller.updateLibrary(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateRec.Code)
	}

	lib, err := store.GetLibrary(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetLibrary failed: %v", err)
	}
	if lib.Name != "Updated" {
		t.Fatalf("expected updated library name, got %q", lib.Name)
	}
	if lib.ScanInterval != 60 {
		t.Fatalf("expected defaulted update scan interval 60, got %d", lib.ScanInterval)
	}
}

func TestRebuildIndexKeepsSearchUsable(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	req := httptest.NewRequest(http.MethodPost, "/api/system/rebuild-index", nil)
	rec := httptest.NewRecorder()
	controller.rebuildIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	book := database.Book{ID: 9, Name: "Reindexed Book"}
	if err := controller.engine.IndexBook(book, "Series After Rebuild"); err != nil {
		t.Fatalf("IndexBook after rebuild failed: %v", err)
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/api/search?q=Reindexed&target=book", nil)
	searchRec := httptest.NewRecorder()
	controller.searchBooks(searchRec, searchReq)

	if searchRec.Code != http.StatusOK {
		t.Fatalf("expected search to keep working, got %d", searchRec.Code)
	}
}

func TestListTasksReturnsMostRecentFirst(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if !controller.startTask("older", "scan_library", "older task", 1) {
		t.Fatal("expected first task to start")
	}
	controller.finishTask("older", "done")

	if !controller.startTask("newer", "rebuild_index", "newer task", 1) {
		t.Fatal("expected second task to start")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks", nil)
	rec := httptest.NewRecorder()
	controller.listTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []TaskStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks failed: %v", err)
	}
	if len(tasks) < 2 {
		t.Fatalf("expected at least 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Key != "newer" {
		t.Fatalf("expected most recent task first, got %q", tasks[0].Key)
	}
}

func TestScanLibraryRejectsDuplicateTask(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	libPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}

	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:         "Main",
		Path:         libPath,
		AutoScan:     false,
		ScanInterval: 60,
		ScanFormats:  "zip,cbz,rar,cbr,pdf",
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}

	if !controller.startTask("scan_library_"+strconv.FormatInt(lib.ID, 10), "scan_library", "running", 1) {
		t.Fatal("expected task to start")
	}

	req := requestWithRouteParam(http.MethodPost, "/api/libraries/1/scan", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	controller.scanLibrary(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate scan task, got %d", rec.Code)
	}
}

func TestUpdateBookProgressClampsToPageCount(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 12)

	reqBody := []byte(`{"page":999}`)
	req := requestWithRouteParam(http.MethodPost, "/api/books/1/progress", reqBody, "bookId", strconv.FormatInt(book.ID, 10))
	rec := httptest.NewRecorder()
	controller.updateBookProgress(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	updated, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !updated.LastReadPage.Valid || updated.LastReadPage.Int64 != 12 {
		t.Fatalf("expected clamped page 12, got %+v", updated.LastReadPage)
	}
}

func TestBulkUpdateBookProgressMarksReadAndUnread(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 8)

	readReq := httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress", bytes.NewReader([]byte(`{"book_ids":[`+strconv.FormatInt(book.ID, 10)+`],"is_read":true}`)))
	readRec := httptest.NewRecorder()
	controller.bulkUpdateBookProgress(readRec, readReq)

	if readRec.Code != http.StatusOK {
		t.Fatalf("expected 200 when marking read, got %d", readRec.Code)
	}

	updated, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !updated.LastReadPage.Valid || updated.LastReadPage.Int64 != 8 {
		t.Fatalf("expected last_read_page=8, got %+v", updated.LastReadPage)
	}

	unreadReq := httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress", bytes.NewReader([]byte(`{"book_ids":[`+strconv.FormatInt(book.ID, 10)+`],"is_read":false}`)))
	unreadRec := httptest.NewRecorder()
	controller.bulkUpdateBookProgress(unreadRec, unreadReq)

	if unreadRec.Code != http.StatusOK {
		t.Fatalf("expected 200 when marking unread, got %d", unreadRec.Code)
	}

	updated, err = store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if updated.LastReadPage.Valid {
		t.Fatalf("expected unread book to clear last_read_page, got %+v", updated.LastReadPage)
	}
}

func TestRecentReadHandlersReturnUpdatedBooks(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	lib, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 15)

	progressReq := requestWithRouteParam(http.MethodPost, "/api/books/1/progress", []byte(`{"page":5}`), "bookId", strconv.FormatInt(book.ID, 10))
	progressRec := httptest.NewRecorder()
	controller.updateBookProgress(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", progressRec.Code)
	}

	recentSeriesReq := httptest.NewRequest(http.MethodGet, "/api/series/recent-read?libraryId="+strconv.FormatInt(lib.ID, 10)+"&limit=10", nil)
	recentSeriesRec := httptest.NewRecorder()
	controller.getRecentReadSeries(recentSeriesRec, recentSeriesReq)
	if recentSeriesRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from recent series, got %d", recentSeriesRec.Code)
	}

	var recentSeries struct {
		Items []any `json:"items"`
	}
	if err := json.NewDecoder(recentSeriesRec.Body).Decode(&recentSeries); err != nil {
		t.Fatalf("decode recent series failed: %v", err)
	}
	if len(recentSeries.Items) != 1 {
		t.Fatalf("expected 1 recent series item, got %d", len(recentSeries.Items))
	}

	recentAllReq := httptest.NewRequest(http.MethodGet, "/api/stats/recent-read?limit=10", nil)
	recentAllRec := httptest.NewRecorder()
	controller.getRecentReadAll(recentAllRec, recentAllReq)
	if recentAllRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from recent read all, got %d", recentAllRec.Code)
	}

	var recentAll []any
	if err := json.NewDecoder(recentAllRec.Body).Decode(&recentAll); err != nil {
		t.Fatalf("decode recent read all failed: %v", err)
	}
	if len(recentAll) != 1 {
		t.Fatalf("expected 1 recent read item, got %d", len(recentAll))
	}
}

func TestGetDashboardStatsReflectsReadingProgress(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book1 := seedBookFixture(t, store, t.TempDir(), "LibA", "SeriesA", "book-a.cbz", 12)
	_, _, book2 := seedBookFixture(t, store, t.TempDir(), "LibB", "SeriesB", "book-b.cbz", 8)

	for _, item := range []struct {
		bookID int64
		page   int64
	}{
		{book1.ID, 5},
		{book2.ID, 8},
	} {
		req := requestWithRouteParam(
			http.MethodPost,
			"/api/books/progress",
			[]byte(`{"page":`+strconv.FormatInt(item.page, 10)+`}`),
			"bookId",
			strconv.FormatInt(item.bookID, 10),
		)
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 updating progress, got %d", rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats/dashboard", nil)
	rec := httptest.NewRecorder()
	controller.getDashboardStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var stats struct {
		TotalSeries int `json:"total_series"`
		TotalBooks  int `json:"total_books"`
		ReadBooks   int `json:"read_books"`
		TotalPages  int `json:"total_pages"`
		ActiveDays7 int `json:"active_days_7"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("decode dashboard stats failed: %v", err)
	}

	if stats.TotalSeries != 2 || stats.TotalBooks != 2 {
		t.Fatalf("unexpected totals: %+v", stats)
	}
	if stats.ReadBooks != 2 {
		t.Fatalf("expected 2 read books, got %d", stats.ReadBooks)
	}
	if stats.TotalPages != 20 {
		t.Fatalf("expected 20 total pages, got %d", stats.TotalPages)
	}
	if stats.ActiveDays7 < 1 {
		t.Fatalf("expected active_days_7 >= 1, got %d", stats.ActiveDays7)
	}
}

func TestGetActivityHeatmapReturnsReadingData(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)

	if err := store.LogReadingActivity(context.Background(), book.ID, 7); err != nil {
		t.Fatalf("LogReadingActivity failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats/activity-heatmap?weeks=1", nil)
	rec := httptest.NewRecorder()
	controller.getActivityHeatmap(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var days []struct {
		Date      string `json:"date"`
		PageCount int    `json:"page_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&days); err != nil {
		t.Fatalf("decode heatmap failed: %v", err)
	}

	if len(days) == 0 {
		t.Fatal("expected at least one activity day")
	}
	if days[len(days)-1].PageCount != 7 {
		t.Fatalf("expected latest activity page count 7, got %d", days[len(days)-1].PageCount)
	}
}

func TestGetRecentReadAllHonorsLimit(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	for i, pages := range []int64{3, 4} {
		_, _, book := seedBookFixture(
			t,
			store,
			t.TempDir(),
			"Lib"+strconv.Itoa(i),
			"Series"+strconv.Itoa(i),
			"book-"+strconv.Itoa(i)+".cbz",
			10,
		)
		req := requestWithRouteParam(
			http.MethodPost,
			"/api/books/progress",
			[]byte(`{"page":`+strconv.FormatInt(pages, 10)+`}`),
			"bookId",
			strconv.FormatInt(book.ID, 10),
		)
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 updating progress, got %d", rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stats/recent-read?limit=1", nil)
	rec := httptest.NewRecorder()
	controller.getRecentReadAll(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var recent []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&recent); err != nil {
		t.Fatalf("decode recent read all failed: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("expected exactly 1 recent read row, got %d", len(recent))
	}
}

func TestApplyScrapedMetadataPersistsSeriesTagsAndLink(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)

	payload := []byte(`{
		"Title":"Updated Title",
		"Summary":"Updated summary",
		"Publisher":"Kodansha",
		"Rating":8.6,
		"Tags":["Action","Drama"],
		"SourceID":12345
	}`)

	req := requestWithRouteParam(
		http.MethodPost,
		"/api/series/1/scrape-apply?provider=bangumi",
		payload,
		"seriesId",
		strconv.FormatInt(series.ID, 10),
	)
	req.URL.RawQuery = "provider=bangumi"
	rec := httptest.NewRecorder()
	controller.applyScrapedMetadata(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	updated, err := store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries failed: %v", err)
	}
	if !updated.Title.Valid || updated.Title.String != "Updated Title" {
		t.Fatalf("expected updated title, got %+v", updated.Title)
	}
	if !updated.Publisher.Valid || updated.Publisher.String != "Kodansha" {
		t.Fatalf("expected updated publisher, got %+v", updated.Publisher)
	}

	tags, err := store.GetTagsForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetTagsForSeries failed: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	links, err := store.GetLinksForSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetLinksForSeries failed: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 source link, got %d", len(links))
	}
	if links[0].Url != "https://bgm.tv/subject/12345" {
		t.Fatalf("unexpected source link: %s", links[0].Url)
	}
}

func TestGetRecommendationsReturnsCachedEntries(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	controller.recommendationsMutex.Lock()
	controller.recommendationsCache = []AIRecommendationResponse{{
		SeriesID:  99,
		Reason:    "Cached reason",
		Title:     "Cached title",
		CoverPath: "cached.webp",
	}}
	controller.recommendationsCacheTime = time.Now()
	controller.recommendationsMutex.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/stats/recommendations", nil)
	rec := httptest.NewRecorder()
	controller.getRecommendations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var recommendations []AIRecommendationResponse
	if err := json.NewDecoder(rec.Body).Decode(&recommendations); err != nil {
		t.Fatalf("decode recommendations failed: %v", err)
	}
	if len(recommendations) != 1 {
		t.Fatalf("expected cached recommendation, got %d items", len(recommendations))
	}
	if recommendations[0].SeriesID != 99 {
		t.Fatalf("unexpected recommendation payload: %+v", recommendations[0])
	}
}

func TestTestLLMConfigReturnsErrorForInvalidEndpoint(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/system/test-llm",
		bytes.NewReader([]byte(`{"provider":"ollama","endpoint":"http://127.0.0.1:1","model":"fake","prompt":"ping"}`)),
	)
	rec := httptest.NewRecorder()
	controller.testLLMConfig(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestScrapeSeriesMetadataReturnsErrorForInvalidLLMEndpoint(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)

	cfg := controller.currentConfig()
	cfg.LLM.Provider = "ollama"
	cfg.LLM.Endpoint = "http://127.0.0.1:1"
	cfg.LLM.Model = "fake"
	controller.config.Replace(&cfg)

	req := requestWithRouteParam(
		http.MethodPost,
		"/api/series/1/scrape",
		[]byte(`{"provider":"llm"}`),
		"seriesId",
		strconv.FormatInt(series.ID, 10),
	)
	rec := httptest.NewRecorder()
	controller.scrapeSeriesMetadata(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}
