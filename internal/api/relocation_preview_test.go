package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/relocation"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"

	"github.com/go-chi/chi/v5"
)

func TestRelocationPreviewPreservesRecordsAndCanBeReadAfterReopen(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "旧资料库", "漫画 空格", "第一卷.cbz", 10)
	target := filepath.Join(root, "目标盘")
	if err := os.MkdirAll(filepath.Join(target, "漫画 空格"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "漫画 空格", "第一卷.cbz"), make([]byte, 1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(lib.Path); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"target_root": target})
	req := requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10))
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start preview: %d %s", rec.Code, rec.Body.String())
	}
	var started RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	plan := awaitRelocationPlan(t, c, started.ID)
	if plan.Status != "checked" || plan.BlockerCount != 0 || plan.AnomalyCount != 0 || plan.SeriesCount != 1 || plan.BookCount != 1 {
		t.Fatalf("preview result: %+v", plan)
	}
	if plan.SourceRoot != lib.Path || plan.TargetRoot != target || plan.RunID == 0 {
		t.Fatalf("preview roots and run: %+v", plan)
	}
	gotLib, _ := store.GetLibrary(context.Background(), lib.ID)
	gotBook, _ := store.GetBook(context.Background(), book.ID)
	gotSeries, _ := store.GetSeries(context.Background(), series.ID)
	if gotLib.Path != lib.Path || gotBook.Path != book.Path || gotSeries.Path != series.Path {
		t.Fatalf("read-only preview changed paths: %s %s %s", gotLib.Path, gotSeries.Path, gotBook.Path)
	}
	reopened := restartController(t, c, store, root)
	latestReq := requestWithRouteParam(http.MethodGet, "/api/libraries/1/relocation-plans/latest", nil, "libraryId", strconv.FormatInt(lib.ID, 10))
	latestRec := httptest.NewRecorder()
	reopened.latestRelocationPlan(latestRec, latestReq)
	var latest RelocationPlan
	if err := json.Unmarshal(latestRec.Body.Bytes(), &latest); err != nil || latest.ID != started.ID || latest.Status != "checked" {
		t.Fatalf("reopened preview: %d %s (%v)", latestRec.Code, latestRec.Body.String(), err)
	}
}

func startRelocationPreview(t *testing.T, c *Controller, libraryID int64, target string) RelocationPlan {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"target_root": target})
	req := requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(libraryID, 10))
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start preview: %d %s", rec.Code, rec.Body.String())
	}
	var plan RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return awaitRelocationPlan(t, c, plan.ID)
}

func previewDetails(t *testing.T, c *Controller, id int64, query string) RelocationDetails {
	t.Helper()
	req := requestWithRouteParam(http.MethodGet, "/api/relocation-plans/1/details?"+query, nil, "planId", strconv.FormatInt(id, 10))
	rec := httptest.NewRecorder()
	c.relocationDetails(rec, req)
	var page RelocationDetails
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("preview details: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	return page
}

func TestRelocationPreviewReportsExistingRecordsAndRejectsPrefixLookalikes(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	other, otherSeries, _ := seedBookFixture(t, store, root, "other", "occupied", "other.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: other.ID, SeriesID: otherSeries.ID,
		Name: "collision.cbz", Path: filepath.Join(target, "series", "book.cbz"), Size: 1, FileModifiedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "lookalike", Path: lib.Path + "-other", NameInitial: "L"}); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	page := previewDetails(t, c, plan.ID, "kind=blocker")
	reasons := map[string]bool{}
	for _, detail := range page.Items {
		reasons[detail.Reason] = true
	}
	if !reasons["record_occupied"] || !reasons["outside_source_root"] {
		t.Fatalf("missing conflict reasons: %+v", page.Items)
	}
	got, _ := store.GetBook(context.Background(), book.ID)
	if got.Path != book.Path {
		t.Fatal("blocked preview rewrote original book")
	}
}

type unreadableRelocationFiles struct {
	relocation.HostFiles
	path string
}

func (f unreadableRelocationFiles) Open(path string) (io.Closer, error) {
	if path == f.path {
		return nil, os.ErrPermission
	}
	return f.HostFiles.Open(path)
}

