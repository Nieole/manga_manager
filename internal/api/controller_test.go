// 业务说明：本文件是业务回归测试，属于后端 HTTP API 层，负责把前端请求转换为数据库、扫描器、图片处理和元数据服务调用。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/external"
	"manga-manager/internal/koreader"
	"manga-manager/internal/metadata"
	"manga-manager/internal/parser"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"

	"github.com/go-chi/chi/v5"
	lru "github.com/hashicorp/golang-lru/v2"
)

func newTestController(t testing.TB) (*Controller, database.Store, any, string) {
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
	pageCache, _ := lru.New[string, []parser.PageMetadata](8)
	bookPageSourceCache, _ := lru.New[int64, cachedBookPageSource](8)
	progressWriteCache, _ := lru.New[int64, cachedProgressWrite](8)
	parser.InitPool(cfg.Scanner.ArchivePoolSize)
	scan := scanner.NewScanner(store, cfgManager)

	controller := &Controller{
		store:               store,
		imageCache:          imageCache,
		pageCache:           pageCache,
		bookPageSourceCache: bookPageSourceCache,
		progressWriteCache:  progressWriteCache,
		scanner:             scan,
		config:              cfgManager,
		koreader:            koreader.NewService(store, cfgManager),
		external:            external.NewManager(store, 30*time.Minute),
		configPath:          configPath,
		taskEngine:          newTaskEngine(),
		messages:            make(chan string, 32),
	}
	// 与 NewController 一致：构建任务重试注册表，否则新建任务的 Retryable 恒为 false。
	controller.taskEngine.relaunchers = controller.buildTaskRelaunchers()

	t.Cleanup(parser.ResetArchivePool)
	t.Cleanup(controller.Close)

	return controller, store, nil, tempDir
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

func requestWithRouteParams(method, path string, body []byte, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	routeCtx := chi.NewRouteContext()
	for key, value := range params {
		routeCtx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

type countingStore struct {
	database.Store
	getBookCalls            int
	getDashboardStatsCalls  int
	structuralStatsCalls    int
	volatileStatsCalls      int
	updateBookProgressCalls int
	logReadingActivityCalls int
}

func (s *countingStore) GetBook(ctx context.Context, id int64) (database.Book, error) {
	s.getBookCalls++
	return s.Store.GetBook(ctx, id)
}

func (s *countingStore) GetDashboardStats(ctx context.Context) (*database.DashboardStats, error) {
	s.getDashboardStatsCalls++
	return s.Store.GetDashboardStats(ctx)
}

func (s *countingStore) GetDashboardStructuralStats(ctx context.Context) (*database.DashboardStructuralStats, error) {
	s.structuralStatsCalls++
	return s.Store.GetDashboardStructuralStats(ctx)
}

func (s *countingStore) GetDashboardVolatileStats(ctx context.Context) (*database.DashboardVolatileStats, error) {
	s.volatileStatsCalls++
	return s.Store.GetDashboardVolatileStats(ctx)
}

func (s *countingStore) UpdateBookProgress(ctx context.Context, arg database.UpdateBookProgressParams) error {
	s.updateBookProgressCalls++
	return s.Store.UpdateBookProgress(ctx, arg)
}

func (s *countingStore) LogReadingActivity(ctx context.Context, arg database.LogReadingActivityParams) error {
	s.logReadingActivityCalls++
	return s.Store.LogReadingActivity(ctx, arg)
}

type blockingMetadataProvider struct {
	requests chan string
	release  chan struct{}
}

func newBlockingMetadataProvider() *blockingMetadataProvider {
	return &blockingMetadataProvider{
		requests: make(chan string, 8),
		release:  make(chan struct{}),
	}
}

func (p *blockingMetadataProvider) Name() string {
	return "TestProvider"
}

func (p *blockingMetadataProvider) FetchSeriesMetadata(ctx context.Context, title string) (*metadata.SeriesMetadata, error) {
	select {
	case p.requests <- title:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &metadata.SeriesMetadata{
		Title:      title + " scraped",
		Summary:    "summary " + title,
		Provider:   p.Name(),
		Confidence: 0.95,
	}, nil
}

func (p *blockingMetadataProvider) SearchMetadata(ctx context.Context, title string, limit, offset int) ([]*metadata.SeriesMetadata, int, error) {
	result, err := p.FetchSeriesMetadata(ctx, title)
	if err != nil || result == nil {
		return nil, 0, err
	}
	return []*metadata.SeriesMetadata{result}, 1, nil
}

func createTestKOReaderAccount(t *testing.T, controller *Controller, username string) KOReaderAccountResponse {
	t.Helper()

	reqBody := []byte(`{"username":"` + username + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader/accounts", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.createKOReaderAccount(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected account create 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp KOReaderAccountResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode KOReader account response failed: %v", err)
	}
	return resp
}

func seedBookFixture(t testing.TB, store database.Store, rootDir, libName, seriesName, bookName string, pageCount int64) (database.Library, database.Series, database.Book) {
	t.Helper()

	libPath := filepath.Join(rootDir, libName)
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir lib path failed: %v", err)
	}

	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                libName,
		Path:                libPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}

	seriesPath := filepath.Join(libPath, seriesName)
	if err := os.MkdirAll(seriesPath, 0o755); err != nil {
		t.Fatalf("mkdir series path failed: %v", err)
	}

	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        seriesName,
		Path:        seriesPath,
		NameInitial: database.SeriesInitial("", seriesName),
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
	controller, _, _, _ := newTestController(t)

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
	if len(got.Capabilities.SupportedLogLevels) != len(config.SupportedLogLevels) {
		t.Fatalf("expected supported log levels %+v, got %+v", config.SupportedLogLevels, got.Capabilities.SupportedLogLevels)
	}

	updated := got.Config
	updated.Server.Port = 9090
	updated.Cache.Dir = "./custom-cache"
	updated.Logging.Level = config.LogLevelDebug
	updated.Protocols.OPDS.Enabled = true
	updated.Protocols.Mihon.Enabled = true

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
	if snapshot.Logging.Level != config.LogLevelDebug {
		t.Fatalf("expected updated log level %q, got %q", config.LogLevelDebug, snapshot.Logging.Level)
	}
	if !snapshot.Protocols.OPDS.Enabled || !snapshot.Protocols.Mihon.Enabled {
		t.Fatalf("expected protocol toggles to persist, got OPDS=%v Mihon=%v", snapshot.Protocols.OPDS.Enabled, snapshot.Protocols.Mihon.Enabled)
	}

	if _, err := os.Stat(controller.configPath); err != nil {
		t.Fatalf("expected config file to be written: %v", err)
	}
}

func TestUpdateKOReaderSettings(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	t.Cleanup(func() { _ = store.Close() })

	reqBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "file_path",
		"path_ignore_extension": true
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.updateKOReaderSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp KOReaderSystemResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if !resp.Enabled || resp.AccountCount != 0 || resp.MatchMode != config.KOReaderMatchModeFilePath || !resp.PathIgnoreExtension {
		t.Fatalf("unexpected KOReader response: %+v", resp)
	}
	accounts, err := store.ListKOReaderAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListKOReaderAccounts failed: %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("expected no KOReader accounts yet, got %d", len(accounts))
	}
}

func TestCreateKOReaderAccount(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	account := createTestKOReaderAccount(t, controller, "reader")
	if account.Username != "reader" || account.SyncKey == "" || !account.Enabled {
		t.Fatalf("unexpected account payload: %+v", account)
	}
}

