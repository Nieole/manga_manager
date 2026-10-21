package api

import (
	"context"
	"encoding/json"
	"errors"
	"manga-manager/internal/scanner"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

func TestRestorationRecheckIsReadOnlyAndReleaseRequiresCurrentRiskConsent(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "source", "series", "missing.cbz", 1)
	if _, err := store.DB().Exec(`UPDATE libraries SET scan_mode='interval',scan_interval=17,scan_formats='cbz' WHERE id=?`, lib.ID); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "series"), 0755); err != nil {
		t.Fatal(err)
	}
	migration := startRelocationPreview(t, c, lib.ID, target)
	accepted := acceptRelocationAnomalies(t, c, migration.ID)
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)
	router := chi.NewRouter()
	c.SetupRoutes(router)
	server := httptest.NewServer(router)
	defer server.Close()
	client := newAuthClient(t)
	_, login := authDo(t, client, http.MethodPost, server.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "password1"})
	var session authSessionResponse
	if err := json.Unmarshal(login, &session); err != nil {
		t.Fatal(err)
	}
	base := server.URL + "/api/libraries/" + strconv.FormatInt(lib.ID, 10) + "/relocation-restoration"
	response, body := authDo(t, client, http.MethodPost, base+"/checks", session.CSRFToken, map[string]any{})
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("recheck: %d %s", response.StatusCode, body)
	}
	var check RelocationPlan
	if err := json.Unmarshal(body, &check); err != nil {
		t.Fatal(err)
	}
	check = awaitRelocationPlan(t, c, check.ID)
	if check.AnomalyCount != 1 || check.BlockerCount != 0 {
		t.Fatalf("current facts: %+v", check)
	}
	protection, err := store.LibraryRelocationProtection(context.Background(), lib.ID)
	if err != nil || protection == nil || protection.OperationID != accepted.ID || protection.ScanMode != "interval" || protection.ScanInterval != 17 {
		t.Fatalf("historical protection lost: %+v %v", protection, err)
	}
	if err = c.scanner.CleanupLibrary(context.Background(), lib.ID); err == nil {
		t.Fatal("read-only recheck released protection")
	}
	request := map[string]any{"plan_id": check.ID, "confirm_release": true, "scan_mode": "none", "scan_interval": 23, "scan_formats": "cbz"}
	response, body = authDo(t, client, http.MethodPost, base+"/release", session.CSRFToken, request)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("remaining risk not acknowledged: %d %s", response.StatusCode, body)
	}
	request["accept_loss_risk"] = true
	response, body = authDo(t, client, http.MethodPost, base+"/release", session.CSRFToken, request)
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("explicit release: %d %s", response.StatusCode, body)
	}
	var operation RelocationOperation
	if err = json.Unmarshal(body, &operation); err != nil {
		t.Fatal(err)
	}
	operation = awaitRelocationOperation(t, c, operation.ID)
	if operation.Status != "completed" {
		t.Fatalf("release: %+v", operation)
	}
	protection, err = store.LibraryRelocationProtection(context.Background(), lib.ID)
	if err != nil || protection != nil {
		t.Fatalf("release did not clear protection: %+v %v", protection, err)
	}
	gotLib, _ := store.GetLibrary(context.Background(), lib.ID)
	gotSeries, e1 := store.GetSeries(context.Background(), series.ID)
	gotBook, e2 := store.GetBook(context.Background(), book.ID)
	if e1 != nil || e2 != nil || gotLib.Path != target || gotLib.ScanMode != "none" || gotLib.ScanInterval != 23 || gotSeries.Path != filepath.Join(target, "series") || gotBook.Path != filepath.Join(target, "series", "missing.cbz") {
		t.Fatalf("release changed records: %+v %+v %+v %v %v", gotLib, gotSeries, gotBook, e1, e2)
	}
	response, body = authDo(t, client, http.MethodPost, base+"/release", session.CSRFToken, request)
	var duplicate RelocationOperation
	_ = json.Unmarshal(body, &duplicate)
	if response.StatusCode != http.StatusAccepted || duplicate.ID != operation.ID {
		t.Fatalf("duplicate: %d %s", response.StatusCode, body)
	}
}