func TestRelocationPreviewClassifiesFileAnomaliesAndPagesPersistedDetails(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "source", "series", "missing.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "series", "directory.cbz"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"changed.cbz", "unreadable.cbz"} {
		if err := os.WriteFile(filepath.Join(target, "series", name), []byte("smaller"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"changed.cbz", "directory.cbz", "unreadable.cbz"} {
		if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID,
			Name: name, Path: filepath.Join(series.Path, name), Size: 1024, FileModifiedAt: time.Now(), Title: sql.NullString{}}); err != nil {
			t.Fatal(err)
		}
	}
	c.relocation.Files = unreadableRelocationFiles{path: filepath.Join(target, "series", "unreadable.cbz")}
	plan := startRelocationPreview(t, c, lib.ID, target)
	for _, reason := range []string{"missing", "size_mismatch", "not_regular", "unreadable"} {
		if plan.AnomalyCounts[reason] != 1 {
			t.Fatalf("anomaly %s count: %+v", reason, plan.AnomalyCounts)
		}
	}
	first := previewDetails(t, c, plan.ID, "kind=anomaly&limit=2")
	second := previewDetails(t, c, plan.ID, "kind=anomaly&limit=2&offset=2")
	if first.Total != 4 || len(first.Items) != 2 || len(second.Items) != 2 || first.Items[0].Ordinal == second.Items[0].Ordinal {
		t.Fatalf("paged anomalies: %+v %+v", first, second)
	}
}

func TestRelocationPreviewPoliciesShowConflictsAndOtherLibraryEffects(t *testing.T) {
	for _, scenario := range []string{"different_target", "nested_source", "nested_target", "same_target"} {
		t.Run(scenario, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			target := filepath.Join(root, "target")
			if err := os.MkdirAll(target, 0o755); err != nil {
				t.Fatal(err)
			}
			cfg := c.config.Snapshot()
			cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: lib.Path, StorageProfile: config.StorageProfileHDDExternal},
				{Path: root, StorageProfile: config.StorageProfileSSD}}
			if scenario == "different_target" || scenario == "same_target" {
				profile := config.StorageProfileSSD
				if scenario == "same_target" {
					profile = config.StorageProfileHDDExternal
				}
				cfg.Library.StoragePolicies = append(cfg.Library.StoragePolicies, config.LibraryStoragePolicy{Path: target, StorageProfile: profile})
			} else {
				parent := lib.Path
				if scenario == "nested_target" {
					parent = target
				}
				if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "nested", Path: filepath.Join(parent, "nested"), ScanMode: "none", ScanInterval: 60}); err != nil {
					t.Fatal(err)
				}
			}
			c.config.Replace(&cfg)
			before, _ := json.Marshal(c.config.Snapshot().Library.StoragePolicies)
			plan := startRelocationPreview(t, c, lib.ID, target)
			if plan.PolicyCount != 1 {
				t.Fatalf("expected explicit policy mapping: %+v", plan)
			}
			expected := "policy_affects_other_library"
			if scenario == "different_target" {
				expected = "policy_conflict"
			}
			page := previewDetails(t, c, plan.ID, "kind=blocker")
			found := false
			for _, detail := range page.Items {
				found = found || detail.Reason == expected
			}
			if scenario == "same_target" {
				if page.Total != 0 {
					t.Fatalf("identical target policy should merge: %+v", page)
				}
			} else if !found {
				t.Fatalf("expected %s: %+v", expected, page)
			}
			after, _ := json.Marshal(c.config.Snapshot().Library.StoragePolicies)
			if string(before) != string(after) {
				t.Fatal("policy preview changed live policies")
			}
		})
	}
}

func TestRelocationPreviewDetectsPolicyChangesInEmptyOtherLibraryDescendants(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	if _, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "parent", Path: root, ScanMode: "none", ScanInterval: 60}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	cfg := c.config.Snapshot()
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: filepath.Join(lib.Path, "empty-child"), StorageProfile: config.StorageProfileHDDExternal}}
	c.config.Replace(&cfg)
	plan := startRelocationPreview(t, c, lib.ID, target)
	page := previewDetails(t, c, plan.ID, "kind=blocker")
	for _, detail := range page.Items {
		if detail.Reason == "policy_affects_other_library" && detail.TargetPath == filepath.Join(lib.Path, "empty-child") {
			return
		}
	}
	t.Fatalf("empty descendant policy effect missing: %+v", page.Items)
}