func TestGeneratedKOReaderAccountCanAuthenticateThroughClientHashedHeader(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	settingsReq := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader([]byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "binary_hash",
		"path_ignore_extension": false
	}`)))
	settingsRec := httptest.NewRecorder()
	controller.updateKOReaderSettings(settingsRec, settingsReq)
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", settingsRec.Code, settingsRec.Body.String())
	}

	account := createTestKOReaderAccount(t, controller, "reader")
	authReq := httptest.NewRequest(http.MethodGet, "/koreader/users/auth", nil)
	authReq.Header.Set("x-auth-user", account.Username)
	authReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	authRec := httptest.NewRecorder()
	controller.koreaderAuth(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("expected auth 200, got %d body=%s", authRec.Code, authRec.Body.String())
	}
}

func TestRotateAndToggleKOReaderAccount(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	account := createTestKOReaderAccount(t, controller, "reader")

	toggleReq := httptest.NewRequest(http.MethodPost, "/api/system/koreader/accounts/1/toggle", bytes.NewReader([]byte(`{"enabled":false}`)))
	toggleReq = requestWithRouteParam(http.MethodPost, "/api/system/koreader/accounts/1/toggle", []byte(`{"enabled":false}`), "accountId", strconv.FormatInt(account.ID, 10))
	toggleRec := httptest.NewRecorder()
	controller.toggleKOReaderAccount(toggleRec, toggleReq)
	if toggleRec.Code != http.StatusOK {
		t.Fatalf("expected toggle 200, got %d body=%s", toggleRec.Code, toggleRec.Body.String())
	}

	rotateReq := requestWithRouteParam(http.MethodPost, "/api/system/koreader/accounts/1/rotate-key", nil, "accountId", strconv.FormatInt(account.ID, 10))
	rotateRec := httptest.NewRecorder()
	controller.rotateKOReaderAccountKey(rotateRec, rotateReq)
	if rotateRec.Code != http.StatusOK {
		t.Fatalf("expected rotate 200, got %d body=%s", rotateRec.Code, rotateRec.Body.String())
	}

	var rotated KOReaderAccountResponse
	if err := json.NewDecoder(rotateRec.Body).Decode(&rotated); err != nil {
		t.Fatalf("decode rotated account failed: %v", err)
	}
	if rotated.SyncKey == "" || rotated.SyncKey == account.SyncKey {
		t.Fatalf("expected rotated sync key, got %+v", rotated)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/system/koreader/accounts", nil)
	listRec := httptest.NewRecorder()
	controller.listKOReaderAccounts(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected account list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}

	var accounts []KOReaderAccountResponse
	if err := json.NewDecoder(listRec.Body).Decode(&accounts); err != nil {
		t.Fatalf("decode account list failed: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if accounts[0].LatestError != "" {
		t.Fatalf("expected no latest error after rotate/toggle system events, got %+v", accounts[0])
	}
}

func TestKOReaderAuthAndProgressSyncBinaryHash(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	reqBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "binary_hash",
		"path_ignore_extension": false
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.updateKOReaderSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d", rec.Code)
	}
	account := createTestKOReaderAccount(t, controller, "reader")

	root := t.TempDir()
	lib, series, book := seedBookFixture(t, store, root, "Library", "Series", "Book.cbz", 120)
	_ = series
	if err := os.WriteFile(book.Path, []byte("book-content-for-koreader-sync"), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}

	fileHash, err := koreader.FingerprintFile(book.Path)
	if err != nil {
		t.Fatalf("FingerprintFile failed: %v", err)
	}
	if err := store.UpdateBookIdentity(context.Background(), database.UpdateBookIdentityParams{
		ID:                   book.ID,
		FileHash:             fileHash,
		PathFingerprint:      koreader.FingerprintRelativePath(lib.Path, book.Path, false),
		PathFingerprintNoExt: koreader.FingerprintRelativePath(lib.Path, book.Path, true),
	}); err != nil {
		t.Fatalf("UpdateBookIdentity failed: %v", err)
	}

	authReq := httptest.NewRequest(http.MethodGet, "/koreader/users/auth", nil)
	authReq.Header.Set("x-auth-user", account.Username)
	authReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	authRec := httptest.NewRecorder()
	controller.koreaderAuth(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("expected auth 200, got %d body=%s", authRec.Code, authRec.Body.String())
	}
	if got := authRec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected application/json content type, got %q", got)
	}

	progressPayload := []byte(`{
		"document":"` + fileHash + `",
		"progress":"epubcfi(/6/4!/4/10)",
		"percentage":0.5,
		"device":"Boox",
		"device_id":"DEVICE-1"
	}`)
	progressReq := httptest.NewRequest(http.MethodPut, "/koreader/syncs/progress", bytes.NewReader(progressPayload))
	progressReq.Header.Set("x-auth-user", account.Username)
	progressReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	progressRec := httptest.NewRecorder()
	controller.koreaderUpdateProgress(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected progress update 200, got %d body=%s", progressRec.Code, progressRec.Body.String())
	}

	updatedBook, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !updatedBook.LastReadPage.Valid || updatedBook.LastReadPage.Int64 != 60 {
		t.Fatalf("expected projected page 60, got %+v", updatedBook.LastReadPage)
	}

	getReq := requestWithRouteParam(http.MethodGet, "/koreader/syncs/progress/doc", nil, "document", fileHash)
	getReq.Header.Set("x-auth-user", account.Username)
	getReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	getRec := httptest.NewRecorder()
	controller.koreaderGetProgress(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected progress fetch 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}

	var got map[string]interface{}
	if err := json.NewDecoder(getRec.Body).Decode(&got); err != nil {
		t.Fatalf("decode progress response failed: %v", err)
	}
	if got["document"] != fileHash {
		t.Fatalf("unexpected document payload: %+v", got)
	}
}

func TestKOReaderAuthSupportsVendorJSON(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	reqBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "binary_hash",
		"path_ignore_extension": false
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.updateKOReaderSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	account := createTestKOReaderAccount(t, controller, "reader")

	authReq := httptest.NewRequest(http.MethodGet, "/koreader/users/auth", nil)
	authReq.Header.Set("Accept", "application/vnd.koreader.v1+json")
	authReq.Header.Set("x-auth-user", account.Username)
	authReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	authRec := httptest.NewRecorder()
	controller.koreaderAuth(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("expected auth 200, got %d body=%s", authRec.Code, authRec.Body.String())
	}
	if got := authRec.Header().Get("Content-Type"); got != "application/vnd.koreader.v1+json" {
		t.Fatalf("unexpected content type %q", got)
	}

	var body map[string]string
	if err := json.NewDecoder(authRec.Body).Decode(&body); err != nil {
		t.Fatalf("decode auth response failed: %v", err)
	}
	if body["state"] != "OK" || body["authorized"] != "OK" {
		t.Fatalf("unexpected auth response body: %+v", body)
	}
}

func TestKOReaderAuthAndProgressSyncFilePath(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	reqBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "file_path",
		"path_ignore_extension": false
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.updateKOReaderSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	account := createTestKOReaderAccount(t, controller, "reader")

	root := t.TempDir()
	lib, _, book := seedBookFixture(t, store, root, "Library", filepath.Join("Series", "Volume"), "Book.cbz", 100)
	if err := os.WriteFile(book.Path, []byte("book-content-for-koreader-sync-path"), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}

	fileHash, err := koreader.FingerprintFile(book.Path)
	if err != nil {
		t.Fatalf("FingerprintFile failed: %v", err)
	}
	if err := store.UpdateBookIdentity(context.Background(), database.UpdateBookIdentityParams{
		ID:                   book.ID,
		FileHash:             fileHash,
		PathFingerprint:      koreader.FingerprintRelativePath(lib.Path, book.Path, false),
		PathFingerprintNoExt: koreader.FingerprintRelativePath(lib.Path, book.Path, true),
	}); err != nil {
		t.Fatalf("UpdateBookIdentity failed: %v", err)
	}

	progressPayload := []byte(`{
		"document":"/mnt/other/Series/Volume/Book.cbz",
		"progress":"p-1",
		"percentage":0.4,
		"device":"Boox",
		"device_id":"DEVICE-2"
	}`)
	progressReq := httptest.NewRequest(http.MethodPut, "/koreader/syncs/progress", bytes.NewReader(progressPayload))
	progressReq.Header.Set("x-auth-user", account.Username)
	progressReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	progressRec := httptest.NewRecorder()
	controller.koreaderUpdateProgress(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected progress update 200, got %d body=%s", progressRec.Code, progressRec.Body.String())
	}

	updatedBook, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !updatedBook.LastReadPage.Valid || updatedBook.LastReadPage.Int64 != 40 {
		t.Fatalf("expected projected page 40, got %+v", updatedBook.LastReadPage)
	}
}

func TestKOReaderFilePathIgnoreExtension(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	reqBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "file_path",
		"path_ignore_extension": true
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.updateKOReaderSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	account := createTestKOReaderAccount(t, controller, "reader")

	root := t.TempDir()
	lib, _, book := seedBookFixture(t, store, root, "Library", filepath.Join("Series", "Volume"), "Book.cbz", 80)
	if err := os.WriteFile(book.Path, []byte("book-content-for-koreader-sync-path-ext"), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}

	fileHash, err := koreader.FingerprintFile(book.Path)
	if err != nil {
		t.Fatalf("FingerprintFile failed: %v", err)
	}
	if err := store.UpdateBookIdentity(context.Background(), database.UpdateBookIdentityParams{
		ID:                   book.ID,
		FileHash:             fileHash,
		PathFingerprint:      koreader.FingerprintRelativePath(lib.Path, book.Path, false),
		PathFingerprintNoExt: koreader.FingerprintRelativePath(lib.Path, book.Path, true),
	}); err != nil {
		t.Fatalf("UpdateBookIdentity failed: %v", err)
	}

	progressPayload := []byte(`{
		"document":"/different/root/Series/Volume/Book.zip",
		"progress":"p-2",
		"percentage":0.25,
		"device":"Boox",
		"device_id":"DEVICE-3"
	}`)
	progressReq := httptest.NewRequest(http.MethodPut, "/koreader/syncs/progress", bytes.NewReader(progressPayload))
	progressReq.Header.Set("x-auth-user", account.Username)
	progressReq.Header.Set("x-auth-key", koreader.HashKey(account.SyncKey))
	progressRec := httptest.NewRecorder()
	controller.koreaderUpdateProgress(progressRec, progressReq)
	if progressRec.Code != http.StatusOK {
		t.Fatalf("expected progress update 200, got %d body=%s", progressRec.Code, progressRec.Body.String())
	}

	updatedBook, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !updatedBook.LastReadPage.Valid || updatedBook.LastReadPage.Int64 != 20 {
		t.Fatalf("expected projected page 20, got %+v", updatedBook.LastReadPage)
	}
}

func TestKOReaderUnmatchedListAndApplyMatching(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	reqBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "file_path",
		"path_ignore_extension": false
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	controller.updateKOReaderSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	account := createTestKOReaderAccount(t, controller, "reader")

	_, err := store.UpsertKOReaderProgress(context.Background(), database.UpsertKOReaderProgressParams{
		Username:   account.Username,
		Document:   "/mnt/koreader/Unknown/Vol1/Book.cbz",
		Progress:   "p-x",
		Percentage: 0.15,
		Device:     "Boox",
		DeviceID:   "DEVICE-X",
		Timestamp:  time.Now().Unix(),
		MatchedBy:  "",
		RawPayload: `{"document":"unknown"}`,
	})
	if err != nil {
		t.Fatalf("UpsertKOReaderProgress failed: %v", err)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/system/koreader/unmatched?limit=10", nil)
	listRec := httptest.NewRecorder()
	controller.listKOReaderUnmatched(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected unmatched list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}

	var items []KOReaderUnmatchedItem
	if err := json.NewDecoder(listRec.Body).Decode(&items); err != nil {
		t.Fatalf("decode unmatched list failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 unmatched item, got %d", len(items))
	}
	if items[0].NormalizedKey == "" {
		t.Fatalf("expected normalized key in unmatched item: %+v", items[0])
	}

	applyReq := httptest.NewRequest(http.MethodPost, "/api/system/koreader/apply-matching", nil)
	applyRec := httptest.NewRecorder()
	controller.applyKOReaderMatching(applyRec, applyReq)
	if applyRec.Code != http.StatusAccepted {
		t.Fatalf("expected apply matching 202, got %d body=%s", applyRec.Code, applyRec.Body.String())
	}
}

func TestKOReaderDeviceDiagnostics(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	account := createTestKOReaderAccount(t, controller, "reader")

	if _, err := store.UpsertKOReaderProgress(context.Background(), database.UpsertKOReaderProgressParams{
		Username:   account.Username,
		Document:   "matched-document",
		Progress:   "p-1",
		Percentage: 0.75,
		Device:     "Boox",
		DeviceID:   "DEVICE-X",
		BookID:     sql.NullInt64{Int64: book.ID, Valid: true},
		MatchedBy:  "binary_hash",
		Timestamp:  time.Now().Unix(),
		RawPayload: `{"document":"matched-document"}`,
	}); err != nil {
		t.Fatalf("Upsert matched KOReaderProgress failed: %v", err)
	}
	if _, err := store.UpsertKOReaderProgress(context.Background(), database.UpsertKOReaderProgressParams{
		Username:   account.Username,
		Document:   "/mnt/koreader/Unknown/Vol1/Book.cbz",
		Progress:   "p-x",
		Percentage: 0.15,
		Device:     "Boox",
		DeviceID:   "DEVICE-X",
		MatchedBy:  "",
		Timestamp:  time.Now().Unix(),
		RawPayload: `{"document":"unknown"}`,
	}); err != nil {
		t.Fatalf("Upsert unmatched KOReaderProgress failed: %v", err)
	}
	if err := store.CreateKOReaderSyncEvent(context.Background(), database.CreateKOReaderSyncEventParams{
		Direction: "auth",
		Username:  account.Username,
		Document:  "/mnt/koreader/Unknown/Vol1/Book.cbz",
		Status:    "auth_failed_invalid_key",
		Message:   "Unauthorized",
	}); err != nil {
		t.Fatalf("CreateKOReaderSyncEvent failed: %v", err)
	}
	if _, err := store.ListKOReaderDeviceDiagnostics(context.Background()); err != nil {
		t.Fatalf("ListKOReaderDeviceDiagnostics failed: %v", err)
	}
	if _, err := store.ListKOReaderDeviceMatchMethods(context.Background()); err != nil {
		t.Fatalf("ListKOReaderDeviceMatchMethods failed: %v", err)
	}
	if _, err := store.ListKOReaderDeviceConflicts(context.Background(), 10); err != nil {
		t.Fatalf("ListKOReaderDeviceConflicts failed: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.getKOReaderDeviceDiagnostics(rec, httptest.NewRequest(http.MethodGet, "/api/system/koreader/devices?limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected device diagnostics 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp KOReaderDeviceDiagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode device diagnostics failed: %v", err)
	}
	if resp.Summary.DeviceCount != 1 || resp.Summary.UnmatchedRecords != 1 || resp.Summary.ConflictCount != 2 {
		t.Fatalf("unexpected diagnostics summary: %+v", resp.Summary)
	}
	if len(resp.Devices) != 1 || resp.Devices[0].Health != "error" || len(resp.Devices[0].MatchMethods) != 2 {
		t.Fatalf("unexpected device diagnostics: %+v", resp.Devices)
	}
	if len(resp.Conflicts) != 2 || resp.Conflicts[0].Suggestion == "" {
		t.Fatalf("unexpected conflict diagnostics: %+v", resp.Conflicts)
	}
}

func TestGetStorageIODiagnostics(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	cfg := controller.currentConfig()
	cfg.Cache.Dir = filepath.Join(rootDir, "cache")
	cfg.Library.StorageProfile = config.StorageProfileHDDExternal
	config.NormalizeConfig(&cfg)
	controller.config.Replace(&cfg)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/system/storage-io", nil)
	controller.getStorageIODiagnostics(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var response StorageIODiagnosticsResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode diagnostics failed: %v", err)
	}
	if len(response.Libraries) == 0 {
		t.Fatalf("expected library diagnostics, got %+v", response)
	}
	found := false
	for _, item := range response.Libraries {
		if item.ID != lib.ID {
			continue
		}
		found = true
		if item.StorageProfile != config.StorageProfileHDDExternal {
			t.Fatalf("expected external HDD profile, got %+v", item)
		}
		if item.HeavyBackgroundConcurrency != 1 || !item.CacheOnSameVolume || !item.DisableSameDiskPageCache {
			t.Fatalf("unexpected external HDD diagnostics: %+v", item)
		}
	}
	if !found {
		t.Fatalf("expected diagnostics for library %d, got %+v", lib.ID, response.Libraries)
	}
}

func TestPauseAndResumeStorageIO(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	storageio.Default.ResumeBackground()
	t.Cleanup(storageio.Default.ResumeBackground)

	pauseRec := httptest.NewRecorder()
	controller.pauseStorageIO(pauseRec, httptest.NewRequest(http.MethodPost, "/api/system/storage-io/pause", nil))
	if pauseRec.Code != http.StatusAccepted {
		t.Fatalf("expected pause 202, got %d", pauseRec.Code)
	}

	getRec := httptest.NewRecorder()
	controller.getStorageIODiagnostics(getRec, httptest.NewRequest(http.MethodGet, "/api/system/storage-io", nil))
	var response StorageIODiagnosticsResponse
	if err := json.NewDecoder(getRec.Body).Decode(&response); err != nil {
		t.Fatalf("decode diagnostics failed: %v", err)
	}
	if !response.Paused {
		t.Fatalf("expected paused diagnostics, got %+v", response)
	}

	resumeRec := httptest.NewRecorder()
	controller.resumeStorageIO(resumeRec, httptest.NewRequest(http.MethodPost, "/api/system/storage-io/resume", nil))
	if resumeRec.Code != http.StatusAccepted {
		t.Fatalf("expected resume 202, got %d", resumeRec.Code)
	}
	if storageio.Default.BackgroundPaused() {
		t.Fatal("expected storage IO scheduler to be resumed")
	}
}

func TestScannerMetricsUpdateTaskParams(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	taskKey := "scan_library_42"
	if !controller.startTask(taskKey, "scan_library", "scan", 1) {
		t.Fatal("expected task to start")
	}
	controller.handleScannerMetricsEvent(scanner.ScanMetricsReport{
		Scope:                  "library",
		ID:                     42,
		StorageProfile:         config.StorageProfileHDDExternal,
		VolumeKey:              "e:",
		ArchiveOpenConcurrency: 1,
		CoverConcurrency:       1,
		DiscoveredArchives:     12,
		SkippedArchives:        7,
		ProcessedArchives:      5,
		OpenedArchives:         5,
		HashedFiles:            2,
		IOWaitMillis:           123,
		DurationMillis:         456,
	})

	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if task.Params["opened_archives"] != "5" || task.Params["hashed_files"] != "2" || task.Params["io_wait_ms"] != "123" {
		t.Fatalf("expected scanner metrics params, got %+v", task.Params)
	}
}

func TestScannerMetricsAggregateIntoRebuildThumbnailsTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	if !controller.startTask("rebuild_thumbnails", "rebuild_thumbnails", "running", 1) {
		t.Fatal("expected thumbnail rebuild task to start")
	}
	if !controller.startTask("scan_library_42", "scan_library", "running", 1) {
		t.Fatal("expected scan task to start")
	}

	controller.handleScannerMetricsEvent(scanner.ScanMetricsReport{
		Scope:                "library",
		ID:                   42,
		StorageProfile:       config.StorageProfileHDDExternal,
		VolumeKey:            "e:",
		OpenedArchives:       3,
		IOWaitMillis:         120,
		PausedMillis:         80,
		ThumbnailWriteMillis: 40,
		DurationMillis:       60000,
	})
	controller.handleScannerMetricsEvent(scanner.ScanMetricsReport{
		Scope:                "library",
		ID:                   43,
		StorageProfile:       config.StorageProfileHDDExternal,
		VolumeKey:            "e:",
		OpenedArchives:       2,
		IOWaitMillis:         30,
		PausedMillis:         20,
		ThumbnailWriteMillis: 10,
		DurationMillis:       30000,
	})

	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks["rebuild_thumbnails"]
	controller.taskEngine.mutex.Unlock()
	if task.Params["opened_archives"] != "5" || task.Params["io_wait_ms"] != "150" || task.Params["paused_ms"] != "100" || task.Params["thumbnail_write_ms"] != "50" {
		t.Fatalf("expected aggregated thumbnail metrics, got %+v", task.Params)
	}

	scanRate, coverRate, thumbnailWriteMillis := controller.recentStorageIOTaskRates()
	if scanRate <= 0 || coverRate <= 0 || thumbnailWriteMillis != 50 {
		t.Fatalf("expected recent storage IO rates, got scan=%f cover=%f writes=%d", scanRate, coverRate, thumbnailWriteMillis)
	}
}

func TestUpdateSystemConfigRejectsInvalidConfiguration(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	missingCacheDir := filepath.Join(t.TempDir(), "missing-parent", "cache")

	payload := []byte(`{
		"server":{"port":0},
		"database":{"path":""},
		"cache":{"dir":"` + filepath.ToSlash(missingCacheDir) + `"},
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
	controller, store, _, rootDir := newTestController(t)

	t.Run("searches sqlite index", func(t *testing.T) {
		_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Alpha", "Alpha Volume 01.cbz", 10)

		req := httptest.NewRequest(http.MethodGet, "/api/search?q=Alpha&target=book", nil)
		rec := httptest.NewRecorder()
		controller.searchBooks(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}

		var response struct {
			Hits []struct {
				ID string `json:"id"`
			} `json:"hits"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode search response failed: %v", err)
		}
		if len(response.Hits) == 0 || response.Hits[0].ID != "b_"+strconv.FormatInt(book.ID, 10) {
			t.Fatalf("expected sqlite search hit for book %d, got %+v", book.ID, response.Hits)
		}
	})
}

// TestSearchNormalizesScores 验证搜索响应里命中得分被归一化到 (0,1]，最佳匹配为 1.0。
func TestSearchNormalizesScores(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)

	lib, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Naruto", "Naruto Volume 01.cbz", 10)
	for _, name := range []string{"Naruto Gaiden Special Edition.cbz", "Naruto Shippuden Complete.cbz"} {
		if _, err := store.CreateBook(context.Background(), database.CreateBookParams{
			SeriesID:       series.ID,
			LibraryID:      lib.ID,
			Name:           name,
			Path:           filepath.Join(series.Path, name),
			Size:           1024,
			FileModifiedAt: time.Now(),
			PageCount:      10,
		}); err != nil {
			t.Fatalf("CreateBook %s failed: %v", name, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Naruto&target=book", nil)
	rec := httptest.NewRecorder()
	controller.searchBooks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Hits []struct {
			ID    string  `json:"id"`
			Score float64 `json:"score"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response failed: %v", err)
	}
	if len(response.Hits) == 0 {
		t.Fatal("expected at least one search hit")
	}

	var maxScore float64
	for _, hit := range response.Hits {
		if hit.Score <= 0 || hit.Score > 1.0000001 {
			t.Fatalf("score out of (0,1] range: id=%s score=%f", hit.ID, hit.Score)
		}
		if hit.Score > maxScore {
			maxScore = hit.Score
		}
	}
	if maxScore < 0.9999999 {
		t.Fatalf("expected top hit normalized to 1.0, got max=%f", maxScore)
	}
}

// TestSearchHydratesCoversFromDB 验证：即使搜索索引里 cover_path 为空（封面在扫描后才异步生成），
// 搜索响应也会用数据库中的最新封面回填 book/series 命中。
func TestSearchHydratesCoversFromDB(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	ctx := context.Background()

	lib, series, book := seedBookFixture(t, store, rootDir, "Library A", "Beta", "Beta 01.cbz", 10)

	// 模拟扫描后异步生成封面：写入数据库（同时刷新 series_stats 派生 series 封面）。
	const bookCover = "covers/beta-01.webp"
	if _, err := store.SetBookCoverIfMissing(ctx, database.SetBookCoverIfMissingParams{
		CoverPath: sql.NullString{String: bookCover, Valid: true},
		ID:        book.ID,
	}); err != nil {
		t.Fatalf("SetBookCoverIfMissing failed: %v", err)
	}
	if err := store.RefreshSeriesStats(ctx, series.ID); err != nil {
		t.Fatalf("RefreshSeriesStats failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=Beta&target=all", nil)
	rec := httptest.NewRecorder()
	controller.searchBooks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var response struct {
		Hits []struct {
			ID     string         `json:"id"`
			Fields map[string]any `json:"fields"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode search response failed: %v", err)
	}
	if len(response.Hits) == 0 {
		t.Fatal("expected at least one search hit")
	}

	var sawBook, sawSeries bool
	for _, hit := range response.Hits {
		cover, _ := hit.Fields["cover_path"].(string)
		switch hit.ID {
		case "b_" + strconv.FormatInt(book.ID, 10):
			sawBook = true
			if cover != bookCover {
				t.Fatalf("book hit cover not hydrated: got %q want %q", cover, bookCover)
			}
		case "s_" + strconv.FormatInt(series.ID, 10):
			sawSeries = true
			// series 封面由 RefreshSeriesStats 从书的封面派生而来，应非空。
			if cover == "" {
				t.Fatal("series hit cover not hydrated: got empty string")
			}
		}
	}
	if !sawBook {
		t.Fatal("expected book hit in response")
	}
	if !sawSeries {
		t.Fatal("expected series hit in response")
	}
	_ = lib
}

func TestCreateAndUpdateLibraryDefaults(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	libPath := filepath.Join(t.TempDir(), "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}

	createPayload, err := json.Marshal(map[string]string{
		"name": "Main",
		"path": libPath,
	})
	if err != nil {
		t.Fatalf("marshal create library payload failed: %v", err)
	}
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

	updatePayload, err := json.Marshal(map[string]string{
		"name": "Updated",
		"path": updatedPath,
	})
	if err != nil {
		t.Fatalf("marshal update library payload failed: %v", err)
	}
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
	controller, store, _, rootDir := newTestController(t)
	seedBookFixture(t, store, rootDir, "Library A", "Series After Rebuild", "Reindexed Book.cbz", 10)

	req := httptest.NewRequest(http.MethodPost, "/api/system/rebuild-index", nil)
	rec := httptest.NewRecorder()
	controller.rebuildIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
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

	volumeBook, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      series.LibraryID,
		Name:           "Alpha Vol 01.cbz",
		Path:           filepath.Join(rootDir, "Library A", "Series Alpha", "Alpha Vol 01.cbz"),
		Size:           2048,
		FileModifiedAt: time.Now(),
		Volume:         "Volume 1",
		PageCount:      20,
		CoverPath:      sql.NullString{String: "cover.jpg", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateBook volume failed: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 8, Valid: true},
		ID:           volumeBook.ID,
	}); err != nil {
		t.Fatalf("UpdateBookProgress volume failed: %v", err)
	}

	targetSeries, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID:   series.LibraryID,
		Name:        "Series Beta",
		Path:        filepath.Join(rootDir, "Library A", "Series Beta"),
		NameInitial: database.SeriesInitial("", "Series Beta"),
	})
	if err != nil {
		t.Fatalf("CreateSeries target failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().ExecContext(context.Background(),
		`INSERT INTO series_relations (source_series_id, target_series_id, relation_type) VALUES (?, ?, ?)`,
		series.ID, targetSeries.ID, "spinoff"); err != nil {
		t.Fatalf("insert relation failed: %v", err)
	}

	if _, _, _, err := controller.queueMetadataReview(context.Background(), info, &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "Alpha Metadata",
		Summary:    "Reviewed summary",
		SourceID:   42,
		SourceURL:  "https://bgm.tv/subject/42",
		Confidence: 0.9,
	}, "bangumi", "Alpha"); err != nil {
		t.Fatalf("queue metadata review failed: %v", err)
	}

	taskKey := "scan_series_" + strconv.FormatInt(series.ID, 10)
	if !controller.startTask(taskKey, "scan_series", "scan series", 1) {
		t.Fatal("expected scan series task to start")
	}
	controller.failTaskWithError(taskKey, "failed series scan", "archive error")

	contextRec := httptest.NewRecorder()
	controller.getSeriesContext(contextRec, requestWithRouteParam(http.MethodGet, "/api/series/1/context", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if contextRec.Code != http.StatusOK {
		t.Fatalf("expected get series context 200, got %d", contextRec.Code)
	}

	var seriesContext SeriesContextResponse
	if err := json.NewDecoder(contextRec.Body).Decode(&seriesContext); err != nil {
		t.Fatalf("decode series context failed: %v", err)
	}
	if seriesContext.Series.ID != series.ID || len(seriesContext.Books) != 2 || len(seriesContext.Tags) != 2 || len(seriesContext.Authors) != 2 || len(seriesContext.Links) != 1 {
		t.Fatalf("unexpected series context payload: %+v", seriesContext)
	}
	if len(seriesContext.Volumes) != 1 || seriesContext.Volumes[0].Name != "Volume 1" || seriesContext.Volumes[0].ReadPages != 8 {
		t.Fatalf("unexpected volume summary: %+v", seriesContext.Volumes)
	}
	if len(seriesContext.Relations) != 1 || seriesContext.Relations[0].TargetSeriesID != targetSeries.ID {
		t.Fatalf("unexpected relation context: %+v", seriesContext.Relations)
	}
	if len(seriesContext.MetadataReview.Reviews) != 1 || seriesContext.MetadataReview.Reviews[0].Provider != "bangumi" {
		t.Fatalf("unexpected metadata review context: %+v", seriesContext.MetadataReview)
	}
	if seriesContext.MetadataSummary.PendingReviewCount != 1 {
		t.Fatalf("unexpected metadata summary: %+v", seriesContext.MetadataSummary)
	}
	if len(seriesContext.FailedTasks) != 1 || seriesContext.FailedTasks[0].Key != taskKey || seriesContext.FailedTaskSummary.Count != 1 {
		t.Fatalf("unexpected failed task context: tasks=%+v summary=%+v", seriesContext.FailedTasks, seriesContext.FailedTaskSummary)
	}
	if seriesContext.Continue.TotalBooks != 2 {
		t.Fatalf("expected continue.total_books=2, got %+v", seriesContext.Continue)
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
	if providers[0]["id"] != "bangumi" {
		t.Fatalf("expected first provider to be bangumi: %+v", providers)
	}
	providerIDs := map[string]bool{}
	for _, p := range providers {
		providerIDs[p["id"]] = true
	}
	for _, want := range []string{"bangumi", "anilist", "mangadex", "llm"} {
		if !providerIDs[want] {
			t.Fatalf("expected provider %q present, got %+v", want, providers)
		}
	}
	// 未配置密钥时 MyAnimeList / Comic Vine 不应出现。
	if providerIDs["myanimelist"] || providerIDs["comicvine"] {
		t.Fatalf("key-gated providers should be absent without config: %+v", providers)
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

func TestOpenSeriesDirectory(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	_, series, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	var openedPath string
	controller.openPath = func(path string) error {
		openedPath = path
		return nil
	}

	successRec := httptest.NewRecorder()
	successReq := requestWithRouteParam(http.MethodPost, "/api/series/1/open-dir", nil, "seriesId", strconv.FormatInt(series.ID, 10))
	controller.openSeriesDirectory(successRec, successReq)
	if successRec.Code != http.StatusOK {
		t.Fatalf("expected open series directory 200, got %d body=%s", successRec.Code, successRec.Body.String())
	}
	if openedPath != series.Path {
		t.Fatalf("expected opened path %q, got %q", series.Path, openedPath)
	}

	notFoundRec := httptest.NewRecorder()
	notFoundReq := requestWithRouteParam(http.MethodPost, "/api/series/999/open-dir", nil, "seriesId", "999")
	controller.openSeriesDirectory(notFoundRec, notFoundReq)
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected missing series 404, got %d", notFoundRec.Code)
	}

	controller.openPath = func(path string) error {
		return os.ErrPermission
	}

	errorRec := httptest.NewRecorder()
	errorReq := requestWithRouteParam(http.MethodPost, "/api/series/1/open-dir", nil, "seriesId", strconv.FormatInt(series.ID, 10))
	controller.openSeriesDirectory(errorRec, errorReq)
	if errorRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected open failure 500, got %d body=%s", errorRec.Code, errorRec.Body.String())
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

func TestSearchSeriesPagedFiltersChineseInitials(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)

	createSeries := func(name, title string) database.Series {
		t.Helper()
		series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        filepath.Join(rootDir, "Library A", name),
			Title:       sql.NullString{String: title, Valid: title != ""},
			NameInitial: database.SeriesInitial(title, name),
		})
		if err != nil {
			t.Fatalf("CreateSeries %s failed: %v", name, err)
		}
		return series
	}

	jSeries := createSeries("folder-j", "《进击的巨人》")
	oSeries := createSeries("folder-o", "— One Piece")
	hashSeries := createSeries("folder-hash", "12345...")

	requestSearch := func(letter string) []database.SearchSeriesPagedRow {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/series/search?libraryId="+strconv.FormatInt(lib.ID, 10)+"&limit=10&page=1&letter="+letter, nil)
		rec := httptest.NewRecorder()
		controller.searchSeriesPaged(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected search %s 200, got %d body=%s", letter, rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []database.SearchSeriesPagedRow `json:"items"`
			Total int                             `json:"total"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode search %s failed: %v", letter, err)
		}
		if resp.Total != len(resp.Items) {
			t.Fatalf("expected total to match item count, got total=%d items=%d", resp.Total, len(resp.Items))
		}
		return resp.Items
	}

	assertOnly := func(letter string, expectedID int64) {
		t.Helper()
		items := requestSearch(letter)
		if len(items) != 1 || items[0].ID != expectedID {
			t.Fatalf("expected only series %d for letter %s, got %+v", expectedID, letter, items)
		}
	}

	assertOnly("J", jSeries.ID)
	assertOnly("O", oSeries.ID)
	assertOnly("#", hashSeries.ID)
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

	searchTagsRec := httptest.NewRecorder()
	controller.searchTags(searchTagsRec, httptest.NewRequest(http.MethodGet, "/api/tags/search?q=mys&limit=5", nil))
	if searchTagsRec.Code != http.StatusOK {
		t.Fatalf("expected search tags 200, got %d", searchTagsRec.Code)
	}
	var searchedTags []database.Tag
	if err := json.NewDecoder(searchTagsRec.Body).Decode(&searchedTags); err != nil {
		t.Fatalf("decode searched tags failed: %v", err)
	}
	if len(searchedTags) != 1 || searchedTags[0].Name != "Mystery" {
		t.Fatalf("unexpected searched tags payload: %+v", searchedTags)
	}

	searchAuthorsRec := httptest.NewRecorder()
	controller.searchAuthors(searchAuthorsRec, httptest.NewRequest(http.MethodGet, "/api/authors/search?q=writer&limit=5", nil))
	if searchAuthorsRec.Code != http.StatusOK {
		t.Fatalf("expected search authors 200, got %d", searchAuthorsRec.Code)
	}
	var searchedAuthors []database.Author
	if err := json.NewDecoder(searchAuthorsRec.Body).Decode(&searchedAuthors); err != nil {
		t.Fatalf("decode searched authors failed: %v", err)
	}
	if len(searchedAuthors) != 1 || searchedAuthors[0].Name != "Writer A" {
		t.Fatalf("unexpected searched authors payload: %+v", searchedAuthors)
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

func TestGetNextBookUsesChineseChapterOrder(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)

	_, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series Chinese", "第一话.cbz", 10)
	if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE books SET sort_number = ? WHERE id = ?`, 0, firstBook.ID); err != nil {
		t.Fatalf("update first sort failed: %v", err)
	}
	createBook := func(name string) database.Book {
		t.Helper()
		book, err := store.CreateBook(context.Background(), database.CreateBookParams{
			SeriesID:       series.ID,
			LibraryID:      firstBook.LibraryID,
			Name:           name,
			Path:           filepath.Join(rootDir, "Library A", "Series Chinese", name),
			Size:           1024,
			FileModifiedAt: time.Now(),
			Volume:         "",
			Title:          sql.NullString{String: name, Valid: true},
			SortNumber:     sql.NullFloat64{Float64: 0, Valid: true},
			PageCount:      10,
		})
		if err != nil {
			t.Fatalf("CreateBook %s failed: %v", name, err)
		}
		return book
	}
	secondBook := createBook("第二话.cbz")
	tenthBook := createBook("第十话.cbz")
	eleventhBook := createBook("第十一话.cbz")

	listRec := httptest.NewRecorder()
	controller.getBooksBySeries(listRec, requestWithRouteParam(http.MethodGet, "/api/series/1/books", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected get books 200, got %d", listRec.Code)
	}
	var books []database.Book
	if err := json.NewDecoder(listRec.Body).Decode(&books); err != nil {
		t.Fatalf("decode books failed: %v", err)
	}
	if len(books) != 4 {
		t.Fatalf("expected 4 books, got %+v", books)
	}
	gotOrder := []int64{books[0].ID, books[1].ID, books[2].ID, books[3].ID}
	wantOrder := []int64{firstBook.ID, secondBook.ID, tenthBook.ID, eleventhBook.ID}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("unexpected Chinese chapter order: got %+v want %+v", gotOrder, wantOrder)
		}
	}

	nextRec := httptest.NewRecorder()
	controller.getNextBook(nextRec, requestWithRouteParam(http.MethodGet, "/api/books/10/next", nil, "bookId", strconv.FormatInt(tenthBook.ID, 10)))
	if nextRec.Code != http.StatusOK {
		t.Fatalf("expected get next book 200, got %d body=%s", nextRec.Code, nextRec.Body.String())
	}
	var nextBook database.Book
	if err := json.NewDecoder(nextRec.Body).Decode(&nextBook); err != nil {
		t.Fatalf("decode next book failed: %v", err)
	}
	if nextBook.ID != eleventhBook.ID {
		t.Fatalf("expected eleventh book as next, got %+v", nextBook)
	}
}

func TestSearchSeriesPagedSupportsAdditionalSortFields(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)

	lib, seriesA, bookA := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 10)
	if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE series SET volume_count = ?, total_pages = ? WHERE id = ?`, 1, 10, seriesA.ID); err != nil {
		t.Fatalf("update series A stats failed: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 3, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           bookA.ID,
	}); err != nil {
		t.Fatalf("update book A read progress failed: %v", err)
	}

	seriesB, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Series Beta",
		Path:        filepath.Join(rootDir, "Library A", "Series Beta"),
		NameInitial: database.SeriesInitial("", "Series Beta"),
	})
	if err != nil {
		t.Fatalf("CreateSeries B failed: %v", err)
	}
	if err := os.MkdirAll(seriesB.Path, 0o755); err != nil {
		t.Fatalf("mkdir series B path failed: %v", err)
	}
	bookB, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       seriesB.ID,
		LibraryID:      lib.ID,
		Name:           "Beta 01.cbz",
		Path:           filepath.Join(seriesB.Path, "Beta 01.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		PageCount:      30,
		Title:          sql.NullString{String: "Beta 01", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateBook B failed: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 20, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           bookB.ID,
	}); err != nil {
		t.Fatalf("update book B read progress failed: %v", err)
	}
	if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE series SET volume_count = ?, book_count = ?, total_pages = ? WHERE id = ?`, 4, 1, 30, seriesB.ID); err != nil {
		t.Fatalf("update series B stats failed: %v", err)
	}

	assertFirst := func(sortBy string, expectedID int64) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/series/search?libraryId="+strconv.FormatInt(lib.ID, 10)+"&limit=10&page=1&sortBy="+sortBy, nil)
		rec := httptest.NewRecorder()
		controller.searchSeriesPaged(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for sort %s, got %d body=%s", sortBy, rec.Code, rec.Body.String())
		}

		var resp struct {
			Items []database.SearchSeriesPagedRow `json:"items"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode search response for %s failed: %v", sortBy, err)
		}
		if len(resp.Items) < 2 {
			t.Fatalf("expected at least 2 items for %s, got %+v", sortBy, resp.Items)
		}
		if resp.Items[0].ID != expectedID {
			t.Fatalf("expected first series %d for sort %s, got %+v", expectedID, sortBy, resp.Items)
		}
	}

	assertFirst("volumes_desc", seriesB.ID)
	assertFirst("pages_desc", seriesB.ID)
	assertFirst("read_desc", seriesB.ID)
}

