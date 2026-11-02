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

// The approved seam is the bound Host's real HTTP consumer. Compatibility
// feature references are retired after binding; auth and push remain shared.
func TestHostLibraryRoutesDoNotUseCompatibilityCapabilities(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "Library", "Series", "book.cbz", 10)
	o := c.organizationHTTP
	h := (&Transport{legacy: c}).Bind(HostDependencies{
		Reading:        ReadingDependencies{Capabilities: NewReaderCapabilities(store, c.relations, c.proposals, c.taskEngine.engine, func() { c.stats.InvalidateVolatile("reading") }), Content: c.content, ImageCache: c.imageCache, Statistics: c.stats, Recommendations: c.recommendations, KOReader: c.koreader},
		Organization:   OrganizationDependencies{Relations: o.relations, Proposals: o.proposals, Collections: o.collections, Shelves: o.shelves, ReadingLists: o.lists, Grouping: o.grouping, Metadata: o.metadata, Selection: o.selection, GroupingQueries: o.groupingQueries, Series: o.series, StaticCollections: o.staticCollections, UnmatchedCount: o.unmatchedCount, KOReaderEnabled: o.koreaderEnabled, Publish: o.publish},
		Administration: AdministrationDependencies{Configuration: c.configuration, Store: store, Manager: c.config, KOReader: c.koreader},
		Libraries:      LibraryDependencies{Removal: store, Initials: store, Intelligence: c.intelligence, Queries: store, ComicInfo: store, Management: c.libraryManagement, Scans: c.scans, Maintenance: c.maintenance, Moves: c.moves, Disk: c.diskJobs, External: c.external, Relocation: c.relocation, Runs: RunDependencies{Engine: c.taskEngine.engine, Runs: c.taskEngine.runs, Store: c.taskEngine.runStore, Registry: c.taskEngine.registry}},
		Lifecycle:      c.process,
	})
	projection, external, relocation := c.taskEngine, c.external, c.relocation
	defer func() { c.taskEngine, c.external, c.relocation = projection, external, relocation }()
	management, scans, maintenance, moves, disk := c.libraryManagement, c.scans, c.maintenance, c.moves, c.diskJobs
	defer func() {
		c.libraryManagement, c.scans, c.maintenance, c.moves, c.diskJobs = management, scans, maintenance, moves, disk
	}()
	c.libraryManagement, c.scans, c.maintenance, c.moves, c.diskJobs = nil, nil, nil, nil, nil
	c.taskEngine, c.external, c.relocation = nil, nil, nil
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