func TestRelocationPreviewRequiresAdminAndCSRFForHostPathResults(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "private source", "series", "book.cbz", 1)
	_ = mkTestUser(t, store, "admin", database.RoleAdmin)
	_ = mkTestUser(t, store, "reader", database.RoleRegular)
	router := chi.NewRouter()
	c.SetupRoutes(router)
	srv := httptest.NewServer(router)
	defer srv.Close()
	admin, reader := newAuthClient(t), newAuthClient(t)
	_, login := authDo(t, admin, http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "admin", "password": "password1"})
	var session authSessionResponse
	if err := json.Unmarshal(login, &session); err != nil {
		t.Fatal(err)
	}
	_, _ = authDo(t, reader, http.MethodPost, srv.URL+"/api/auth/login", "", map[string]string{"username": "reader", "password": "password1"})
	url := srv.URL + "/api/libraries/" + strconv.FormatInt(lib.ID, 10) + "/relocation-plans"
	target := t.TempDir()
	if resp, _ := authDo(t, admin, http.MethodPost, url, "", map[string]string{"target_root": target}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing CSRF accepted: %d", resp.StatusCode)
	}
	resp, body := authDo(t, admin, http.MethodPost, url, session.CSRFToken, map[string]string{"target_root": target})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("admin preview: %d %s", resp.StatusCode, body)
	}
	var started RelocationPlan
	if err := json.Unmarshal(body, &started); err != nil {
		t.Fatal(err)
	}
	_ = awaitRelocationPlan(t, c, started.ID)
	for _, path := range []string{"/api/relocation-plans/" + strconv.FormatInt(started.ID, 10), "/api/relocation-plans/" + strconv.FormatInt(started.ID, 10) + "/details", "/api/libraries/" + strconv.FormatInt(lib.ID, 10) + "/relocation-plans/latest"} {
		response, data := authDo(t, reader, http.MethodGet, srv.URL+path, "", nil)
		if response.StatusCode != http.StatusForbidden || strings.Contains(string(data), target) || strings.Contains(string(data), lib.Path) {
			t.Fatalf("reader received host plan: %d %s", response.StatusCode, data)
		}
	}
}

func TestRelocationPreviewQueuedCancellationSurvivesRunHistoryPruning(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	other, _, _ := seedBookFixture(t, store, root, "other", "series", "book.cbz", 1)
	var bodies []func()
	c.taskEngine.runBackground = func(fn func()) { bodies = append(bodies, fn) }
	c.taskEngine.slots = func() int { return 1 }
	err := c.taskEngine.Run(libraryTask("test_hold", other.ID, variantSole), task.TriggerManual, RunSpec{Key: "test_hold", CanCancel: true}, func(context.Context, *runhandle.Handle) (RunResult, error) { return RunResult{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"target_root": t.TempDir()})
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var plan RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("queued preview: %d %s (%v)", rec.Code, rec.Body.String(), err)
	}
	cancel := httptest.NewRecorder()
	c.cancelRun(cancel, requestWithRouteParam(http.MethodPost, "/api/system/runs/1/cancel", nil, "runID", strconv.FormatInt(plan.RunID, 10)))
	if cancel.Code != http.StatusAccepted {
		t.Fatalf("cancel queued preview: %d %s", cancel.Code, cancel.Body.String())
	}
	if got := awaitRelocationPlan(t, c, plan.ID); got.Status != "cancelled" {
		t.Fatalf("queued cancellation: %+v", got)
	}
	if _, err = c.taskEngine.runStore.DeleteRuns(context.Background(), task.RunFilter{Statuses: []task.RunStatus{task.StatusCancelled}}); err != nil {
		t.Fatal(err)
	}
	restarted := restartController(t, c, store, root)
	if got := awaitRelocationPlan(t, restarted, plan.ID); got.Status != "cancelled" {
		t.Fatalf("pruning lost plan cancellation: %+v", got)
	}
	bodies[0]()
}

func TestRelocationPreviewReportsCancellingAndQueuedWorkWithoutCountingItself(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	var bodies []func()
	c.taskEngine.runBackground = func(fn func()) { bodies = append(bodies, fn) }
	first, err := c.startLibraryScanRun(lib, false, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.startLibraryScanRun(lib, false, task.TriggerWatch)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.taskEngine.cancelRun(first.Run.ID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"target_root": t.TempDir()})
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var plan RelocationPlan
	if err = json.Unmarshal(rec.Body.Bytes(), &plan); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	if len(bodies) != 2 {
		t.Fatalf("want held scan and preview bodies, got %d", len(bodies))
	}
	bodies[1]()
	got := awaitRelocationPlan(t, c, plan.ID)
	page := previewDetails(t, c, plan.ID, "kind=blocker")
	statuses := map[int64]string{}
	for _, detail := range page.Items {
		if detail.RecordType == "work" {
			statuses[detail.RunID] = detail.RunStatus
		}
	}
	if statuses[first.Run.ID] != "cancelling" || statuses[second.Run.ID] != "queued" || statuses[plan.RunID] != "" || got.BlockerCount != 2 {
		t.Fatalf("related work: %+v, plan %+v", page.Items, got)
	}
	if err = c.taskEngine.cancelRun(second.Run.ID); err != nil {
		t.Fatal(err)
	}
	bodies[0]()
}

