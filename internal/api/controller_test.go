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
	cfg.Scanner.ArchivePoolSize = 5
	cfg.Scanner.MaxAiConcurrency = 3
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "qwen2.5"
	cfg.LLM.Timeout = 30
	config.NormalizeConfig(cfg)
	if err := os.MkdirAll(cfg.Cache.Dir, 0o755); err != nil {
		t.Fatalf("mkdir cache dir failed: %v", err)
	}

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
		ScanFormats:  config.DefaultScanFormatsCSV,
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

	var got SystemConfigResponse
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode getSystemConfig failed: %v", err)
	}
	if got.Config.Database.Path == "" {
		t.Fatal("expected database path in config response")
	}
	if got.Capabilities.DefaultScanFormats != config.DefaultScanFormatsCSV {
		t.Fatalf("expected default scan formats %q, got %q", config.DefaultScanFormatsCSV, got.Capabilities.DefaultScanFormats)
	}

	updated := got.Config
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

func TestUpdateSystemConfigRejectsInvalidConfiguration(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	payload := []byte(`{
		"server":{"port":0},
		"database":{"path":""},
		"cache":{"dir":"/definitely/missing/cache"},
		"scanner":{"workers":-1,"thumbnail_format":"gif","archive_pool_size":0,"max_ai_concurrency":0},
		"llm":{"provider":"openai","api_mode":"","base_url":"","request_path":"","model":"","timeout":5}
	}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/system/config", bytes.NewReader(payload))
	controller.updateSystemConfig(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Validation config.ValidationResult `json:"validation"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode validation failed: %v", err)
	}
	if body.Validation.Valid {
		t.Fatal("expected invalid validation result")
	}
	if len(body.Validation.Issues) == 0 {
		t.Fatal("expected validation issues")
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
	if created.ScanFormats != config.DefaultScanFormatsCSV {
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

func TestUpdateSeriesInfoAndGetSeriesContext(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
	if err := writeTestCBZ(archivePath, map[string][]byte{
		"001.png": png1x1,
	}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}

	payload := []byte(`{
		"title":"Alpha Display",
		"summary":"Updated summary",
		"publisher":"Test Publisher",
		"status":"ongoing",
		"rating":8.6,
		"language":"ja",
		"locked_fields":"title,summary",
		"tags":["Action","Drama",""],
		"authors":[
			{"name":"Writer A","role":"story"},
			{"name":"Artist B","role":"art"},
			{"name":"","role":"ignore"}
		],
		"links":[
			{"name":"Bangumi","url":"https://bgm.tv/subject/1"},
			{"name":"","url":"https://invalid.example"}
		]
	}`)

	updateReq := requestWithRouteParam(http.MethodPut, "/api/series/1", payload, "seriesId", strconv.FormatInt(series.ID, 10))
	updateRec := httptest.NewRecorder()
	controller.updateSeriesInfo(updateRec, updateReq)

	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update series 200, got %d", updateRec.Code)
	}

	infoRec := httptest.NewRecorder()
	controller.getSeriesInfo(infoRec, requestWithRouteParam(http.MethodGet, "/api/series/1", nil, "seriesId", strconv.FormatInt(series.ID, 10)))

	if infoRec.Code != http.StatusOK {
		t.Fatalf("expected get series info 200, got %d", infoRec.Code)
	}

	var info database.Series
	if err := json.NewDecoder(infoRec.Body).Decode(&info); err != nil {
		t.Fatalf("decode series info failed: %v", err)
	}
	if !info.Title.Valid || info.Title.String != "Alpha Display" {
		t.Fatalf("expected updated title, got %+v", info.Title)
	}
	if !info.Summary.Valid || info.Summary.String != "Updated summary" {
		t.Fatalf("expected updated summary, got %+v", info.Summary)
	}
	if !info.Publisher.Valid || info.Publisher.String != "Test Publisher" {
		t.Fatalf("expected updated publisher, got %+v", info.Publisher)
	}

	tagsRec := httptest.NewRecorder()
	controller.getSeriesTags(tagsRec, requestWithRouteParam(http.MethodGet, "/api/series/1/tags", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if tagsRec.Code != http.StatusOK {
		t.Fatalf("expected get series tags 200, got %d", tagsRec.Code)
	}
	var tags []database.Tag
	if err := json.NewDecoder(tagsRec.Body).Decode(&tags); err != nil {
		t.Fatalf("decode series tags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	authorsRec := httptest.NewRecorder()
	controller.getSeriesAuthors(authorsRec, requestWithRouteParam(http.MethodGet, "/api/series/1/authors", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if authorsRec.Code != http.StatusOK {
		t.Fatalf("expected get series authors 200, got %d", authorsRec.Code)
	}
	var authors []database.Author
	if err := json.NewDecoder(authorsRec.Body).Decode(&authors); err != nil {
		t.Fatalf("decode series authors failed: %v", err)
	}
	if len(authors) != 2 {
		t.Fatalf("expected 2 authors, got %d", len(authors))
	}

	linksRec := httptest.NewRecorder()
	controller.getSeriesLinks(linksRec, requestWithRouteParam(http.MethodGet, "/api/series/1/links", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if linksRec.Code != http.StatusOK {
		t.Fatalf("expected get series links 200, got %d", linksRec.Code)
	}
	var links []database.SeriesLink
	if err := json.NewDecoder(linksRec.Body).Decode(&links); err != nil {
		t.Fatalf("decode series links failed: %v", err)
	}
	if len(links) != 1 || links[0].Name != "Bangumi" {
		t.Fatalf("unexpected series links: %+v", links)
	}

	booksRec := httptest.NewRecorder()
	controller.getBooksBySeries(booksRec, requestWithRouteParam(http.MethodGet, "/api/series/1/books", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if booksRec.Code != http.StatusOK {
		t.Fatalf("expected get books by series 200, got %d", booksRec.Code)
	}
	var books []database.Book
	if err := json.NewDecoder(booksRec.Body).Decode(&books); err != nil {
		t.Fatalf("decode books by series failed: %v", err)
	}
	if len(books) != 1 || books[0].ID != book.ID {
		t.Fatalf("unexpected books response: %+v", books)
	}

	contextRec := httptest.NewRecorder()
	controller.getSeriesContext(contextRec, requestWithRouteParam(http.MethodGet, "/api/series/1/context", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if contextRec.Code != http.StatusOK {
		t.Fatalf("expected get series context 200, got %d", contextRec.Code)
	}

	var seriesContext SeriesContextResponse
	if err := json.NewDecoder(contextRec.Body).Decode(&seriesContext); err != nil {
		t.Fatalf("decode series context failed: %v", err)
	}
	if seriesContext.Series.ID != series.ID || len(seriesContext.Books) != 1 || len(seriesContext.Tags) != 2 || len(seriesContext.Authors) != 2 || len(seriesContext.Links) != 1 {
		t.Fatalf("unexpected series context payload: %+v", seriesContext)
	}
}

func TestMetadataLookupValidationHandlers(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	providersRec := httptest.NewRecorder()
	controller.listProviders(providersRec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if providersRec.Code != http.StatusOK {
		t.Fatalf("expected list providers 200, got %d", providersRec.Code)
	}
	var providers []map[string]string
	if err := json.NewDecoder(providersRec.Body).Decode(&providers); err != nil {
		t.Fatalf("decode providers failed: %v", err)
	}
	if len(providers) != 2 || providers[0]["id"] != "bangumi" {
		t.Fatalf("unexpected providers payload: %+v", providers)
	}

	searchRec := httptest.NewRecorder()
	controller.searchMetadata(searchRec, httptest.NewRequest(http.MethodGet, "/api/metadata/search", nil))
	if searchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing q to return 400, got %d", searchRec.Code)
	}

	scrapeSearchRec := httptest.NewRecorder()
	controller.scrapeSearchMetadata(scrapeSearchRec, requestWithRouteParam(http.MethodGet, "/api/series/invalid/scrape/search", nil, "seriesId", "invalid"))
	if scrapeSearchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid series id to return 400, got %d", scrapeSearchRec.Code)
	}

	notFoundRec := httptest.NewRecorder()
	controller.scrapeSearchMetadata(notFoundRec, requestWithRouteParam(http.MethodGet, "/api/series/999/scrape/search", nil, "seriesId", "999"))
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing series to return 404, got %d", notFoundRec.Code)
	}

	applyRec := httptest.NewRecorder()
	controller.applyScrapedMetadata(applyRec, requestWithRouteParam(http.MethodPost, "/api/series/1/scrape/apply", []byte("{"), "seriesId", "1"))
	if applyRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid payload to return 400, got %d", applyRec.Code)
	}
}

func TestLibraryAndSeriesReadEndpoints(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	libsRec := httptest.NewRecorder()
	controller.getLibraries(libsRec, httptest.NewRequest(http.MethodGet, "/api/libraries", nil))
	if libsRec.Code != http.StatusOK {
		t.Fatalf("expected get libraries 200, got %d", libsRec.Code)
	}
	var libs []database.Library
	if err := json.NewDecoder(libsRec.Body).Decode(&libs); err != nil {
		t.Fatalf("decode libraries failed: %v", err)
	}
	if len(libs) != 1 || libs[0].ID != lib.ID {
		t.Fatalf("unexpected libraries payload: %+v", libs)
	}

	seriesRec := httptest.NewRecorder()
	controller.getSeriesByLibrary(seriesRec, requestWithRouteParam(http.MethodGet, "/api/libraries/1/series", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if seriesRec.Code != http.StatusOK {
		t.Fatalf("expected get series by library 200, got %d", seriesRec.Code)
	}
	var seriesRows []database.ListSeriesByLibraryRow
	if err := json.NewDecoder(seriesRec.Body).Decode(&seriesRows); err != nil {
		t.Fatalf("decode series by library failed: %v", err)
	}
	if len(seriesRows) != 1 || seriesRows[0].ID != series.ID {
		t.Fatalf("unexpected series rows: %+v", seriesRows)
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/api/series/search?libraryId="+strconv.FormatInt(lib.ID, 10)+"&limit=5&page=1", nil)
	searchRec := httptest.NewRecorder()
	controller.searchSeriesPaged(searchRec, searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("expected search series paged 200, got %d", searchRec.Code)
	}
	var searchResp struct {
		Items []database.SearchSeriesPagedRow `json:"items"`
		Total int                             `json:"total"`
		Page  int                             `json:"page"`
		Limit int                             `json:"limit"`
	}
	if err := json.NewDecoder(searchRec.Body).Decode(&searchResp); err != nil {
		t.Fatalf("decode search series response failed: %v", err)
	}
	if searchResp.Total != 1 || len(searchResp.Items) != 1 || searchResp.Items[0].ID != series.ID {
		t.Fatalf("unexpected search response: %+v", searchResp)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/api/series/search?libraryId=bad", nil)
	invalidRec := httptest.NewRecorder()
	controller.searchSeriesPaged(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid library id 400, got %d", invalidRec.Code)
	}
}

func TestGlobalMetadataAndBookReadEndpoints(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	secondBook, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Alpha 02.cbz",
		Path:           filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 02.cbz"),
		Size:           2048,
		FileModifiedAt: time.Now(),
		Volume:         "",
		Title:          sql.NullString{String: "Second Book", Valid: true},
		SortNumber:     sql.NullFloat64{Float64: 2, Valid: true},
		PageCount:      20,
	})
	if err != nil {
		t.Fatalf("CreateBook second failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE books SET sort_number = ? WHERE id = ?`, 1, firstBook.ID); err != nil {
		t.Fatalf("update first book sort number failed: %v", err)
	}

	updatePayload := []byte(`{
		"title":"Alpha Display",
		"tags":["Action","Mystery"],
		"authors":[{"name":"Writer A","role":"story"}],
		"links":[{"name":"Bangumi","url":"https://bgm.tv/subject/1"}]
	}`)
	updateReq := requestWithRouteParam(http.MethodPut, "/api/series/1", updatePayload, "seriesId", strconv.FormatInt(series.ID, 10))
	updateRec := httptest.NewRecorder()
	controller.updateSeriesInfo(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("expected update series 200, got %d", updateRec.Code)
	}

	allTagsRec := httptest.NewRecorder()
	controller.getAllTags(allTagsRec, httptest.NewRequest(http.MethodGet, "/api/tags", nil))
	if allTagsRec.Code != http.StatusOK {
		t.Fatalf("expected get all tags 200, got %d", allTagsRec.Code)
	}
	var tags []database.Tag
	if err := json.NewDecoder(allTagsRec.Body).Decode(&tags); err != nil {
		t.Fatalf("decode all tags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}

	allAuthorsRec := httptest.NewRecorder()
	controller.getAllAuthors(allAuthorsRec, httptest.NewRequest(http.MethodGet, "/api/authors", nil))
	if allAuthorsRec.Code != http.StatusOK {
		t.Fatalf("expected get all authors 200, got %d", allAuthorsRec.Code)
	}
	var authors []database.Author
	if err := json.NewDecoder(allAuthorsRec.Body).Decode(&authors); err != nil {
		t.Fatalf("decode all authors failed: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "Writer A" {
		t.Fatalf("unexpected authors payload: %+v", authors)
	}

	bookInfoRec := httptest.NewRecorder()
	controller.getBookInfo(bookInfoRec, requestWithRouteParam(http.MethodGet, "/api/books/1", nil, "bookId", strconv.FormatInt(firstBook.ID, 10)))
	if bookInfoRec.Code != http.StatusOK {
		t.Fatalf("expected get book info 200, got %d", bookInfoRec.Code)
	}
	var gotBook database.Book
	if err := json.NewDecoder(bookInfoRec.Body).Decode(&gotBook); err != nil {
		t.Fatalf("decode book info failed: %v", err)
	}
	if gotBook.ID != firstBook.ID {
		t.Fatalf("unexpected book info payload: %+v", gotBook)
	}

	nextRec := httptest.NewRecorder()
	controller.getNextBook(nextRec, requestWithRouteParam(http.MethodGet, "/api/books/1/next", nil, "bookId", strconv.FormatInt(firstBook.ID, 10)))
	if nextRec.Code != http.StatusOK {
		t.Fatalf("expected get next book 200, got %d", nextRec.Code)
	}
	var nextBook database.Book
	if err := json.NewDecoder(nextRec.Body).Decode(&nextBook); err != nil {
		t.Fatalf("decode next book failed: %v", err)
	}
	if nextBook.ID != secondBook.ID {
		t.Fatalf("expected second book as next, got %+v", nextBook)
	}

	notFoundNextRec := httptest.NewRecorder()
	controller.getNextBook(notFoundNextRec, requestWithRouteParam(http.MethodGet, "/api/books/2/next", nil, "bookId", strconv.FormatInt(secondBook.ID, 10)))
	if notFoundNextRec.Code != http.StatusNotFound {
		t.Fatalf("expected no next book 404, got %d", notFoundNextRec.Code)
	}
}

func TestBulkUpdateSeriesAndGetPagesByBook(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	favorite := true
	bulkReq := httptest.NewRequest(http.MethodPost, "/api/series/bulk", bytes.NewBufferString(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`],"is_favorite":true}`))
	bulkRec := httptest.NewRecorder()
	controller.bulkUpdateSeries(bulkRec, bulkReq)
	if bulkRec.Code != http.StatusOK {
		t.Fatalf("expected bulk update series 200, got %d", bulkRec.Code)
	}
	updatedSeries, err := store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries after bulk update failed: %v", err)
	}
	if updatedSeries.IsFavorite != favorite {
		t.Fatalf("expected favorite=true after bulk update, got %v", updatedSeries.IsFavorite)
	}

	noopRec := httptest.NewRecorder()
	controller.bulkUpdateSeries(noopRec, httptest.NewRequest(http.MethodPost, "/api/series/bulk", bytes.NewBufferString(`{"series_ids":[]}`)))
	if noopRec.Code != http.StatusOK {
		t.Fatalf("expected empty bulk update 200, got %d", noopRec.Code)
	}

	pagesInvalidRec := httptest.NewRecorder()
	controller.getPagesByBook(pagesInvalidRec, requestWithRouteParam(http.MethodGet, "/api/books/page-list/bad", nil, "bookId", "bad"))
	if pagesInvalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid book id 400, got %d", pagesInvalidRec.Code)
	}

	pagesMissingRec := httptest.NewRecorder()
	controller.getPagesByBook(pagesMissingRec, requestWithRouteParam(http.MethodGet, "/api/books/page-list/999", nil, "bookId", "999"))
	if pagesMissingRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing book 404, got %d", pagesMissingRec.Code)
	}

	pagesErrorRec := httptest.NewRecorder()
	controller.getPagesByBook(pagesErrorRec, requestWithRouteParam(http.MethodGet, "/api/books/page-list/1", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if pagesErrorRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected invalid archive 500, got %d", pagesErrorRec.Code)
	}

	archivePath := filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha 01.cbz")
	if err := writeTestCBZ(archivePath, map[string][]byte{
		"001.png": png1x1,
		"002.png": png1x1,
	}); err != nil {
		t.Fatalf("write test cbz failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE books SET path = ? WHERE id = ?`, archivePath, book.ID); err != nil {
		t.Fatalf("update book archive path failed: %v", err)
	}

	pagesRec := httptest.NewRecorder()
	controller.getPagesByBook(pagesRec, requestWithRouteParam(http.MethodGet, "/api/books/page-list/1", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if pagesRec.Code != http.StatusOK {
		t.Fatalf("expected get pages 200, got %d", pagesRec.Code)
	}
	var pages []struct {
		Number int64  `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.NewDecoder(pagesRec.Body).Decode(&pages); err != nil {
		t.Fatalf("decode pages response failed: %v", err)
	}
	if len(pages) != 2 || pages[0].Number != 1 || pages[0].URL == "" {
		t.Fatalf("unexpected pages payload: %+v", pages)
	}
}

func TestDeleteLibraryAndValidationHandlers(t *testing.T) {
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
		ScanFormats:  config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}

	invalidDeleteRec := httptest.NewRecorder()
	controller.deleteLibrary(invalidDeleteRec, requestWithRouteParam(http.MethodDelete, "/api/libraries/bad", nil, "libraryId", "bad"))
	if invalidDeleteRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid delete library 400, got %d", invalidDeleteRec.Code)
	}

	deleteRec := httptest.NewRecorder()
	controller.deleteLibrary(deleteRec, requestWithRouteParam(http.MethodDelete, "/api/libraries/1", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete library 200, got %d", deleteRec.Code)
	}

	if _, err := store.GetLibrary(context.Background(), lib.ID); err == nil {
		t.Fatal("expected deleted library lookup to fail")
	}
}

func TestTaskConflictHandlers(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	if !controller.startTask("scan_series_"+strconv.FormatInt(series.ID, 10), "scan_series", "running", 1) {
		t.Fatal("expected scan series task to start")
	}
	scanSeriesRec := httptest.NewRecorder()
	controller.scanSeries(scanSeriesRec, requestWithRouteParam(http.MethodPost, "/api/series/1/scan", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if scanSeriesRec.Code != http.StatusConflict {
		t.Fatalf("expected duplicate scan series 409, got %d", scanSeriesRec.Code)
	}

	if !controller.startTask("cleanup_library_"+strconv.FormatInt(lib.ID, 10), "cleanup_library", "running", 1) {
		t.Fatal("expected cleanup task to start")
	}
	cleanupRec := httptest.NewRecorder()
	controller.cleanupLibrary(cleanupRec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/cleanup", nil, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if cleanupRec.Code != http.StatusConflict {
		t.Fatalf("expected duplicate cleanup 409, got %d", cleanupRec.Code)
	}

	invalidScanRec := httptest.NewRecorder()
	controller.scanSeries(invalidScanRec, requestWithRouteParam(http.MethodPost, "/api/series/bad/scan", nil, "seriesId", "bad"))
	if invalidScanRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid scan series 400, got %d", invalidScanRec.Code)
	}

	invalidCleanupRec := httptest.NewRecorder()
	controller.cleanupLibrary(invalidCleanupRec, requestWithRouteParam(http.MethodPost, "/api/libraries/bad/cleanup", nil, "libraryId", "bad"))
	if invalidCleanupRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid cleanup 400, got %d", invalidCleanupRec.Code)
	}
}

func TestRecentReadValidationAndBrowseDirs(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	missingLibraryRec := httptest.NewRecorder()
	controller.getRecentReadSeries(missingLibraryRec, httptest.NewRequest(http.MethodGet, "/api/series/recent", nil))
	if missingLibraryRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing libraryId 400, got %d", missingLibraryRec.Code)
	}

	invalidLibraryRec := httptest.NewRecorder()
	controller.getRecentReadSeries(invalidLibraryRec, httptest.NewRequest(http.MethodGet, "/api/series/recent?libraryId=bad", nil))
	if invalidLibraryRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid libraryId 400, got %d", invalidLibraryRec.Code)
	}

	rootDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootDir, "Beta"), 0o755); err != nil {
		t.Fatalf("mkdir Beta failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha failed: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rootDir, ".hidden"), 0o755); err != nil {
		t.Fatalf("mkdir hidden failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "file.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}

	browseInvalidRec := httptest.NewRecorder()
	controller.browseDirs(browseInvalidRec, httptest.NewRequest(http.MethodGet, "/api/browse?path=/definitely/missing", nil))
	if browseInvalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid browse path 400, got %d", browseInvalidRec.Code)
	}

	browseReq := httptest.NewRequest(http.MethodGet, "/api/browse?path="+rootDir, nil)
	browseRec := httptest.NewRecorder()
	controller.browseDirs(browseRec, browseReq)
	if browseRec.Code != http.StatusOK {
		t.Fatalf("expected browse dirs 200, got %d", browseRec.Code)
	}

	var result struct {
		Current string `json:"current"`
		Parent  string `json:"parent"`
		Dirs    []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"dirs"`
	}
	if err := json.NewDecoder(browseRec.Body).Decode(&result); err != nil {
		t.Fatalf("decode browse result failed: %v", err)
	}
	if result.Current != rootDir {
		t.Fatalf("expected current dir %q, got %q", rootDir, result.Current)
	}
	if len(result.Dirs) != 2 {
		t.Fatalf("expected 2 visible dirs, got %+v", result.Dirs)
	}
	if result.Dirs[0].Name != "alpha" || result.Dirs[1].Name != "Beta" {
		t.Fatalf("expected case-insensitive sorting and hidden dir filtering, got %+v", result.Dirs)
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

func TestListTasksSupportsStatusFilter(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if !controller.startTask("failed_one", "scan_library", "failed task", 1) {
		t.Fatal("expected failed task to start")
	}
	controller.failTask("failed_one", "boom")

	if !controller.startTask("completed_one", "rebuild_index", "completed task", 1) {
		t.Fatal("expected completed task to start")
	}
	controller.finishTask("completed_one", "done")

	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks?status=failed", nil)
	rec := httptest.NewRecorder()
	controller.listTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []TaskStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode filtered tasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Key != "failed_one" {
		t.Fatalf("expected only failed task, got %+v", tasks)
	}
}

func TestListTasksSupportsScopeIDFilter(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if !controller.startTask("scan_series_12", "scan_series", "series 12", 1) {
		t.Fatal("expected task to start")
	}
	controller.finishTask("scan_series_12", "done")

	if !controller.startTask("scan_series_18", "scan_series", "series 18", 1) {
		t.Fatal("expected task to start")
	}
	controller.finishTask("scan_series_18", "done")

	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks?scope=series&scope_id=18", nil)
	rec := httptest.NewRecorder()
	controller.listTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var tasks []TaskStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode filtered tasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Key != "scan_series_18" {
		t.Fatalf("expected only series 18 task, got %+v", tasks)
	}
}

func TestRetryTaskRestartsRetryableTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if !controller.startTask("scan_series_999", "scan_series", "failed series scan", 1) {
		t.Fatal("expected task to start")
	}
	controller.failTask("scan_series_999", "failed")

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/scan_series_999/retry", nil, "taskKey", "scan_series_999")
	rec := httptest.NewRecorder()
	controller.retryTask(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	time.Sleep(20 * time.Millisecond)
	controller.taskMutex.Lock()
	task := controller.tasks["scan_series_999"]
	controller.taskMutex.Unlock()
	if task.Status == "running" {
		t.Fatalf("expected retried task to finish quickly, got %+v", task)
	}
	if task.Message == "failed" {
		t.Fatalf("expected retried task to update message, got %+v", task)
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
		ScanFormats:  config.DefaultScanFormatsCSV,
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
		bytes.NewReader([]byte(`{"provider":"ollama","base_url":"http://127.0.0.1:1","model":"fake","prompt":"ping"}`)),
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
	cfg.LLM.BaseURL = "http://127.0.0.1:1"
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
