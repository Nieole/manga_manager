package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/application/libraries"
)

func TestLibraryCreateHTTPSeparatesSavedFactsAndInitialScanAdmission(t *testing.T) {
	c, store, _, _ := newTestController(t)
	body, _ := json.Marshal(CreateLibraryRequest{Name: "Managed", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz"})
	rec := httptest.NewRecorder()
	c.createLibrary(rec, requestWithRouteParam(http.MethodPost, "/api/libraries", body, "", ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("HTTP%d %s", rec.Code, rec.Body.String())
	}
	var result libraries.SaveResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Saved || !result.RefreshRequested || result.Watching != "disabled" || result.InitialScan == nil || !result.InitialScan.Admitted || result.InitialScan.RunID == 0 {
		t.Fatalf("lost save/admission facts: %+v", result)
	}
	if _, err := store.GetLibrary(context.Background(), result.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err := c.runtime.Engine.SnapshotOf(context.Background(), result.InitialScan.RunID)
	if err != nil || snapshot.Run.ID != result.InitialScan.RunID {
		t.Fatalf("admission did not name actual run: %+v %v", snapshot, err)
	}
}

func TestLibraryUpdateHTTPReportsActualPathChangeOccupiers(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "Managed", "Series", "book.cbz", 1)
	release, err := c.runtime.Engine.AcquireLibraryRead(lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	body, _ := json.Marshal(UpdateLibraryRequest{Name: "Moved", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz"})
	rec := httptest.NewRecorder()
	c.updateLibrary(rec, requestWithRouteParam(http.MethodPut, "/api/libraries/x", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var result struct {
		Code      string `json:"code"`
		Conflicts []any  `json:"conflicts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || result.Code != "library_unavailable" || len(result.Conflicts) != 1 || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("occupancy facts lost: HTTP%d %s", rec.Code, rec.Body.String())
	}
}