func TestSearchSeriesPagedReturnsAndAcceptsCursor(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 10)

	for _, name := range []string{"Series Beta", "Series Gamma"} {
		series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        filepath.Join(rootDir, "Library A", name),
			NameInitial: database.SeriesInitial("", name),
		})
		if err != nil {
			t.Fatalf("CreateSeries %s failed: %v", name, err)
		}
		if _, err := controller.store.(*database.SqlStore).DB().Exec(`UPDATE series SET updated_at = ? WHERE id = ?`, time.Now().Add(time.Duration(len(name))*time.Minute), series.ID); err != nil {
			t.Fatalf("update series %s failed: %v", name, err)
		}
	}

	firstReq := httptest.NewRequest(http.MethodGet, "/api/series/search?libraryId="+strconv.FormatInt(lib.ID, 10)+"&limit=1&page=1&sortBy=name_asc", nil)
	firstRec := httptest.NewRecorder()
	controller.searchSeriesPaged(firstRec, firstReq)
	if firstRec.Code != http.StatusOK {
		t.Fatalf("expected first page 200, got %d body=%s", firstRec.Code, firstRec.Body.String())
	}
	var firstResp struct {
		Items      []database.SearchSeriesPagedRow `json:"items"`
		Total      int                             `json:"total"`
		NextCursor string                          `json:"next_cursor"`
		HasMore    bool                            `json:"has_more"`
	}
	if err := json.NewDecoder(firstRec.Body).Decode(&firstResp); err != nil {
		t.Fatalf("decode first response failed: %v", err)
	}
	if firstResp.Total != 3 || len(firstResp.Items) != 1 || firstResp.NextCursor == "" || !firstResp.HasMore {
		t.Fatalf("unexpected first response: %+v", firstResp)
	}

	secondURL := "/api/series/search?libraryId=" + strconv.FormatInt(lib.ID, 10) + "&limit=1&page=2&sortBy=name_asc&cursor=" + url.QueryEscape(firstResp.NextCursor)
	secondReq := httptest.NewRequest(http.MethodGet, secondURL, nil)
	secondRec := httptest.NewRecorder()
	controller.searchSeriesPaged(secondRec, secondReq)
	if secondRec.Code != http.StatusOK {
		t.Fatalf("expected cursor page 200, got %d body=%s", secondRec.Code, secondRec.Body.String())
	}
	var secondResp struct {
		Items      []database.SearchSeriesPagedRow `json:"items"`
		Total      int                             `json:"total"`
		NextCursor string                          `json:"next_cursor"`
		HasMore    bool                            `json:"has_more"`
	}
	if err := json.NewDecoder(secondRec.Body).Decode(&secondResp); err != nil {
		t.Fatalf("decode second response failed: %v", err)
	}
	if secondResp.Total != 0 || len(secondResp.Items) != 1 || secondResp.Items[0].Name <= firstResp.Items[0].Name {
		t.Fatalf("unexpected cursor response: first=%+v second=%+v", firstResp, secondResp)
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
		Name:                "Main",
		Path:                libPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
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

func TestExternalLibraryScanAndTransferFlow(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)

	lib, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series Alpha", "Alpha 01.cbz", 12)
	if err := os.WriteFile(firstBook.Path, []byte("alpha-01"), 0o644); err != nil {
		t.Fatalf("write first book failed: %v", err)
	}

	secondPath := filepath.Join(lib.Path, "Series Alpha", "Alpha 02.cbz")
	secondBook, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Alpha 02.cbz",
		Path:           secondPath,
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "",
		Title:          sql.NullString{String: "Book 2", Valid: true},
		PageCount:      10,
	})
	if err != nil {
		t.Fatalf("CreateBook second failed: %v", err)
	}
	if err := os.WriteFile(secondBook.Path, []byte("alpha-02"), 0o644); err != nil {
		t.Fatalf("write second book failed: %v", err)
	}

	externalRoot := filepath.Join(rootDir, "device")
	if err := os.MkdirAll(filepath.Join(externalRoot, "Series Alpha"), 0o755); err != nil {
		t.Fatalf("mkdir external root failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(externalRoot, "Series Alpha", "Alpha 01.cbz"), []byte("existing-copy"), 0o644); err != nil {
		t.Fatalf("write external file failed: %v", err)
	}

	createPayload, err := json.Marshal(map[string]any{
		"external_path": externalRoot,
	})
	if err != nil {
		t.Fatalf("marshal create external session payload failed: %v", err)
	}
	createReq := requestWithRouteParams(http.MethodPost, "/api/libraries/1/external-libraries/session", createPayload, map[string]string{
		"libraryId": strconv.FormatInt(lib.ID, 10),
	})
	createRec := httptest.NewRecorder()
	controller.createExternalLibrarySession(createRec, createReq)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("expected create external session 202, got %d body=%s", createRec.Code, createRec.Body.String())
	}

	var createResp struct {
		Session external.SessionSnapshot `json:"session"`
		TaskKey string                   `json:"task_key"`
	}
	if err := json.NewDecoder(createRec.Body).Decode(&createResp); err != nil {
		t.Fatalf("decode external session failed: %v", err)
	}
	session := createResp.Session
	if createResp.TaskKey == "" {
		t.Fatal("expected task_key in create external session response")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, getErr := controller.external.GetSession(lib.ID, session.SessionID)
		if getErr == nil && current.Status == "ready" {
			session = current
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if session.Status != "ready" {
		t.Fatalf("expected session ready, got %+v", session)
	}

	seriesReq := requestWithRouteParams(http.MethodGet, "/api/libraries/1/external-libraries/session/s/series?ids="+strconv.FormatInt(series.ID, 10), nil, map[string]string{
		"libraryId": strconv.FormatInt(lib.ID, 10),
		"sessionId": session.SessionID,
	})
	seriesRec := httptest.NewRecorder()
	controller.getExternalLibrarySeries(seriesRec, seriesReq)
	if seriesRec.Code != http.StatusOK {
		t.Fatalf("expected series coverage 200, got %d body=%s", seriesRec.Code, seriesRec.Body.String())
	}

	var coverage []external.SeriesCoverage
	if err := json.NewDecoder(seriesRec.Body).Decode(&coverage); err != nil {
		t.Fatalf("decode series coverage failed: %v", err)
	}
	if len(coverage) != 1 || coverage[0].ExternalMatchCount != 1 || coverage[0].ExternalTotalCount != 2 || coverage[0].ExternalSyncStatus != "partial" {
		t.Fatalf("unexpected coverage: %+v", coverage)
	}

	transferReq := requestWithRouteParams(http.MethodPost, "/api/libraries/1/external-libraries/session/s/transfer", []byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`]}`), map[string]string{
		"libraryId": strconv.FormatInt(lib.ID, 10),
		"sessionId": session.SessionID,
	})
	transferRec := httptest.NewRecorder()
	controller.transferToExternalLibrary(transferRec, transferReq)
	if transferRec.Code != http.StatusAccepted {
		t.Fatalf("expected transfer queued 202, got %d body=%s", transferRec.Code, transferRec.Body.String())
	}
	var transferResp struct {
		TaskKey string `json:"task_key"`
	}
	if err := json.NewDecoder(transferRec.Body).Decode(&transferResp); err != nil {
		t.Fatalf("decode transfer response failed: %v", err)
	}
	if transferResp.TaskKey == "" {
		t.Fatal("expected task_key in transfer response")
	}

	targetPath := filepath.Join(externalRoot, "Series Alpha", "Alpha 02.cbz")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(targetPath); statErr == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("expected transferred file to exist: %v", err)
	}

	updatedCoverage, err := controller.external.GetSeriesCoverage(lib.ID, session.SessionID, []int64{series.ID})
	if err != nil {
		t.Fatalf("GetSeriesCoverage after transfer failed: %v", err)
	}
	if len(updatedCoverage) != 1 || updatedCoverage[0].ExternalMatchCount != 2 || updatedCoverage[0].ExternalSyncStatus != "complete" {
		t.Fatalf("unexpected updated coverage: %+v", updatedCoverage)
	}
}