func TestRelocationPreviewRestartMarksUnfinishedPlansInterruptedWithoutRunHistory(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	c.taskEngine.runBackground = func(func()) {}
	body, _ := json.Marshal(map[string]string{"target_root": t.TempDir()})
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var started RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil || rec.Code != http.StatusAccepted {
		t.Fatalf("start unfinished: %d %s", rec.Code, rec.Body.String())
	}
	restarted := restartController(t, c, store, root)
	if got := awaitRelocationPlan(t, restarted, started.ID); got.Status != "interrupted" {
		t.Fatalf("startup left unfinished preview: %+v", got)
	}
	if _, err := restarted.taskEngine.runStore.DeleteRuns(context.Background(), task.RunFilter{Statuses: []task.RunStatus{task.StatusInterrupted}}); err != nil {
		t.Fatal(err)
	}
	if got := awaitRelocationPlan(t, restarted, started.ID); got.Status != "interrupted" {
		t.Fatalf("pruning erased domain terminal state: %+v", got)
	}
}

func TestRelocationPreviewBoundsLargeResultPagesAndSamples(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "source", "series", "first.cbz", 1)
	for i := range 124 {
		name := strconv.Itoa(i) + ".cbz"
		if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: name, Path: filepath.Join(series.Path, name), Size: 1024, FileModifiedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	plan := startRelocationPreview(t, c, lib.ID, t.TempDir())
	if plan.BookCount != 125 || plan.AnomalyCount != 125 || len(plan.Samples) != 6 {
		t.Fatalf("bounded preview summary: %+v", plan)
	}
	page := previewDetails(t, c, plan.ID, "kind=anomaly&limit=99999")
	last := previewDetails(t, c, plan.ID, "kind=anomaly&limit=100&offset=100")
	if page.Total != 125 || page.Limit != 100 || len(page.Items) != 100 || len(last.Items) != 25 {
		t.Fatalf("page bounds: %+v %+v", page, last)
	}
}

func TestRelocationPreviewUsesNativeCaseAndSeparatorSemantics(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "source", "series", "case.cbz", 1)
	if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "CASE.cbz", Path: filepath.Join(series.Path, "CASE.cbz"), Size: 1024, FileModifiedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if runtime.GOOS == "windows" {
		other, otherSeries, _ := seedBookFixture(t, store, root, "other", "other", "other.cbz", 1)
		if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: other.ID, SeriesID: otherSeries.ID, Name: "collision.cbz", Path: filepath.ToSlash(strings.ToUpper(filepath.Join(target, "series", "case.cbz"))), Size: 1024, FileModifiedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	body, _ := json.Marshal(map[string]string{"target_root": target + string(filepath.Separator)})
	rec := httptest.NewRecorder()
	c.createRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var started RelocationPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	plan := awaitRelocationPlan(t, c, started.ID)
	page := previewDetails(t, c, plan.ID, "kind=blocker")
	reasons := map[string]bool{}
	for _, item := range page.Items {
		reasons[item.Reason] = true
	}
	if runtime.GOOS == "windows" {
		if !reasons["ambiguous_mapping"] || !reasons["record_occupied"] {
			t.Fatalf("Windows native canonicalization: %+v", page.Items)
		}
	} else if reasons["ambiguous_mapping"] {
		t.Fatalf("case-sensitive host rejected distinct filenames: %+v", page.Items)
	}
}

func TestRelocationPreviewDetectsOccupancyAcrossSeriesAndBookRecords(t *testing.T) {
	for _, occupiedKind := range []string{"series", "book"} {
		t.Run(occupiedKind, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			other, otherSeries, _ := seedBookFixture(t, store, root, "other", "series", "other.cbz", 1)
			target := t.TempDir()
			if occupiedKind == "series" {
				if _, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: other.ID, Name: "occupied", Path: filepath.Join(target, "series", "book.cbz"), NameInitial: "O"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := store.CreateBook(context.Background(), database.CreateBookParams{LibraryID: other.ID, SeriesID: otherSeries.ID, Name: "occupied", Path: filepath.Join(target, "series"), Size: 1024, FileModifiedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			plan := startRelocationPreview(t, c, lib.ID, target)
			page := previewDetails(t, c, plan.ID, "kind=blocker")
			for _, detail := range page.Items {
				if detail.Reason == "record_occupied" {
					return
				}
			}
			t.Fatalf("cross-kind occupancy missing: %+v", page.Items)
		})
	}
}

func awaitRelocationPlan(t *testing.T, c *Controller, id int64) RelocationPlan {
	t.Helper()
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		req := requestWithRouteParam(http.MethodGet, "/api/relocation-plans/1", nil, "planId", strconv.FormatInt(id, 10))
		rec := httptest.NewRecorder()
		c.getRelocationPlan(rec, req)
		var plan RelocationPlan
		if err := json.Unmarshal(rec.Body.Bytes(), &plan); err != nil {
			t.Fatalf("get preview: %d %s (%v)", rec.Code, rec.Body.String(), err)
		}
		if plan.Status != "pending" && plan.Status != "checking" {
			return plan
		}
		select {
		case <-deadline:
			t.Fatalf("preview did not finish: %+v", plan)
		case <-ticker.C:
		}
	}
}