func TestRestorationRejectsConfigPolicyEditedOnDiskAfterCheck(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "missing.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	acceptRelocationAnomalies(t, c, startRelocationPreview(t, c, lib.ID, target).ID)
	check := startRestorationCheck(t, c, lib.ID)
	if err := c.config.Save(c.configPath, nil, func(*config.Config) error { return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadConfigFile(c.configPath)
	if err != nil {
		t.Fatal(err)
	}
	disk.Library.StorageProfile = "hdd_external"
	encoded, err := yaml.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err = config.AtomicWriteFile(c.configPath, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	op := submitRestoration(t, c, lib.ID, check.ID, "none", true)
	if op.Status != "failed" || op.Code != "relocation_stale_plan" {
		t.Fatalf("external policy edit accepted: %+v", op)
	}
	if p, err := store.LibraryRelocationProtection(context.Background(), lib.ID); err != nil || p == nil {
		t.Fatalf("stale consent removed protection: %+v %v", p, err)
	}
}

func startRestorationCheck(t *testing.T, c *Controller, libraryID int64) RelocationPlan {
	t.Helper()
	rec := httptest.NewRecorder()
	c.createRestorationCheck(rec, requestWithRouteParam(http.MethodPost, "/restore/checks", []byte(`{}`), "libraryId", strconv.FormatInt(libraryID, 10)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("check: %d %s", rec.Code, rec.Body.String())
	}
	var plan RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return awaitRelocationPlan(t, c, plan.ID)
}

func submitRestoration(t *testing.T, c *Controller, libraryID, planID int64, mode string, accept bool) RelocationOperation {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"plan_id": planID, "confirm_release": true, "accept_loss_risk": accept, "scan_mode": mode, "scan_interval": 23, "scan_formats": "cbz"})
	rec := httptest.NewRecorder()
	c.releaseRelocationProtection(rec, requestWithRouteParam(http.MethodPost, "/restore/release", body, "libraryId", strconv.FormatInt(libraryID, 10)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("release: %d %s", rec.Code, rec.Body.String())
	}
	var op RelocationOperation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	return awaitRelocationOperation(t, c, op.ID)
}

func TestRestorationWatcherFailureKeepsProtectionThroughFreshStartup(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "missing.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "series"), 0755); err != nil {
		t.Fatal(err)
	}
	acceptRelocationAnomalies(t, c, startRelocationPreview(t, c, lib.ID, target).ID)
	check := startRestorationCheck(t, c, lib.ID)
	offline := target + "-offline"
	effects := c.relocation.RestoreEffects
	c.relocation.RestoreEffects = func(ctx context.Context, op RelocationOperation) error {
		if err := os.Rename(target, offline); err != nil {
			return err
		}
		return effects(ctx, op)
	}
	op := submitRestoration(t, c, lib.ID, check.ID, "watch", true)
	if op.Status != "paths_committed" || op.Code != "restoration_recovering" || !op.Protected {
		t.Fatalf("watch failure exposed half release: %+v", op)
	}
	if p, err := store.LibraryRelocationProtection(context.Background(), lib.ID); err != nil || p == nil {
		t.Fatalf("watch failure lost protection: %+v %v", p, err)
	}
	if err := c.scanner.CleanupLibrary(context.Background(), lib.ID); !errors.Is(err, database.ErrLibraryProtected) {
		t.Fatalf("pending release cleanup: %v", err)
	}
	if err := c.config.Save(c.configPath, nil, func(*config.Config) error { return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	fresh, freshStore := freshRestorationController(t, c, store, root)
	op = awaitRelocationOperation(t, fresh, op.ID)
	if op.Status != "paths_committed" || op.Code != "restoration_recovering" {
		t.Fatalf("offline startup falsely completed: %+v", op)
	}
	if p, err := freshStore.LibraryRelocationProtection(context.Background(), lib.ID); err != nil || p == nil {
		t.Fatalf("startup lost protection: %+v %v", p, err)
	}
	rec := httptest.NewRecorder()
	fresh.servePageImage(rec, requestWithRouteParams(http.MethodGet, "/page", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("pending recovery allowed access: %d", rec.Code)
	}
	if err := os.Rename(offline, target); err != nil {
		t.Fatal(err)
	}
	final, finalStore := freshRestorationController(t, fresh, freshStore, root)
	op = awaitRelocationOperation(t, final, op.ID)
	if op.Status != "completed" || op.Protected {
		t.Fatalf("restoration did not recover: %+v", op)
	}
	if p, err := finalStore.LibraryRelocationProtection(context.Background(), lib.ID); err != nil || p != nil {
		t.Fatalf("completed recovery still protected: %+v %v", p, err)
	}
	if _, err := finalStore.GetBook(context.Background(), book.ID); err != nil {
		t.Fatalf("release deleted missing book: %v", err)
	}
	for _, run := range taskCenterFirstPage(t, final) {
		if run.Type == "scan_library" || run.Type == "cleanup_library" {
			t.Fatalf("release started immediate processing: %+v", run)
		}
	}
}

func freshRestorationController(t *testing.T, previous *Controller, store database.Store, root string) (*Controller, database.Store) {
	t.Helper()
	previous.Close()
	if _, err := store.DB().Exec(`DELETE FROM runs`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	freshStore, err := database.NewStore(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = freshStore.Close() })
	disk, err := config.LoadConfigFile(previous.configPath)
	if err != nil {
		t.Fatal(err)
	}
	manager := config.NewManager(disk)
	fresh := newControllerCore(freshStore, scanner.NewScanner(freshStore, manager), manager, previous.configPath, defaultControllerCacheSizes())
	t.Cleanup(fresh.Close)
	return fresh, freshStore
}