func TestExternalLibraryScanIgnoreExtensionOption(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)

	lib, series, book := seedBookFixture(t, store, rootDir, "Library B", "Series Beta", "Beta 01.cbz", 10)
	if err := os.WriteFile(book.Path, []byte("beta-01"), 0o644); err != nil {
		t.Fatalf("write book failed: %v", err)
	}

	externalRoot := filepath.Join(rootDir, "device-ignore-extension")
	if err := os.MkdirAll(filepath.Join(externalRoot, "Series Beta"), 0o755); err != nil {
		t.Fatalf("mkdir external root failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(externalRoot, "Series Beta", "Beta 01.zip"), []byte("existing-copy"), 0o644); err != nil {
		t.Fatalf("write external file failed: %v", err)
	}

	createSession := func(ignoreExtension bool) external.SessionSnapshot {
		body, err := json.Marshal(map[string]any{
			"external_path":    externalRoot,
			"ignore_extension": ignoreExtension,
		})
		if err != nil {
			t.Fatalf("marshal create external session payload failed: %v", err)
		}
		req := requestWithRouteParams(http.MethodPost, "/api/libraries/1/external-libraries/session", body, map[string]string{
			"libraryId": strconv.FormatInt(lib.ID, 10),
		})
		rec := httptest.NewRecorder()
		controller.createExternalLibrarySession(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected create external session 202, got %d body=%s", rec.Code, rec.Body.String())
		}

		var resp struct {
			Session external.SessionSnapshot `json:"session"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode external session failed: %v", err)
		}

		session := resp.Session
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			current, getErr := controller.external.GetSession(lib.ID, session.SessionID)
			if getErr == nil && current.Status == "ready" {
				return current
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("expected external session ready, got %+v", session)
		return external.SessionSnapshot{}
	}

	strictSession := createSession(false)
	if strictSession.IgnoreExtension {
		t.Fatal("expected strict session to require matching extension")
	}
	strictCoverage, err := controller.external.GetSeriesCoverage(lib.ID, strictSession.SessionID, []int64{series.ID})
	if err != nil {
		t.Fatalf("GetSeriesCoverage strict failed: %v", err)
	}
	if len(strictCoverage) != 1 || strictCoverage[0].ExternalMatchCount != 0 || strictCoverage[0].ExternalSyncStatus != "missing" {
		t.Fatalf("unexpected strict coverage: %+v", strictCoverage)
	}

	ignoreSession := createSession(true)
	if !ignoreSession.IgnoreExtension {
		t.Fatal("expected ignore-extension session flag to be true")
	}
	ignoreCoverage, err := controller.external.GetSeriesCoverage(lib.ID, ignoreSession.SessionID, []int64{series.ID})
	if err != nil {
		t.Fatalf("GetSeriesCoverage ignore-extension failed: %v", err)
	}
	if len(ignoreCoverage) != 1 || ignoreCoverage[0].ExternalMatchCount != 1 || ignoreCoverage[0].ExternalTotalCount != 1 || ignoreCoverage[0].ExternalSyncStatus != "complete" {
		t.Fatalf("unexpected ignore-extension coverage: %+v", ignoreCoverage)
	}

	transferReq := requestWithRouteParams(http.MethodPost, "/api/libraries/1/external-libraries/session/s/transfer", []byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`]}`), map[string]string{
		"libraryId": strconv.FormatInt(lib.ID, 10),
		"sessionId": ignoreSession.SessionID,
	})
	transferRec := httptest.NewRecorder()
	controller.transferToExternalLibrary(transferRec, transferReq)
	if transferRec.Code != http.StatusOK {
		t.Fatalf("expected transfer to be skipped with 200, got %d body=%s", transferRec.Code, transferRec.Body.String())
	}
	var transferResp struct {
		MissingBooks int `json:"missing_books"`
	}
	if err := json.NewDecoder(transferRec.Body).Decode(&transferResp); err != nil {
		t.Fatalf("decode transfer response failed: %v", err)
	}
	if transferResp.MissingBooks != 0 {
		t.Fatalf("expected no missing books when ignoring extension, got %+v", transferResp)
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
	missingBrowsePath := filepath.Join(t.TempDir(), "definitely-missing")
	controller.browseDirs(browseInvalidRec, httptest.NewRequest(http.MethodGet, "/api/browse?path="+url.QueryEscape(missingBrowsePath), nil))
	if browseInvalidRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid browse path 400, got %d", browseInvalidRec.Code)
	}

	browseReq := httptest.NewRequest(http.MethodGet, "/api/browse?path="+url.QueryEscape(rootDir), nil)
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

func TestTasksPersistAcrossControllerInstances(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)

	if !controller.startTask("scan_series_77", "scan_series", "series 77", 1) {
		t.Fatal("expected task to start")
	}
	controller.setTaskMetadata("scan_series_77", map[string]string{"force": "true"}, "Series 77")
	controller.failTaskWithError("scan_series_77", "failed series scan", "archive error")
	// 进度/终态改为异步落盘（M42）：显式刷一次，模拟服务生命周期内的落盘/优雅关闭，再由新实例读回。
	controller.flushTaskPersist()

	cfg := controller.config
	imageCache, _ := lru.New[string, []byte](8)
	pageCache, _ := lru.New[string, []parser.PageMetadata](8)
	bookPageSourceCache, _ := lru.New[int64, cachedBookPageSource](8)
	progressWriteCache, _ := lru.New[int64, cachedProgressWrite](8)
	reloaded := &Controller{
		store:               store,
		imageCache:          imageCache,
		pageCache:           pageCache,
		bookPageSourceCache: bookPageSourceCache,
		progressWriteCache:  progressWriteCache,
		scanner:             scanner.NewScanner(store, cfg),
		config:              cfg,
		koreader:            koreader.NewService(store, cfg),
		external:            external.NewManager(store, 30*time.Minute),
		configPath:          filepath.Join(tempDir, "config.yaml"),
		taskEngine:          newTaskEngine(),
		messages:            make(chan string, 32),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks?scope=series&scope_id=77", nil)
	rec := httptest.NewRecorder()
	reloaded.listTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var tasks []TaskStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode persisted tasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one persisted task, got %+v", tasks)
	}
	task := tasks[0]
	if task.Key != "scan_series_77" || task.Status != "failed" || task.Error != "archive error" {
		t.Fatalf("unexpected persisted task: %+v", task)
	}
	if task.Params == nil || task.Params["force"] != "true" {
		t.Fatalf("expected persisted params, got %+v", task.Params)
	}
	if task.ScopeName != "Series 77" {
		t.Fatalf("expected persisted scope name, got %q", task.ScopeName)
	}
}

func TestNewControllerMarksPersistedRunningTasksInterrupted(t *testing.T) {
	_, store, _, tempDir := newTestController(t)
	now := time.Now().Add(-time.Minute)
	scopeID := int64(55)
	if err := store.UpsertTask(context.Background(), database.TaskRecord{
		Key:       "scan_series_55",
		Type:      "scan_series",
		Scope:     "series",
		ScopeID:   &scopeID,
		Status:    "running",
		Message:   "running before restart",
		Total:     1,
		Retryable: true,
		StartedAt: now,
		UpdatedAt: now,
		Sequence:  42,
	}); err != nil {
		t.Fatalf("upsert running task failed: %v", err)
	}

	cfg := &config.Config{}
	cfg.Database.Path = filepath.Join(tempDir, "test.db")
	cfg.Cache.Dir = filepath.Join(tempDir, "cache")
	cfg.Scanner.ArchivePoolSize = 5
	cfg.Scanner.MaxAiConcurrency = 3
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "qwen2.5"
	config.NormalizeConfig(cfg)
	cfgManager := config.NewManager(cfg)
	controller := NewController(store, scanner.NewScanner(store, cfgManager), cfgManager, filepath.Join(tempDir, "config.yaml"))
	t.Cleanup(controller.Close)

	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks?scope=series&scope_id=55", nil)
	rec := httptest.NewRecorder()
	controller.listTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var tasks []TaskStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one task, got %+v", tasks)
	}
	if tasks[0].Status != "interrupted" || tasks[0].Error == "" {
		t.Fatalf("expected interrupted task status with error, got %+v", tasks[0])
	}
	if !tasks[0].Retryable {
		t.Fatalf("expected interrupted task to remain retryable")
	}
}

func TestClearTasksRemovesMatchingStatuses(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if !controller.startTask("completed_one", "rebuild_index", "completed task", 1) {
		t.Fatal("expected completed task to start")
	}
	controller.finishTask("completed_one", "done")

	if !controller.startTask("failed_one", "scan_library", "failed task", 1) {
		t.Fatal("expected failed task to start")
	}
	controller.failTask("failed_one", "boom")

	req := httptest.NewRequest(http.MethodDelete, "/api/system/tasks?status=completed", nil)
	rec := httptest.NewRecorder()
	controller.clearTasks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	controller.taskEngine.mutex.Lock()
	_, completedExists := controller.taskEngine.tasks["completed_one"]
	_, failedExists := controller.taskEngine.tasks["failed_one"]
	controller.taskEngine.mutex.Unlock()
	if completedExists {
		t.Fatal("expected completed task to be removed")
	}
	if !failedExists {
		t.Fatal("expected failed task to remain")
	}
}

func TestClearTasksSupportsTypeAndScopeIDFilters(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if !controller.startTask("scan_series_10", "scan_series", "series 10", 1) {
		t.Fatal("expected first task to start")
	}
	controller.finishTask("scan_series_10", "done")

	if !controller.startTask("scan_series_11", "scan_series", "series 11", 1) {
		t.Fatal("expected second task to start")
	}
	controller.finishTask("scan_series_11", "done")

	if !controller.startTask("scan_library_11", "scan_library", "library 11", 1) {
		t.Fatal("expected third task to start")
	}
	controller.finishTask("scan_library_11", "done")
	// 终态改为异步落盘（M42）：清理走 DeleteTasks 删 DB 记录，需先刷盘让已完成任务进入 DB 再清。
	controller.flushTaskPersist()

	req := httptest.NewRequest(http.MethodDelete, "/api/system/tasks?type=scan_series&scope_id=11", nil)
	rec := httptest.NewRecorder()
	controller.clearTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Removed int `json:"removed"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode clear response failed: %v", err)
	}
	if payload.Removed != 1 {
		t.Fatalf("expected one removed task, got %d", payload.Removed)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/system/tasks", nil)
	rec = httptest.NewRecorder()
	controller.listTasks(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list 200, got %d", rec.Code)
	}
	var tasks []TaskStatus
	if err := json.NewDecoder(rec.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode tasks failed: %v", err)
	}
	keys := make(map[string]bool)
	for _, task := range tasks {
		keys[task.Key] = true
	}
	if keys["scan_series_11"] {
		t.Fatalf("expected scan_series_11 to be removed, got %+v", keys)
	}
	if !keys["scan_series_10"] || !keys["scan_library_11"] {
		t.Fatalf("expected other tasks to remain, got %+v", keys)
	}
}

