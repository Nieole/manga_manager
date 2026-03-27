package api

import (
	"bytes"
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
