package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
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