func TestCancelTaskRequestsRunningCancellation(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "scan_library_42"
	if !controller.startCancelableTask(taskKey, "scan_library", "running", 1) {
		t.Fatal("expected task to start")
	}
	ctx, cleanup := controller.newTaskContext(taskKey)
	defer cleanup()

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/scan_library_42/cancel", nil, "taskKey", taskKey)
	rec := httptest.NewRecorder()
	controller.cancelTask(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("expected task context to be cancelled")
	}

	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if task.CanCancel {
		t.Fatalf("expected task to be marked non-cancellable after cancel request: %+v", task)
	}
	if task.Status != "cancelling" {
		t.Fatalf("expected cancelling status, got %+v", task)
	}
	if task.MessageCode != "task.msg.control.cancelling" {
		t.Fatalf("expected cancelling message code, got code=%q message=%q", task.MessageCode, task.Message)
	}
	if task.Message != "" {
		t.Fatalf("expected message cleared when code set, got %q", task.Message)
	}
}

func TestPauseResumeTaskLifecycle(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "scan_library_42"
	if !controller.startPausableCancelableTask(taskKey, "scan_library", "running", 10) {
		t.Fatal("expected task to start")
	}
	ctx, cleanup := controller.newTaskContext(taskKey)
	defer cleanup()

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/scan_library_42/pause", nil, "taskKey", taskKey)
	rec := httptest.NewRecorder()
	controller.pauseTask(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected pause 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	controller.taskEngine.mutex.Lock()
	paused := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if paused.Status != "paused" || !paused.CanResume || paused.CanPause {
		t.Fatalf("expected paused resumable task, got %+v", paused)
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- taskcontrol.Wait(ctx)
	}()
	select {
	case err := <-waitDone:
		t.Fatalf("expected checkpoint to block while paused, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	req = requestWithRouteParam(http.MethodPost, "/api/system/tasks/scan_library_42/resume", nil, "taskKey", taskKey)
	rec = httptest.NewRecorder()
	controller.resumeTask(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected resume 202, got %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("expected resumed checkpoint without error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected checkpoint to unblock after resume")
	}

	controller.taskEngine.mutex.Lock()
	resumed := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if resumed.Status != "running" || resumed.CanResume || !resumed.CanPause {
		t.Fatalf("expected running pausable task, got %+v", resumed)
	}
}

func TestTaskStatusHydratesDerivedFieldsFromParams(t *testing.T) {
	pausedAt := time.Now().UTC().Truncate(time.Second)
	record := database.TaskRecord{
		Key:       "scan_library_7",
		Type:      "scan_library",
		Scope:     "library",
		Status:    "paused",
		Message:   "paused",
		Current:   5,
		Total:     10,
		StartedAt: time.Now().Add(-time.Minute),
		UpdatedAt: time.Now(),
		Params: map[string]string{
			"phase":                               "reading_metadata",
			"current_item":                        "book.cbz",
			"can_pause":                           "false",
			"can_resume":                          "true",
			"paused_at":                           pausedAt.Format(time.RFC3339Nano),
			"pause_reason":                        "manual_pause",
			"metric.opened_archives":              "5",
			"label.current_library":               "Main",
			"limit.scanner_workers_effective":     "1",
			"limit.storage_profile":               "hdd_external",
			"limit.archive_open_concurrency":      "1",
			"limit.pause_background_when_reading": "true",
			"limit.disable_same_disk_page_cache":  "true",
			"limit.idle_only_heavy_tasks":         "true",
			"limit.scan_profile":                  "metadata_scan",
			"limit.scanner_workers_configured":    "0",
			"limit.scan_concurrency":              "1",
			"limit.cover_concurrency":             "1",
			"limit.hash_concurrency":              "1",
			"limit.volume_key":                    "e:",
		},
	}

	task := taskStatusFromRecord(record)
	if task.Phase != "reading_metadata" || task.CurrentItem != "book.cbz" {
		t.Fatalf("expected derived phase/current item, got %+v", task)
	}
	if task.PausedAt == nil || !task.CanResume || task.CanPause || task.PauseReason != "manual_pause" {
		t.Fatalf("expected pause fields to hydrate, got %+v", task)
	}
	if task.Metrics["opened_archives"] != 5 || task.Labels["current_library"] != "Main" {
		t.Fatalf("expected metrics and labels to hydrate, got %+v", task)
	}
	if task.EffectiveLimit == nil || task.EffectiveLimit.ScannerWorkersEffective != 1 || task.EffectiveLimit.StorageProfile != "hdd_external" {
		t.Fatalf("expected effective limit to hydrate, got %+v", task.EffectiveLimit)
	}
	if task.Percent == nil || *task.Percent != 50 {
		t.Fatalf("expected percent from current/total, got %+v", task.Percent)
	}
}

func TestScanTaskEffectiveLimitsUseExternalHDDPolicy(t *testing.T) {
	controller, _, _, tempDir := newTestController(t)

	libraryPath := filepath.Join(tempDir, "external-hdd", "library-a")
	cfg := controller.config.Snapshot()
	cfg.Scanner.Workers = 8
	cfg.Scanner.ScanProfile = string(scanner.ScanProfileIdentity)
	cfg.Library.StorageProfile = config.StorageProfileAuto
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{
		{
			Path:           libraryPath,
			StorageProfile: config.StorageProfileHDDExternal,
			IOPolicy: config.StorageIOPolicy{
				ScanConcurrency:            1,
				ArchiveOpenConcurrency:     1,
				CoverConcurrency:           1,
				HashConcurrency:            1,
				PauseBackgroundWhenReading: true,
				IdleOnlyHeavyTasks:         true,
				DisableSameDiskPageCache:   true,
			},
		},
	}
	config.NormalizeConfig(&cfg)
	controller.config.Replace(&cfg)

	limit := controller.taskLimitsForPath(libraryPath, true)
	if limit.ScanProfile != string(scanner.ScanProfileIdentity) {
		t.Fatalf("expected identity scan profile, got %+v", limit)
	}
	if limit.ScannerWorkersConfigured != 8 || limit.ScannerWorkersEffective != 1 {
		t.Fatalf("expected worker limit 8 -> 1, got %+v", limit)
	}
	if limit.StorageProfile != config.StorageProfileHDDExternal {
		t.Fatalf("expected external HDD storage profile, got %+v", limit)
	}
	if limit.ScanConcurrency != 1 || limit.ArchiveOpenConcurrency != 1 || limit.HashConcurrency != 1 || limit.CoverConcurrency != 1 {
		t.Fatalf("expected all IO concurrency limits to be 1, got %+v", limit)
	}
	if !limit.PauseBackgroundWhenReading || !limit.IdleOnlyHeavyTasks || !limit.DisableSameDiskPageCache {
		t.Fatalf("expected external HDD pause/cache flags, got %+v", limit)
	}
}

func TestTaskStatusTracksScrapeMetricsAndLabels(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "scrape_library_7"
	if !controller.startPausableCancelableTask(taskKey, "scrape", "running", 3) {
		t.Fatal("expected task to start")
	}
	controller.updateTaskDetails(taskKey, 1, 3, "写入审阅队列: Foo", "queueing_review", "Foo", map[string]int64{
		"total_series":         3,
		"processed_series":     1,
		"success_count":        1,
		"failed_count":         0,
		"not_found_count":      0,
		"queued_review_count":  1,
		"provider_requests":    1,
		"provider_errors":      0,
		"rate_limited_wait_ms": 500,
	}, map[string]string{
		"provider":            "bangumi",
		"provider_name":       "Bangumi",
		"current_series_id":   "42",
		"current_series_name": "Foo",
	})

	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if task.Phase != "queueing_review" || task.CurrentItem != "Foo" {
		t.Fatalf("expected scrape phase/current item, got %+v", task)
	}
	if task.Metrics["queued_review_count"] != 1 || task.Metrics["rate_limited_wait_ms"] != 500 {
		t.Fatalf("expected scrape metrics, got %+v", task.Metrics)
	}
	if task.Labels["provider_name"] != "Bangumi" || task.Labels["current_series_name"] != "Foo" {
		t.Fatalf("expected scrape labels, got %+v", task.Labels)
	}
}

func TestLibraryScrapePauseResumeStopsNewProviderRequests(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	provider := newBlockingMetadataProvider()
	controller.providerFactory = func(string) metadata.Provider {
		return provider
	}

	libPath := filepath.Join(tempDir, "library")
	if err := os.MkdirAll(libPath, 0o755); err != nil {
		t.Fatalf("mkdir library failed: %v", err)
	}
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{
		Name:                "Library",
		Path:                libPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
	})
	if err != nil {
		t.Fatalf("CreateLibrary failed: %v", err)
	}
	for _, name := range []string{"Alpha", "Beta"} {
		if _, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{
			LibraryID:    lib.ID,
			Name:         name,
			Path:         filepath.Join(libPath, name),
			Title:        sql.NullString{},
			Summary:      sql.NullString{},
			Publisher:    sql.NullString{},
			Status:       sql.NullString{},
			Rating:       sql.NullFloat64{},
			Language:     sql.NullString{},
			LockedFields: sql.NullString{},
			NameInitial:  database.SeriesInitial("", name),
		}); err != nil {
			t.Fatalf("CreateSeries %s failed: %v", name, err)
		}
	}

	if err := controller.launchLibraryScrapeTask(context.Background(), lib.ID, "test"); err != nil {
		t.Fatalf("launch library scrape failed: %v", err)
	}
	taskKey := "scrape_library_" + strconv.FormatInt(lib.ID, 10)

	first := waitForProviderRequest(t, provider.requests)
	if first != "Alpha" {
		t.Fatalf("expected first provider request Alpha, got %q", first)
	}

	pauseReq := requestWithRouteParam(http.MethodPost, "/api/system/tasks/"+taskKey+"/pause", nil, "taskKey", taskKey)
	pauseRec := httptest.NewRecorder()
	controller.pauseTask(pauseRec, pauseReq)
	if pauseRec.Code != http.StatusAccepted {
		t.Fatalf("expected pause 202, got %d body=%s", pauseRec.Code, pauseRec.Body.String())
	}

	provider.release <- struct{}{}
	assertNoProviderRequest(t, provider.requests, 150*time.Millisecond)

	controller.taskEngine.mutex.Lock()
	paused := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if paused.Status != "paused" || paused.Metrics["provider_requests"] != 1 {
		t.Fatalf("expected paused scrape after first request, got %+v", paused)
	}

	resumeReq := requestWithRouteParam(http.MethodPost, "/api/system/tasks/"+taskKey+"/resume", nil, "taskKey", taskKey)
	resumeRec := httptest.NewRecorder()
	controller.resumeTask(resumeRec, resumeReq)
	if resumeRec.Code != http.StatusAccepted {
		t.Fatalf("expected resume 202, got %d body=%s", resumeRec.Code, resumeRec.Body.String())
	}

	second := waitForProviderRequest(t, provider.requests)
	if second != "Beta" {
		t.Fatalf("expected second provider request Beta after resume, got %q", second)
	}
	provider.release <- struct{}{}
	waitForTaskStatus(t, controller, taskKey, "completed")

	controller.taskEngine.mutex.Lock()
	done := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	if done.Metrics["provider_requests"] != 2 || done.Metrics["processed_series"] != 2 {
		t.Fatalf("expected completed scrape metrics, got %+v", done.Metrics)
	}
}

func waitForProviderRequest(t testing.TB, requests <-chan string) string {
	t.Helper()
	select {
	case title := <-requests:
		return title
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider request")
		return ""
	}
}

func assertNoProviderRequest(t testing.TB, requests <-chan string, duration time.Duration) {
	t.Helper()
	select {
	case title := <-requests:
		t.Fatalf("expected no provider request while paused, got %q", title)
	case <-time.After(duration):
	}
}

func waitForTaskStatus(t testing.TB, controller *Controller, taskKey, status string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		controller.taskEngine.mutex.Lock()
		task := controller.taskEngine.tasks[taskKey]
		controller.taskEngine.mutex.Unlock()
		if task.Status == status {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks[taskKey]
	controller.taskEngine.mutex.Unlock()
	t.Fatalf("timed out waiting for task %s status %s, got %+v", taskKey, status, task)
}

func TestPauseTaskRejectsNonPausableTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "rebuild_index"
	if !controller.startTask(taskKey, "rebuild_index", "running", 1) {
		t.Fatal("expected task to start")
	}

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/rebuild_index/pause", nil, "taskKey", taskKey)
	rec := httptest.NewRecorder()
	controller.pauseTask(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCancelPausedTaskUnblocksCheckpoint(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "scan_library_42"
	if !controller.startPausableCancelableTask(taskKey, "scan_library", "running", 10) {
		t.Fatal("expected task to start")
	}
	ctx, cleanup := controller.newTaskContext(taskKey)
	defer cleanup()

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/scan_library_42/pause", nil, "taskKey", taskKey)
	rec := httptest.NewRecorder()
	controller.pauseTask(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected pause 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	waitDone := make(chan error, 1)
	go func() {
		waitDone <- taskcontrol.Wait(ctx)
	}()
	select {
	case err := <-waitDone:
		t.Fatalf("expected checkpoint to block while paused, got %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	req = requestWithRouteParam(http.MethodPost, "/api/system/tasks/scan_library_42/cancel", nil, "taskKey", taskKey)
	rec = httptest.NewRecorder()
	controller.cancelTask(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected cancel 202, got %d body=%s", rec.Code, rec.Body.String())
	}

	select {
	case err := <-waitDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected checkpoint cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("expected cancel to unblock paused checkpoint")
	}
}

func TestCancelTaskRejectsNonCancellableTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	taskKey := "rebuild_index"
	if !controller.startTask(taskKey, "rebuild_index", "running", 1) {
		t.Fatal("expected task to start")
	}

	req := requestWithRouteParam(http.MethodPost, "/api/system/tasks/rebuild_index/cancel", nil, "taskKey", taskKey)
	rec := httptest.NewRecorder()
	controller.cancelTask(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRebuildThumbnailsTaskRunsAsCancellableLowImpactTask(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	if err := controller.launchRebuildThumbnailsTask(); err != nil {
		t.Fatalf("launch rebuild thumbnails failed: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var task TaskStatus
	for time.Now().Before(deadline) {
		controller.taskEngine.mutex.Lock()
		task = controller.taskEngine.tasks["rebuild_thumbnails"]
		controller.taskEngine.mutex.Unlock()
		if task.Status != "running" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if task.Status != "completed" {
		t.Fatalf("expected thumbnail rebuild task to complete, got %+v", task)
	}
	if task.Params["execution_mode"] != "low_impact" {
		t.Fatalf("expected low impact task metadata, got %+v", task.Params)
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
	controller.taskEngine.mutex.Lock()
	task := controller.taskEngine.tasks["scan_series_999"]
	controller.taskEngine.mutex.Unlock()
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
		Name:                "Main",
		Path:                libPath,
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         config.DefaultScanFormatsCSV,
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

func TestUpdateBookProgressThrottlesRepeatedWrites(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 12)
	counting := &countingStore{Store: store}
	controller.store = counting

	postProgress := func(page int64) {
		t.Helper()
		req := requestWithRouteParam(http.MethodPost, "/api/books/1/progress", []byte(`{"page":`+strconv.FormatInt(page, 10)+`}`), "bookId", strconv.FormatInt(book.ID, 10))
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected progress update 200, got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	postProgress(6)
	postProgress(6)
	postProgress(4)

	if counting.updateBookProgressCalls != 2 {
		t.Fatalf("expected repeated same-page progress write to be throttled, got %d writes", counting.updateBookProgressCalls)
	}
	if counting.logReadingActivityCalls != 1 {
		t.Fatalf("expected reading activity only for forward progress, got %d calls", counting.logReadingActivityCalls)
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

func TestBulkUpdateSeriesProgressMarksAllBooksReadAndUnread(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, bookA := seedBookFixture(t, store, rootDir, "Lib", "Series", "book-a.cbz", 8)
	bookB, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "book-b.cbz",
		Path:           filepath.Join(series.Path, "book-b.cbz"),
		Size:           2048,
		FileModifiedAt: time.Now(),
		Volume:         "",
		Title:          sql.NullString{String: "Book B", Valid: true},
		PageCount:      12,
	})
	if err != nil {
		t.Fatalf("CreateBook B failed: %v", err)
	}

	readReq := httptest.NewRequest(http.MethodPost, "/api/series/bulk-progress", bytes.NewReader([]byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`],"is_read":true}`)))
	readRec := httptest.NewRecorder()
	controller.bulkUpdateSeriesProgress(readRec, readReq)
	if readRec.Code != http.StatusOK {
		t.Fatalf("expected 200 when marking series read, got %d body=%s", readRec.Code, readRec.Body.String())
	}

	updatedA, err := store.GetBook(context.Background(), bookA.ID)
	if err != nil {
		t.Fatalf("GetBook A failed: %v", err)
	}
	updatedB, err := store.GetBook(context.Background(), bookB.ID)
	if err != nil {
		t.Fatalf("GetBook B failed: %v", err)
	}
	if !updatedA.LastReadPage.Valid || updatedA.LastReadPage.Int64 != 8 {
		t.Fatalf("expected book A read page 8, got %+v", updatedA.LastReadPage)
	}
	if !updatedB.LastReadPage.Valid || updatedB.LastReadPage.Int64 != 12 {
		t.Fatalf("expected book B read page 12, got %+v", updatedB.LastReadPage)
	}

	unreadReq := httptest.NewRequest(http.MethodPost, "/api/series/bulk-progress", bytes.NewReader([]byte(`{"series_ids":[`+strconv.FormatInt(series.ID, 10)+`],"is_read":false}`)))
	unreadRec := httptest.NewRecorder()
	controller.bulkUpdateSeriesProgress(unreadRec, unreadReq)
	if unreadRec.Code != http.StatusOK {
		t.Fatalf("expected 200 when marking series unread, got %d body=%s", unreadRec.Code, unreadRec.Body.String())
	}

	updatedA, err = store.GetBook(context.Background(), bookA.ID)
	if err != nil {
		t.Fatalf("GetBook A after unread failed: %v", err)
	}
	updatedB, err = store.GetBook(context.Background(), bookB.ID)
	if err != nil {
		t.Fatalf("GetBook B after unread failed: %v", err)
	}
	if updatedA.LastReadPage.Valid || updatedB.LastReadPage.Valid {
		t.Fatalf("expected series unread to clear read pages, got A=%+v B=%+v", updatedA.LastReadPage, updatedB.LastReadPage)
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

func TestDashboardStatsCacheAvoidsRepeatedStoreQueriesAndInvalidates(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 12)
	counting := &countingStore{Store: store}
	controller.store = counting

	fetchStats := func() database.DashboardStats {
		t.Helper()
		rec := httptest.NewRecorder()
		controller.getDashboardStats(rec, httptest.NewRequest(http.MethodGet, "/api/stats/dashboard", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected dashboard stats 200, got %d body=%s", rec.Code, rec.Body.String())
		}
		var stats database.DashboardStats
		if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
			t.Fatalf("decode dashboard stats failed: %v", err)
		}
		return stats
	}

	first := fetchStats()
	second := fetchStats()
	if counting.structuralStatsCalls != 1 {
		t.Fatalf("expected one structural query for repeated reads, got %d", counting.structuralStatsCalls)
	}
	if counting.volatileStatsCalls != 1 {
		t.Fatalf("expected one volatile query for repeated reads, got %d", counting.volatileStatsCalls)
	}
	if first.TotalBooks != second.TotalBooks || second.ReadBooks != 0 {
		t.Fatalf("unexpected cached dashboard stats: first=%+v second=%+v", first, second)
	}

	controller.handleScannerBatchEvent("batch_inserted")
	_ = fetchStats()
	if counting.structuralStatsCalls != 2 {
		t.Fatalf("expected scanner batch event to invalidate structural cache, got %d structural calls", counting.structuralStatsCalls)
	}

	req := requestWithRouteParam(
		http.MethodPost,
		"/api/books/progress",
		[]byte(`{"page":5}`),
		"bookId",
		strconv.FormatInt(book.ID, 10),
	)
	rec := httptest.NewRecorder()
	controller.updateBookProgress(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected progress update 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	structuralBefore := counting.structuralStatsCalls
	afterProgress := fetchStats()
	// 阅读进度更新只失效阅读类缓存：结构性统计（含 books 全表扫描）不应被重新查询。
	if counting.structuralStatsCalls != structuralBefore {
		t.Fatalf("progress update should NOT invalidate structural cache: before=%d after=%d", structuralBefore, counting.structuralStatsCalls)
	}
	if afterProgress.ReadBooks != 1 {
		t.Fatalf("expected refreshed read book count 1, got %+v", afterProgress)
	}
}

func TestGetActivityHeatmapReturnsReadingData(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)

	if err := store.LogReadingActivity(context.Background(), database.LogReadingActivityParams{BookID: book.ID, PagesRead: 7}); err != nil {
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

func TestRunRebuildFileIdentitiesBackfillsQuickHash(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, tempDir, "Lib", "Series", "Book.cbz", 12)
	if err := os.WriteFile(book.Path, []byte("quick-hash-book-content"), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}

	updated, total, err := controller.runRebuildFileIdentities(context.Background(), 500, nil)
	if err != nil {
		t.Fatalf("runRebuildFileIdentities failed: %v", err)
	}
	if updated != 1 || total != 1 {
		t.Fatalf("expected one identity update, got updated=%d total=%d", updated, total)
	}

	got, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !got.QuickHash.Valid || got.QuickHash.String == "" {
		t.Fatalf("expected quick hash to be backfilled, got %+v", got.QuickHash)
	}
}

func TestRunBackfillFullHashesLowPriorityBackfillsFileHash(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, tempDir, "Lib", "Series", "Book.cbz", 12)
	if err := os.WriteFile(book.Path, []byte("full-hash-book-content"), 0o644); err != nil {
		t.Fatalf("write book file failed: %v", err)
	}

	var progressCalls int
	var lastMetrics taskIOMetrics
	updated, total, err := controller.runBackfillFullHashesLowPriority(context.Background(), 2, 0, func(current, total int, message string, metrics taskIOMetrics) {
		progressCalls++
		lastMetrics = metrics
	})
	if err != nil {
		t.Fatalf("runBackfillFullHashesLowPriority failed: %v", err)
	}
	if updated != 1 || total != 1 {
		t.Fatalf("expected one full hash update, got updated=%d total=%d", updated, total)
	}
	if progressCalls == 0 {
		t.Fatal("expected progress callback")
	}
	if lastMetrics.HashedFiles != 1 {
		t.Fatalf("expected one hashed file metric, got %+v", lastMetrics)
	}

	got, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("GetBook failed: %v", err)
	}
	if !got.FileHash.Valid || got.FileHash.String == "" {
		t.Fatalf("expected full hash to be backfilled, got %+v", got.FileHash)
	}
}

func TestReadingBookmarksLifecycle(t *testing.T) {
	controller, store, _, tempDir := newTestController(t)
	_, _, book := seedBookFixture(t, store, tempDir, "Lib", "Series", "Book.cbz", 12)

	createBody := []byte(`{"page":5,"note":"重要跨页"}`)
	createReq := requestWithRouteParam(http.MethodPost, "/api/books/1/bookmarks", createBody, "bookId", strconv.FormatInt(book.ID, 10))
	createRec := httptest.NewRecorder()
	controller.upsertReadingBookmark(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected bookmark create 200, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created database.ReadingBookmark
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode bookmark failed: %v", err)
	}
	if created.Page != 5 || created.Note != "重要跨页" {
		t.Fatalf("unexpected bookmark payload: %+v", created)
	}

	listReq := requestWithRouteParam(http.MethodGet, "/api/books/1/bookmarks", nil, "bookId", strconv.FormatInt(book.ID, 10))
	listRec := httptest.NewRecorder()
	controller.listReadingBookmarks(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected bookmark list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed []database.ReadingBookmark
	if err := json.NewDecoder(listRec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode bookmark list failed: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("expected one created bookmark, got %+v", listed)
	}

	deleteReq := requestWithRouteParams(http.MethodDelete, "/api/books/1/bookmarks/1", nil, map[string]string{
		"bookId":     strconv.FormatInt(book.ID, 10),
		"bookmarkId": strconv.FormatInt(created.ID, 10),
	})
	deleteRec := httptest.NewRecorder()
	controller.deleteReadingBookmark(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected bookmark delete 200, got %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	remaining, err := store.ListReadingBookmarks(context.Background(), book.ID)
	if err != nil {
		t.Fatalf("ListReadingBookmarks failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected no bookmarks after delete, got %+v", remaining)
	}
}

func TestApplyScrapedMetadataQueuesReviewThenAppliesExplicitly(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)

	payload := []byte(`{
		"Title":"Updated Title",
		"Summary":"Updated summary",
		"Publisher":"Kodansha",
		"Rating":8.6,
		"Tags":["Action","Drama"],
		"SourceID":12345,
		"SourceURL":"https://bgm.tv/subject/12345",
		"Confidence":0.92
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

	var queued struct {
		Queued     bool  `json:"queued"`
		ReviewID   int64 `json:"review_id"`
		FieldCount int   `json:"field_count"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&queued); err != nil {
		t.Fatalf("decode queued response failed: %v", err)
	}
	if !queued.Queued || queued.ReviewID == 0 || queued.FieldCount == 0 {
		t.Fatalf("expected metadata review to be queued, got %+v", queued)
	}

	updated, err := store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries failed: %v", err)
	}
	if updated.Title.Valid || updated.Publisher.Valid {
		t.Fatalf("expected scraped metadata not to overwrite before review, got title=%+v publisher=%+v", updated.Title, updated.Publisher)
	}

	listRec := httptest.NewRecorder()
	controller.listSeriesMetadataReview(listRec, requestWithRouteParam(
		http.MethodGet,
		"/api/series/1/metadata-review",
		nil,
		"seriesId",
		strconv.FormatInt(series.ID, 10),
	))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected metadata review list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listPayload metadataReviewResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode review list failed: %v", err)
	}
	if len(listPayload.Reviews) != 1 || len(listPayload.Reviews[0].Fields) == 0 {
		t.Fatalf("expected pending review fields, got %+v", listPayload)
	}

	applyRec := httptest.NewRecorder()
	controller.applyMetadataReview(applyRec, requestWithRouteParam(
		http.MethodPost,
		"/api/metadata/reviews/1/apply",
		nil,
		"reviewId",
		strconv.FormatInt(queued.ReviewID, 10),
	))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected metadata review apply 200, got %d body=%s", applyRec.Code, applyRec.Body.String())
	}

	updated, err = store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries after apply failed: %v", err)
	}
	if !updated.Title.Valid || updated.Title.String != "Updated Title" {
		t.Fatalf("expected updated title after review apply, got %+v", updated.Title)
	}
	if !updated.Publisher.Valid || updated.Publisher.String != "Kodansha" {
		t.Fatalf("expected updated publisher after review apply, got %+v", updated.Publisher)
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

	review, err := store.GetMetadataReview(context.Background(), queued.ReviewID)
	if err != nil {
		t.Fatalf("GetMetadataReview failed: %v", err)
	}
	if review.Status != "applied" {
		t.Fatalf("expected applied review status, got %+v", review)
	}
	provenance, err := store.GetSeriesMetadataProvenance(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeriesMetadataProvenance failed: %v", err)
	}
	if len(provenance) == 0 {
		t.Fatal("expected provenance rows after review apply")
	}
	foundTitleProvenance := false
	for _, row := range provenance {
		if row.FieldName == "title" {
			foundTitleProvenance = row.Source == "bangumi" && row.ReviewID.Valid && row.ReviewID.Int64 == queued.ReviewID
		}
	}
	if !foundTitleProvenance {
		t.Fatalf("expected title provenance tied to review %d, got %+v", queued.ReviewID, provenance)
	}
}

func TestQueueMetadataReviewDeduplicatesPendingEquivalentContent(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)
	info, err := store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries failed: %v", err)
	}

	result := &metadata.SeriesMetadata{
		Provider:   "bangumi",
		Title:      "Updated Title",
		Summary:    "Updated summary",
		Publisher:  "Kodansha",
		Tags:       []string{"Action", "Drama"},
		SourceID:   12345,
		SourceURL:  "https://bgm.tv/subject/12345",
		Confidence: 0.92,
	}
	firstReview, firstFields, _, err := controller.queueMetadataReview(context.Background(), info, result, "bangumi", "Series")
	if err != nil {
		t.Fatalf("first queue metadata review failed: %v", err)
	}
	secondReview, secondFields, _, err := controller.queueMetadataReview(context.Background(), info, result, "bangumi", "Series")
	if err != nil {
		t.Fatalf("second queue metadata review failed: %v", err)
	}
	if secondReview.ID != firstReview.ID {
		t.Fatalf("expected duplicate queue to reuse review %d, got %d", firstReview.ID, secondReview.ID)
	}
	if len(secondFields) != len(firstFields) {
		t.Fatalf("expected duplicate queue to return existing fields, first=%d second=%d", len(firstFields), len(secondFields))
	}

	pending, err := store.ListPendingMetadataReviewsBySeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("ListPendingMetadataReviewsBySeries failed: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected one pending review after duplicate queue, got %+v", pending)
	}
}

// TestQueueMetadataReviewKeepsDistinctSourcesSeparate 验证：字段 diff 相同但来源条目（SourceID）
// 不同的两次提交不会被去重复用，而是各自独立入队并保留自己的 source_url。
// 复现 bug：选中第 3 条候选应用后，审核页超链接却指向第 1 条。
func TestQueueMetadataReviewKeepsDistinctSourcesSeparate(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 10)
	info, err := store.GetSeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("GetSeries failed: %v", err)
	}

	// 两条候选的字段值完全一致，仅 SourceID/SourceURL 不同（同一作品的不同 Bangumi 条目）。
	first := &metadata.SeriesMetadata{
		Provider:  "bangumi",
		Title:     "Same Title",
		Summary:   "Same summary",
		SourceID:  111,
		SourceURL: "https://bgm.tv/subject/111",
	}
	third := &metadata.SeriesMetadata{
		Provider:  "bangumi",
		Title:     "Same Title",
		Summary:   "Same summary",
		SourceID:  333,
		SourceURL: "https://bgm.tv/subject/333",
	}

	firstReview, _, firstIsNew, err := controller.queueMetadataReview(context.Background(), info, first, "bangumi", "Series")
	if err != nil {
		t.Fatalf("queue first review failed: %v", err)
	}
	if !firstIsNew {
		t.Fatal("expected first review to be newly created")
	}
	thirdReview, _, thirdIsNew, err := controller.queueMetadataReview(context.Background(), info, third, "bangumi", "Series")
	if err != nil {
		t.Fatalf("queue third review failed: %v", err)
	}
	if !thirdIsNew {
		t.Fatal("expected third review to be newly created, not deduplicated against the first")
	}
	if thirdReview.ID == firstReview.ID {
		t.Fatalf("expected distinct source to create a new review, but reused id %d", firstReview.ID)
	}
	if thirdReview.SourceUrl != "https://bgm.tv/subject/333" {
		t.Fatalf("expected third review to keep its own source_url, got %q", thirdReview.SourceUrl)
	}

	pending, err := store.ListPendingMetadataReviewsBySeries(context.Background(), series.ID)
	if err != nil {
		t.Fatalf("ListPendingMetadataReviewsBySeries failed: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected two pending reviews for distinct sources, got %d", len(pending))
	}
}

func TestMetadataReviewInboxBulkApplyAndReject(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, seriesA, _ := seedBookFixture(t, store, t.TempDir(), "LibA", "Series Alpha", "book-a.cbz", 10)
	_, seriesB, _ := seedBookFixture(t, store, t.TempDir(), "LibB", "Series Beta", "book-b.cbz", 10)

	if _, err := controller.store.(*database.SqlStore).DB().Exec(`
		UPDATE series SET title = ?, publisher = ? WHERE id = ?
	`, "Existing Title", "", seriesA.ID); err != nil {
		t.Fatalf("seed series A metadata failed: %v", err)
	}
	seriesA, err := store.GetSeries(context.Background(), seriesA.ID)
	if err != nil {
		t.Fatalf("GetSeries A failed: %v", err)
	}

	reviewA, _, _, err := controller.queueMetadataReview(context.Background(), seriesA, &metadata.SeriesMetadata{
		Title:      "External Title",
		Publisher:  "External Publisher",
		Summary:    "External summary",
		SourceID:   1,
		SourceURL:  "https://example.test/a",
		Provider:   "bangumi",
		Confidence: 0.7,
	}, "bangumi", "Series Alpha")
	if err != nil {
		t.Fatalf("queue review A failed: %v", err)
	}
	reviewB, _, _, err := controller.queueMetadataReview(context.Background(), seriesB, &metadata.SeriesMetadata{
		Title:      "Beta Title",
		Publisher:  "Beta Publisher",
		SourceID:   2,
		SourceURL:  "https://example.test/b",
		Provider:   "bangumi",
		Confidence: 0.8,
	}, "bangumi", "Series Beta")
	if err != nil {
		t.Fatalf("queue review B failed: %v", err)
	}

	listRec := httptest.NewRecorder()
	controller.listMetadataReviewInbox(listRec, httptest.NewRequest(http.MethodGet, "/api/metadata/reviews?limit=10", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("expected inbox list 200, got %d body=%s", listRec.Code, listRec.Body.String())
	}
	var inbox metadataReviewInboxResponse
	if err := json.NewDecoder(listRec.Body).Decode(&inbox); err != nil {
		t.Fatalf("decode inbox failed: %v", err)
	}
	if inbox.Total != 2 || len(inbox.Items) != 2 {
		t.Fatalf("expected two inbox items, got %+v", inbox)
	}

	applyBody := []byte(`{"review_ids":[` + strconv.FormatInt(reviewA.ID, 10) + `],"mode":"fill_empty"}`)
	applyRec := httptest.NewRecorder()
	controller.bulkApplyMetadataReviews(applyRec, httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-apply", bytes.NewReader(applyBody)))
	if applyRec.Code != http.StatusOK {
		t.Fatalf("expected bulk apply 200, got %d body=%s", applyRec.Code, applyRec.Body.String())
	}
	var applyResp metadataReviewBulkResponse
	if err := json.NewDecoder(applyRec.Body).Decode(&applyResp); err != nil {
		t.Fatalf("decode bulk apply failed: %v", err)
	}
	if len(applyResp.Applied) != 1 || len(applyResp.Failed) != 0 {
		t.Fatalf("unexpected bulk apply response: %+v", applyResp)
	}

	updatedA, err := store.GetSeries(context.Background(), seriesA.ID)
	if err != nil {
		t.Fatalf("GetSeries A after bulk apply failed: %v", err)
	}
	if !updatedA.Title.Valid || updatedA.Title.String != "Existing Title" {
		t.Fatalf("expected fill_empty not to overwrite title, got %+v", updatedA.Title)
	}
	if !updatedA.Publisher.Valid || updatedA.Publisher.String != "External Publisher" {
		t.Fatalf("expected fill_empty to set empty publisher, got %+v", updatedA.Publisher)
	}
	if !updatedA.Summary.Valid || updatedA.Summary.String != "External summary" {
		t.Fatalf("expected fill_empty to set empty summary, got %+v", updatedA.Summary)
	}
	provenance, err := store.GetSeriesMetadataProvenance(context.Background(), seriesA.ID)
	if err != nil {
		t.Fatalf("GetSeriesMetadataProvenance failed: %v", err)
	}
	for _, row := range provenance {
		if row.FieldName == "title" && row.ReviewID.Valid && row.ReviewID.Int64 == reviewA.ID {
			t.Fatalf("expected title provenance not to point at fill_empty review, got %+v", row)
		}
	}

	rejectBody := []byte(`{"review_ids":[` + strconv.FormatInt(reviewB.ID, 10) + `],"mode":"all"}`)
	rejectRec := httptest.NewRecorder()
	controller.bulkRejectMetadataReviews(rejectRec, httptest.NewRequest(http.MethodPost, "/api/metadata/reviews/bulk-reject", bytes.NewReader(rejectBody)))
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected bulk reject 200, got %d body=%s", rejectRec.Code, rejectRec.Body.String())
	}
	rejected, err := store.GetMetadataReview(context.Background(), reviewB.ID)
	if err != nil {
		t.Fatalf("GetMetadataReview B failed: %v", err)
	}
	if rejected.Status != "rejected" {
		t.Fatalf("expected review B rejected, got %+v", rejected)
	}
}

func TestGetRecommendationsReturnsCachedEntries(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	controller.recommendationsMutex.Lock()
	if controller.recommendationsCache == nil {
		controller.recommendationsCache = make(map[string][]AIRecommendationResponse)
	}
	if controller.recommendationsCacheTime == nil {
		controller.recommendationsCacheTime = make(map[string]time.Time)
	}
	controller.recommendationsCache["zh-CN"] = []AIRecommendationResponse{{
		SeriesID:  99,
		Reason:    "Cached reason",
		Title:     "Cached title",
		CoverPath: "cached.webp",
	}}
	controller.recommendationsCacheTime["zh-CN"] = time.Now()
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

// --- 阶段 0 后端调整：上一本 / 系列续读 / 批量进度同步 ---

func seedBookInSeries(t testing.TB, store database.Store, series database.Series, libraryID int64, name string, pageCount int64) database.Book {
	t.Helper()
	book, err := store.CreateBook(context.Background(), database.CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      libraryID,
		Name:           name,
		Path:           filepath.Join(series.Path, name),
		Size:           1024,
		FileModifiedAt: time.Now(),
		Volume:         "",
		Title:          sql.NullString{String: name, Valid: true},
		PageCount:      pageCount,
	})
	if err != nil {
		t.Fatalf("CreateBook %s failed: %v", name, err)
	}
	return book
}

func TestGetPrevBookReturnsPriorEntry(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series", "01.cbz", 10)
	secondBook := seedBookInSeries(t, store, series, lib.ID, "02.cbz", 10)
	thirdBook := seedBookInSeries(t, store, series, lib.ID, "03.cbz", 10)

	prevRec := httptest.NewRecorder()
	controller.getPrevBook(prevRec, requestWithRouteParam(http.MethodGet, "/api/book-prev/2", nil, "bookId", strconv.FormatInt(secondBook.ID, 10)))
	if prevRec.Code != http.StatusOK {
		t.Fatalf("expected prev 200, got %d body=%s", prevRec.Code, prevRec.Body.String())
	}
	var prev database.Book
	if err := json.NewDecoder(prevRec.Body).Decode(&prev); err != nil {
		t.Fatalf("decode prev: %v", err)
	}
	if prev.ID != firstBook.ID {
		t.Fatalf("expected first book as prev of second, got %d", prev.ID)
	}

	// 第一本无前一本
	prevFirst := httptest.NewRecorder()
	controller.getPrevBook(prevFirst, requestWithRouteParam(http.MethodGet, "/api/book-prev/1", nil, "bookId", strconv.FormatInt(firstBook.ID, 10)))
	if prevFirst.Code != http.StatusNotFound {
		t.Fatalf("expected first book to have no prev (404), got %d", prevFirst.Code)
	}

	// 末尾本前一本是中间本
	prevLast := httptest.NewRecorder()
	controller.getPrevBook(prevLast, requestWithRouteParam(http.MethodGet, "/api/book-prev/3", nil, "bookId", strconv.FormatInt(thirdBook.ID, 10)))
	if prevLast.Code != http.StatusOK {
		t.Fatalf("expected prev of third 200, got %d", prevLast.Code)
	}
	var prevOfLast database.Book
	if err := json.NewDecoder(prevLast.Body).Decode(&prevOfLast); err != nil {
		t.Fatalf("decode prev: %v", err)
	}
	if prevOfLast.ID != secondBook.ID {
		t.Fatalf("expected second book as prev of third, got %d", prevOfLast.ID)
	}

	// 非法 ID
	bad := httptest.NewRecorder()
	controller.getPrevBook(bad, requestWithRouteParam(http.MethodGet, "/api/book-prev/bad", nil, "bookId", "bad"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", bad.Code)
	}

	// 不存在 ID
	missing := httptest.NewRecorder()
	controller.getPrevBook(missing, requestWithRouteParam(http.MethodGet, "/api/book-prev/9999", nil, "bookId", "9999"))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing book, got %d", missing.Code)
	}
}

func TestGetSeriesContinueAcrossStates(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series", "01.cbz", 10)
	secondBook := seedBookInSeries(t, store, series, lib.ID, "02.cbz", 10)
	thirdBook := seedBookInSeries(t, store, series, lib.ID, "03.cbz", 10)

	// 全部未读：next_unread = first；read_books = 0；last_read_* 为零
	rec := httptest.NewRecorder()
	controller.getSeriesContinueEndpoint(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/continue", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected continue 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var c1 SeriesContinue
	if err := json.NewDecoder(rec.Body).Decode(&c1); err != nil {
		t.Fatalf("decode continue: %v", err)
	}
	if c1.NextUnreadBookID != firstBook.ID {
		t.Fatalf("expected next_unread=first, got %d", c1.NextUnreadBookID)
	}
	if c1.ReadBooks != 0 || c1.TotalBooks != 3 {
		t.Fatalf("unexpected counts: %+v", c1)
	}
	if c1.LastReadBookID != 0 || c1.LastReadAt != nil {
		t.Fatalf("expected last_read empty, got %+v", c1)
	}

	// 把第一本标完成；第二本读到第 5 页（未读完）；第三本未读
	now := time.Now()
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 10, Valid: true},
		LastReadAt:   sql.NullTime{Time: now.Add(-2 * time.Hour), Valid: true},
		ID:           firstBook.ID,
	}); err != nil {
		t.Fatalf("update first progress: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 5, Valid: true},
		LastReadAt:   sql.NullTime{Time: now.Add(-1 * time.Hour), Valid: true},
		ID:           secondBook.ID,
	}); err != nil {
		t.Fatalf("update second progress: %v", err)
	}
	rec = httptest.NewRecorder()
	controller.getSeriesContinueEndpoint(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/continue", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var c2 SeriesContinue
	if err := json.NewDecoder(rec.Body).Decode(&c2); err != nil {
		t.Fatalf("decode continue: %v", err)
	}
	if c2.NextUnreadBookID != secondBook.ID {
		t.Fatalf("expected next_unread=second, got %d", c2.NextUnreadBookID)
	}
	if c2.LastReadBookID != secondBook.ID {
		t.Fatalf("expected last_read=second (most recent), got %d", c2.LastReadBookID)
	}
	if c2.LastReadPage != 5 {
		t.Fatalf("expected last_read_page=5, got %d", c2.LastReadPage)
	}
	if c2.ReadBooks != 1 {
		t.Fatalf("expected read_books=1, got %d", c2.ReadBooks)
	}
	if c2.ReadPages != 15 {
		t.Fatalf("expected read_pages=15 (10+5), got %d", c2.ReadPages)
	}
	if c2.TotalPages != 30 {
		t.Fatalf("expected total_pages=30, got %d", c2.TotalPages)
	}

	// 全部读完
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 10, Valid: true},
		LastReadAt:   sql.NullTime{Time: now.Add(-30 * time.Minute), Valid: true},
		ID:           secondBook.ID,
	}); err != nil {
		t.Fatalf("update second progress: %v", err)
	}
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 10, Valid: true},
		LastReadAt:   sql.NullTime{Time: now, Valid: true},
		ID:           thirdBook.ID,
	}); err != nil {
		t.Fatalf("update third progress: %v", err)
	}
	rec = httptest.NewRecorder()
	controller.getSeriesContinueEndpoint(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/continue", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	var c3 SeriesContinue
	if err := json.NewDecoder(rec.Body).Decode(&c3); err != nil {
		t.Fatalf("decode continue: %v", err)
	}
	if c3.NextUnreadBookID != 0 {
		t.Fatalf("expected no next_unread when all read, got %d", c3.NextUnreadBookID)
	}
	if c3.ReadBooks != 3 {
		t.Fatalf("expected read_books=3, got %d", c3.ReadBooks)
	}
	if c3.LastReadBookID != thirdBook.ID {
		t.Fatalf("expected last_read=third (most recent), got %d", c3.LastReadBookID)
	}

	// 非法 ID
	bad := httptest.NewRecorder()
	controller.getSeriesContinueEndpoint(bad, requestWithRouteParam(http.MethodGet, "/api/series/bad/continue", nil, "seriesId", "bad"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid id, got %d", bad.Code)
	}

	// 不存在的系列
	missing := httptest.NewRecorder()
	controller.getSeriesContinueEndpoint(missing, requestWithRouteParam(http.MethodGet, "/api/series/9999/continue", nil, "seriesId", "9999"))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing series, got %d", missing.Code)
	}
}

