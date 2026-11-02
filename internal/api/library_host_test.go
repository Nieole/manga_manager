package api

import (
	"encoding/json"
	"fmt"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The bound Host serves library consumers through the shared admission queue.
func TestHostLibraryRoutesUseSharedAdmission(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "Library", "Series", "book.cbz", 10)
	h := c.Host
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)
	router := chi.NewRouter()
	h.SetupRoutes(router)
	login := httptest.NewRecorder()
	router.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"password1"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	var session authSessionResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(login.Result().Cookies()[0])
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	request(http.MethodPost, "/api/system/tasks/pause-all", "", http.StatusAccepted)
	request(http.MethodPost, fmt.Sprintf("/api/libraries/%d/scan", lib.ID), "", http.StatusOK)
	listed := request(http.MethodGet, "/api/system/tasks?type=scan_library", "", http.StatusOK)
	var runs []RunSnapshot
	if err := json.Unmarshal(listed.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "queued" {
		t.Fatalf("same admitted queue: %s", listed.Body.String())
	}
	request(http.MethodGet, fmt.Sprintf("/api/system/runs/%d/events", runs[0].RunID), "", http.StatusOK)
	request(http.MethodGet, fmt.Sprintf("/api/system/runs/%d/samples", runs[0].RunID), "", http.StatusOK)
	request(http.MethodPost, fmt.Sprintf("/api/system/runs/%d/cancel", runs[0].RunID), "", http.StatusAccepted)
	// A later read must project the cancellation from the one shared Engine.
	listed = request(http.MethodGet, "/api/system/tasks?type=scan_library", "", http.StatusOK)
	if err := json.Unmarshal(listed.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "cancelled" {
		t.Fatalf("cancel projection: %s", listed.Body.String())
	}
	request(http.MethodGet, "/api/libraries", "", http.StatusOK)
	request(http.MethodGet, fmt.Sprintf("/api/books/%d/comicinfo.xml", book.ID), "", http.StatusOK)
	request(http.MethodPost, fmt.Sprintf("/api/series/%d/comicinfo", series.ID), "", http.StatusAccepted)
	request(http.MethodGet, "/api/system/storage-io", "", http.StatusOK)
	request(http.MethodPost, "/api/system/rebuild-index", "", http.StatusOK)
	request(http.MethodPost, fmt.Sprintf("/api/libraries/%d/relocation-restoration/checks", lib.ID), "", http.StatusConflict)
	request(http.MethodGet, fmt.Sprintf("/api/libraries/%d/relocation-restoration", lib.ID), "", http.StatusOK)
	externalBody, _ := json.Marshal(map[string]string{"external_path": t.TempDir()})
	request(http.MethodPost, fmt.Sprintf("/api/libraries/%d/external-libraries/session", lib.ID), string(externalBody), http.StatusAccepted)
}
