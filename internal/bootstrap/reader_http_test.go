package bootstrap_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/bootstrap"
	"manga-manager/internal/database"

	"github.com/go-chi/chi/v5"
)

// Session and Basic readers observe the same committed position through the production host.
func TestBoundReaderPositionRatingAndProtocolConsumers(t *testing.T) {
	store, cfg := fixture(t)
	snapshot := cfg.Snapshot()
	snapshot.Protocols.Mihon.Enabled = true
	snapshot.Protocols.OPDS.Enabled = true
	cfg.Replace(&snapshot)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	ctx := context.Background()
	root := t.TempDir()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: filepath.Join(root, "Series")})
	if err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "01.cbz", Path: filepath.Join(root, "Series", "01.cbz"), PageCount: 10, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	process.HTTP.SetupOPDSRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest("POST", "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`)))
	var session struct {
		CSRF string `json:"csrf_token"`
	}
	if setup.Code != 200 || json.Unmarshal(setup.Body.Bytes(), &session) != nil || session.CSRF == "" {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	request := func(method, path, body string, basic bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if basic {
			req.SetBasicAuth("admin", "correct horse battery staple")
		} else {
			req.AddCookie(cookie)
			req.Header.Set("X-CSRF-Token", session.CSRF)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec
	}
	// Prime the shared user statistics cache before the protocol write.
	request("GET", "/api/stats/dashboard", "", false)
	request("POST", fmt.Sprintf("/api/mihon/v1/books/%d/progress", book.ID), `{"page":10}`, true)
	rec := request("GET", fmt.Sprintf("/api/book-info/%d", book.ID), "", false)
	var view struct {
		LastReadPage sql.NullInt64 `json:"last_read_page"`
		IsRead       bool          `json:"is_read"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil || view.LastReadPage.Int64 != 10 || view.IsRead {
		t.Fatalf("book=%s err=%v", rec.Body.String(), err)
	}
	rec = request("GET", "/api/stats/dashboard", "", false)
	var stats database.DashboardStats
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil || stats.ReadBooks != 1 {
		t.Fatalf("stats=%s err=%v", rec.Body.String(), err)
	}
	path := fmt.Sprintf("/api/series/%d/review", series.ID)
	request("PUT", path, `{"rating":4.5,"review":"thoughtful"}`, false)
	rec = request("GET", path, "", false)
	if !strings.Contains(rec.Body.String(), `"rating":4.5`) || !strings.Contains(rec.Body.String(), `"review":"thoughtful"`) {
		t.Fatal(rec.Body.String())
	}
	request("DELETE", path, "", false)
	rec = request("GET", path, "", false)
	if !strings.Contains(rec.Body.String(), `"exists":false`) {
		t.Fatal(rec.Body.String())
	}
	request("GET", fmt.Sprintf("/api/books/%d", series.ID), "", false)
	request("GET", "/api/books/duplicates", "", false)
	rec = request("GET", fmt.Sprintf("/opds/v1.2/series/%d", series.ID), "", true)
	if !strings.Contains(rec.Body.String(), "/opds/v1.2/books/") {
		t.Fatal(rec.Body.String())
	}
	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/mihon/v1/libraries", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("Basic gate=%d", unauthorized.Code)
	}
}