func TestGetSeriesContextIncludesContinue(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series", "01.cbz", 10)
	_ = seedBookInSeries(t, store, series, lib.ID, "02.cbz", 10)

	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 4, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           firstBook.ID,
	}); err != nil {
		t.Fatalf("update progress: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.getSeriesContext(rec, requestWithRouteParam(http.MethodGet, "/api/series/1/context", nil, "seriesId", strconv.FormatInt(series.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected context 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp SeriesContextResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode context: %v", err)
	}
	if resp.Continue.NextUnreadBookID != firstBook.ID {
		t.Fatalf("expected continue.next_unread=first, got %+v", resp.Continue)
	}
	if resp.Continue.LastReadBookID != firstBook.ID || resp.Continue.LastReadPage != 4 {
		t.Fatalf("expected continue last_read=first/page4, got %+v", resp.Continue)
	}
	if resp.Continue.TotalBooks != 2 || resp.Continue.TotalPages != 20 {
		t.Fatalf("unexpected continue totals: %+v", resp.Continue)
	}
}

func TestBulkSyncBookProgressResolvesConflicts(t *testing.T) {
	controller, store, _, rootDir := newTestController(t)
	lib, series, firstBook := seedBookFixture(t, store, rootDir, "Library A", "Series", "01.cbz", 10)
	secondBook := seedBookInSeries(t, store, series, lib.ID, "02.cbz", 10)

	// 服务器先记录 second 已读到第 7 页（较新）
	serverNewer := time.Now()
	if err := store.UpdateBookProgress(context.Background(), database.UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 7, Valid: true},
		LastReadAt:   sql.NullTime{Time: serverNewer, Valid: true},
		ID:           secondBook.ID,
	}); err != nil {
		t.Fatalf("seed server progress: %v", err)
	}

	clientStale := serverNewer.Add(-1 * time.Hour)
	clientNew := serverNewer.Add(10 * time.Minute)

	body := struct {
		Items []BulkSyncProgressItem `json:"items"`
	}{
		Items: []BulkSyncProgressItem{
			{BookID: firstBook.ID, Page: 5, UpdatedAt: &clientNew},    // 首次写入应被接受
			{BookID: secondBook.ID, Page: 3, UpdatedAt: &clientStale}, // 客户端时间戳更早，应跳过
			{BookID: 9999, Page: 1, UpdatedAt: &clientNew},            // 不存在
			{BookID: 0, Page: 1}, // 非法
		},
	}
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	rec := httptest.NewRecorder()
	controller.bulkSyncBookProgress(rec, httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress/sync", bytes.NewReader(payload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Updated int                          `json:"updated"`
		Results []BulkSyncProgressResultItem `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Updated != 1 {
		t.Fatalf("expected 1 updated, got %d (results=%+v)", resp.Updated, resp.Results)
	}
	if len(resp.Results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(resp.Results))
	}
	wantStatuses := map[int64]string{
		firstBook.ID:  "updated",
		secondBook.ID: "skipped_stale",
		9999:          "not_found",
		0:             "invalid",
	}
	for _, r := range resp.Results {
		want, ok := wantStatuses[r.BookID]
		if !ok {
			t.Fatalf("unexpected result book_id=%d", r.BookID)
		}
		if r.Status != want {
			t.Fatalf("book %d: expected %s, got %s (msg=%s)", r.BookID, want, r.Status, r.Message)
		}
	}

	// 验证 server 状态：first 写入到 5；second 仍是 7
	gotFirst, err := store.GetBook(context.Background(), firstBook.ID)
	if err != nil {
		t.Fatalf("get first: %v", err)
	}
	if !gotFirst.LastReadPage.Valid || gotFirst.LastReadPage.Int64 != 5 {
		t.Fatalf("expected first.last_read_page=5, got %+v", gotFirst.LastReadPage)
	}
	gotSecond, err := store.GetBook(context.Background(), secondBook.ID)
	if err != nil {
		t.Fatalf("get second: %v", err)
	}
	if !gotSecond.LastReadPage.Valid || gotSecond.LastReadPage.Int64 != 7 {
		t.Fatalf("expected second.last_read_page=7 (untouched), got %+v", gotSecond.LastReadPage)
	}

	// 重发同一笔（updated_at 相同）应被相同页节流为 skipped_unchanged
	bodyAgain, _ := json.Marshal(struct {
		Items []BulkSyncProgressItem `json:"items"`
	}{
		Items: []BulkSyncProgressItem{
			{BookID: firstBook.ID, Page: 5, UpdatedAt: &clientNew},
		},
	})
	recAgain := httptest.NewRecorder()
	controller.bulkSyncBookProgress(recAgain, httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress/sync", bytes.NewReader(bodyAgain)))
	var respAgain struct {
		Updated int                          `json:"updated"`
		Results []BulkSyncProgressResultItem `json:"results"`
	}
	if err := json.NewDecoder(recAgain.Body).Decode(&respAgain); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if respAgain.Updated != 0 || len(respAgain.Results) != 1 || respAgain.Results[0].Status != "skipped_unchanged" {
		t.Fatalf("expected skipped_unchanged on resend, got %+v", respAgain)
	}
}

func TestBulkSyncBookProgressEmptyBody(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.bulkSyncBookProgress(rec, httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress/sync", bytes.NewReader([]byte(`{"items":[]}`))))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Updated != 0 {
		t.Fatalf("expected updated=0, got %d", resp.Updated)
	}
}

func TestKOReaderSelfRegistrationCreatesAuthenticatableAccount(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	// 开启同步并允许设备自助注册
	settingsBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": true,
		"match_mode": "binary_hash",
		"path_ignore_extension": false
	}`)
	settingsRec := httptest.NewRecorder()
	controller.updateKOReaderSettings(settingsRec, httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(settingsBody)))
	if settingsRec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", settingsRec.Code, settingsRec.Body.String())
	}

	// KOReader 客户端提交的 password 是用户密钥的 md5，与后续 x-auth-key 同值
	password := koreader.HashKey("device-secret")
	createBody := []byte(`{"username":"kobo","password":"` + password + `"}`)

	createRec := httptest.NewRecorder()
	controller.koreaderRegister(createRec, httptest.NewRequest(http.MethodPost, "/koreader/users/create", bytes.NewReader(createBody)))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected create 201, got %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]string
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create body failed: %v", err)
	}
	if created["username"] != "kobo" {
		t.Fatalf("expected username kobo in response, got %+v", created)
	}

	// 用同一个 md5 作为 x-auth-key 应能通过认证
	authReq := httptest.NewRequest(http.MethodGet, "/koreader/users/auth", nil)
	authReq.Header.Set("x-auth-user", "kobo")
	authReq.Header.Set("x-auth-key", password)
	authRec := httptest.NewRecorder()
	controller.koreaderAuth(authRec, authReq)
	if authRec.Code != http.StatusOK {
		t.Fatalf("expected auth 200, got %d body=%s", authRec.Code, authRec.Body.String())
	}

	// 重复注册返回 402
	dupRec := httptest.NewRecorder()
	controller.koreaderRegister(dupRec, httptest.NewRequest(http.MethodPost, "/koreader/users/create", bytes.NewReader(createBody)))
	if dupRec.Code != http.StatusPaymentRequired {
		t.Fatalf("expected duplicate create 402, got %d body=%s", dupRec.Code, dupRec.Body.String())
	}

	// 关闭自助注册后应返回 403
	closedBody := []byte(`{
		"enabled": true,
		"base_path": "/koreader",
		"allow_registration": false,
		"match_mode": "binary_hash",
		"path_ignore_extension": false
	}`)
	closedSettingsRec := httptest.NewRecorder()
	controller.updateKOReaderSettings(closedSettingsRec, httptest.NewRequest(http.MethodPost, "/api/system/koreader", bytes.NewReader(closedBody)))
	if closedSettingsRec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d", closedSettingsRec.Code)
	}
	closedRec := httptest.NewRecorder()
	controller.koreaderRegister(closedRec, httptest.NewRequest(http.MethodPost, "/koreader/users/create", bytes.NewReader([]byte(`{"username":"other","password":"`+password+`"}`))))
	if closedRec.Code != http.StatusForbidden {
		t.Fatalf("expected registration-disabled 403, got %d body=%s", closedRec.Code, closedRec.Body.String())
	}
}

