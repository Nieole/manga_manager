package bootstrap_test

import (
	"context"
	"encoding/json"
	"fmt"
	"manga-manager/internal/bootstrap"
	"manga-manager/internal/database"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// Exercise the production composition and route tree, including the /series mount
// shared with catalogue handlers and the host's existing session/CSRF gate.
func TestOrganizationHTTPThroughProductionHost(t *testing.T) {
	store, cfg := fixture(t)
	root := t.TempDir()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Original", Path: filepath.Join(root, "series")})
	if err != nil {
		t.Fatal(err)
	}
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	var cookie *http.Cookie
	var csrf string
	request := func(method, path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api"+path, strings.NewReader(body))
		if cookie != nil {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", csrf)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	setup := request(http.MethodPost, "/auth/setup", `{"username":"admin","password":"correct horse battery staple"}`, http.StatusOK)
	cookie = setup.Result().Cookies()[0]
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	if err := json.Unmarshal(setup.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	csrf = session.CSRFToken
	created := request(http.MethodPost, "/collections/", `{"name":"Host picks"}`, http.StatusCreated)
	var collection struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &collection); err != nil {
		t.Fatal(err)
	}
	request(http.MethodPost, fmt.Sprintf("/collections/%d/series", collection.ID), fmt.Sprintf(`{"series_ids":[%d]}`, series.ID), http.StatusOK)
	listed := request(http.MethodGet, "/collections/", "", http.StatusOK)
	if !strings.Contains(listed.Body.String(), `"series_count":1`) {
		t.Fatalf("missing committed member: %s", listed.Body.String())
	}
	saved := request(http.MethodPut, fmt.Sprintf("/series/info/%d", series.ID), `{"title":"Edited"}`, http.StatusOK)
	var updated database.Series
	if err := json.Unmarshal(saved.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Title.String != "Edited" || !updated.Title.Valid {
		t.Fatalf("metadata projection: %s", saved.Body.String())
	}
	request(http.MethodPost, "/series/bulk-edit", fmt.Sprintf(`{"series_ids":[%d],"add_tags":["Host tag"]}`, series.ID), http.StatusOK)
	tags := request(http.MethodGet, fmt.Sprintf("/series/%d/tags", series.ID), "", http.StatusOK)
	if !strings.Contains(tags.Body.String(), "Host tag") {
		t.Fatalf("selection not committed: %s", tags.Body.String())
	}
	for _, path := range []string{"/metadata/reviews", "/reviews/inbox/summary", "/ai-grouping/reviews", "/reading-lists/", "/collection-views", fmt.Sprintf("/series/%d/relations", series.ID), fmt.Sprintf("/libraries/%d/smart-filters/", lib.ID)} {
		request(http.MethodGet, path, "", http.StatusOK)
	}
	// A logged-in administrator still needs the very same host CSRF token.
	csrf = "wrong"
	request(http.MethodPost, "/collections/", `{"name":"Denied"}`, http.StatusForbidden)
}
