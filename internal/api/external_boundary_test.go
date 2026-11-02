package api

import (
	"context"
	"encoding/json"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestExternalSessionHTTPRejectsOtherLibraryWithConflictCode(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "Owner", "Series", "book.cbz", 1)
	other := t.TempDir()
	if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Other", Path: other, ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60}); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{"external_path": other})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.families.disk.createExternalLibrarySession(rec, requestWithRouteParam(http.MethodPost, "/external", payload, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var response struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || response.Code != "external_root_overlap" {
		t.Fatalf("conflict: %d %s", rec.Code, rec.Body.String())
	}
	if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Unresolvable", Path: "relative-root", ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60}); err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(map[string]string{"external_path": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	c.families.disk.createExternalLibrarySession(rec, requestWithRouteParam(http.MethodPost, "/external", payload, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest || response.Code != "external_root_unresolvable" {
		t.Fatalf("unresolvable: %d %s", rec.Code, rec.Body.String())
	}
}