func TestTaskProgressAsyncPersistMemoryWins(t *testing.T) {
	controller, store, _, _ := newTestController(t)

	if !controller.startTask("scan_library_5", "scan_library", "lib 5", 100) {
		t.Fatal("expected task to start")
	}
	controller.updateTask("scan_library_5", 42, 100, "processing")

	// 进度改为异步落盘（M42）：listTaskStatuses 应立即反映内存里的最新进度，
	// 而不是尚未刷盘（此时为空/滞后）的 DB 记录。
	tasks, err := controller.listTaskStatuses(context.Background(), database.TaskFilters{})
	if err != nil {
		t.Fatalf("listTaskStatuses failed: %v", err)
	}
	var mem *TaskStatus
	for i := range tasks {
		if tasks[i].Key == "scan_library_5" {
			mem = &tasks[i]
		}
	}
	if mem == nil || mem.Current != 42 {
		t.Fatalf("expected in-memory current 42 before flush, got %+v", mem)
	}

	// 刷盘后 DB 记录也应带上该进度。
	controller.flushTaskPersist()
	records, err := store.ListTasks(context.Background(), database.TaskFilters{})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	persisted := false
	for _, r := range records {
		ts := taskStatusFromRecord(r)
		if ts.Key == "scan_library_5" {
			persisted = true
			if ts.Current != 42 {
				t.Fatalf("expected persisted current 42, got %d", ts.Current)
			}
		}
	}
	if !persisted {
		t.Fatal("expected task persisted to DB after flush")
	}
}

func TestRetryTaskErrorSemantics(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	// 不存在的任务 -> 404
	rec := httptest.NewRecorder()
	controller.retryTask(rec, requestWithRouteParam(http.MethodPost, "/x", nil, "taskKey", "does_not_exist_1"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nonexistent task: expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 运行中的任务 -> 409
	if !controller.startTask("scan_series_5", "scan_series", "running", 1) {
		t.Fatal("expected task to start")
	}
	rec = httptest.NewRecorder()
	controller.retryTask(rec, requestWithRouteParam(http.MethodPost, "/x", nil, "taskKey", "scan_series_5"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("running task: expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}

	// 内部错误（scan_library 指向不存在的库，GetLibrary 失败）-> 500；此前所有重试失败一律误报 409。
	if !controller.startTask("scan_library_77777", "scan_library", "failed", 1) {
		t.Fatal("expected task to start")
	}
	controller.failTask("scan_library_77777", "failed")
	rec = httptest.NewRecorder()
	controller.retryTask(rec, requestWithRouteParam(http.MethodPost, "/x", nil, "taskKey", "scan_library_77777"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("internal error retry: expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIsRetryableTaskTypeDerivedFromRegistry(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	if len(controller.taskEngine.relaunchers) == 0 {
		t.Fatal("expected registered relaunchers")
	}
	for taskType := range controller.taskEngine.relaunchers {
		if !controller.isRetryableTaskType(taskType) {
			t.Fatalf("registered type %q should be retryable", taskType)
		}
	}
	if controller.isRetryableTaskType("nonexistent_type") {
		t.Fatal("unregistered type should not be retryable")
	}
}

func TestTaskMessageCodeEmission(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	// i18n 路径：finishTaskMsg 设置 message_code + message_params，并清空 Message。
	if !controller.startTask("scan_library_9", "scan_library", "start", 1) {
		t.Fatal("expected task to start")
	}
	controller.finishTaskMsg("scan_library_9", "task.msg.scan_library.complete", map[string]string{"name": "Lib A"})
	controller.taskEngine.mutex.Lock()
	coded := controller.taskEngine.tasks["scan_library_9"]
	controller.taskEngine.mutex.Unlock()
	if coded.MessageCode != "task.msg.scan_library.complete" {
		t.Fatalf("expected message_code set, got %q", coded.MessageCode)
	}
	if coded.MessageParams["name"] != "Lib A" {
		t.Fatalf("expected message_params name=Lib A, got %v", coded.MessageParams)
	}
	if coded.Message != "" {
		t.Fatalf("expected Message cleared for coded task, got %q", coded.Message)
	}
	if coded.Status != "completed" {
		t.Fatalf("expected status completed, got %q", coded.Status)
	}

	// 兼容路径：未迁移的 finishTask 仍直接设 Message，并清空 code（互斥）。
	controller.startTask("scan_library_10", "scan_library", "start", 1)
	controller.finishTask("scan_library_10", "直接文案")
	controller.taskEngine.mutex.Lock()
	legacy := controller.taskEngine.tasks["scan_library_10"]
	controller.taskEngine.mutex.Unlock()
	if legacy.Message != "直接文案" || legacy.MessageCode != "" {
		t.Fatalf("legacy finishTask: want Message set & code empty, got msg=%q code=%q", legacy.Message, legacy.MessageCode)
	}
}

func TestTaskMessageCodePersistRoundTrip(t *testing.T) {
	// 编码任务的 message_code/params 需经 Params 往返 DB 记录，否则已完成任务读回后丢失文案。
	task := TaskStatus{
		Key:           "scan_library_3",
		Type:          "scan_library",
		Status:        "completed",
		MessageCode:   "task.msg.scan_library.complete",
		MessageParams: map[string]string{"name": "Alpha"},
	}
	record := taskRecordFromStatus(task)
	restored := taskStatusFromRecord(record)
	if restored.MessageCode != "task.msg.scan_library.complete" {
		t.Fatalf("message_code lost across persistence, got %q", restored.MessageCode)
	}
	if restored.MessageParams["name"] != "Alpha" {
		t.Fatalf("message_params lost across persistence, got %v", restored.MessageParams)
	}
}
