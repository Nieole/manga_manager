package api

import (
	"context"
	"encoding/json"
	"errors"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/relocation"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

type previewReadBarrierKey struct{}

type delayedPreviewRunStore struct {
	task.Store
	entered chan struct{}
	resume  chan struct{}
}

func (s *delayedPreviewRunStore) LoadRun(ctx context.Context, id int64) (task.Run, error) {
	if ctx.Value(previewReadBarrierKey{}) != nil {
		close(s.entered)
		<-s.resume
	}
	return s.Store.LoadRun(ctx, id)
}

type delayedPreviewFiles struct {
	relocation.HostFiles
	once    sync.Once
	entered chan struct{}
	resume  chan struct{}
}

func (f *delayedPreviewFiles) Stat(path string) (os.FileInfo, error) {
	f.once.Do(func() { close(f.entered); <-f.resume })
	return f.HostFiles.Stat(path)
}

func TestRelocationPreviewPollingCannotOverwriteCompletedFacts(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	files := &delayedPreviewFiles{entered: make(chan struct{}), resume: make(chan struct{})}
	c.relocation.Files = files
	originalRuns := c.taskEngine.runStore
	runs := &delayedPreviewRunStore{Store: originalRuns, entered: make(chan struct{}), resume: make(chan struct{})}
	c.taskEngine.runStore = runs
	c.assembleRelocationUseCases()
	body, _ := json.Marshal(map[string]string{"target_root": target})
	created := httptest.NewRecorder()
	c.createRelocationPlan(created, requestWithRouteParam(http.MethodPost, "/api/libraries/1/relocation-plans", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	var plan RelocationPlan
	if err := json.Unmarshal(created.Body.Bytes(), &plan); err != nil || created.Code != http.StatusAccepted {
		t.Fatalf("create: %d %s %v", created.Code, created.Body.String(), err)
	}
	<-files.entered
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		req := requestWithRouteParam(http.MethodGet, "/api/relocation-plans/1", nil, "planId", strconv.FormatInt(plan.ID, 10))
		req = req.WithContext(context.WithValue(req.Context(), previewReadBarrierKey{}, true))
		c.getRelocationPlan(response, req)
	}()
	<-runs.entered
	close(files.resume)
	deadline := time.Now().Add(10 * time.Second)
	for {
		run, err := originalRuns.LoadRun(context.Background(), plan.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status.IsTerminal() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("check did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	close(runs.resume)
	<-done
	var got RelocationPlan
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || response.Code != http.StatusOK || got.Status != "checked" {
		t.Fatalf("poll overwrote completed check: %d %s %v", response.Code, response.Body.String(), err)
	}
	latest := awaitRelocationPlan(t, c, plan.ID)
	if latest.Status != "checked" || latest.AnomalyCount != 1 {
		t.Fatalf("completed facts lost: %+v", latest)
	}
}

type delayedSeriesAdmissionStore struct {
	database.Store
	calls   int
	entered chan struct{}
	resume  chan struct{}
}

func (s *delayedSeriesAdmissionStore) GetSeries(ctx context.Context, id int64) (database.Series, error) {
	s.calls++
	if s.calls == 2 {
		close(s.entered)
		<-s.resume
	}
	return s.Store.GetSeries(ctx, id)
}

func TestProtectedRelocationRejectsPreviouslyCheckedSeriesResume(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	delayed := &delayedSeriesAdmissionStore{Store: store, entered: make(chan struct{}), resume: make(chan struct{})}
	c.store = delayed
	c.assembleScanUseCases()
	result := make(chan error, 1)
	go func() { result <- c.launchSeriesScanTask(series.ID, false, task.TriggerResumed) }()
	<-delayed.entered
	op := acceptRelocationAnomalies(t, c, plan.ID)
	close(delayed.resume)
	if err := <-result; !errors.Is(err, database.ErrLibraryProtected) {
		t.Fatalf("series resume crossed protected commit: operation=%+v error=%v", op, err)
	}
	runs := httptest.NewRecorder()
	c.listTasks(runs, httptest.NewRequest(http.MethodGet, "/api/system/tasks?type=scan_series", nil))
	var snapshots []RunSnapshot
	if err := json.Unmarshal(runs.Body.Bytes(), &snapshots); err != nil || runs.Code != http.StatusOK || len(snapshots) != 0 {
		t.Fatalf("protected series resume created a run: %d %s %v", runs.Code, runs.Body.String(), err)
	}
}

func TestRelocationAcceptsMissingBookAndProtectsEveryRecord(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, series, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "series"), 0755); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if plan.AnomalyCount != 1 || plan.BlockerCount != 0 {
		t.Fatalf("preview: %+v", plan)
	}
	submit := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(body), "planId", strconv.FormatInt(plan.ID, 10)))
		return rec
	}
	if rec := submit(`{}`); rec.Code != http.StatusConflict {
		t.Fatalf("unaccepted: %d %s", rec.Code, rec.Body.String())
	}
	rec := submit(`{"accept_anomalies":true}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("accepted: %d %s", rec.Code, rec.Body.String())
	}
	var op RelocationOperation
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "completed" {
		t.Fatalf("operation: %+v", op)
	}
	libraries := httptest.NewRecorder()
	c.getLibraries(libraries, httptest.NewRequest(http.MethodGet, "/api/libraries", nil))
	var libraryStatus []struct {
		ID        int64 `json:"id"`
		Protected bool  `json:"relocation_protected"`
	}
	if err := json.Unmarshal(libraries.Body.Bytes(), &libraryStatus); err != nil || len(libraryStatus) != 1 || !libraryStatus[0].Protected {
		t.Fatalf("library protection visibility: %s %v", libraries.Body.String(), err)
	}
	gotLib, _ := store.GetLibrary(context.Background(), lib.ID)
	gotBook, err := store.GetBook(context.Background(), book.ID)
	if err != nil || gotBook.Path != filepath.Join(target, "series", "book.cbz") || gotLib.ScanMode != "none" {
		t.Fatalf("mapped protected record: %+v %+v %v", gotLib, gotBook, err)
	}
	if err = c.scanner.CleanupLibrary(context.Background(), lib.ID); err == nil {
		t.Fatal("protected cleanup should explain rejection")
	}
	if _, err = store.GetBook(context.Background(), book.ID); err != nil {
		t.Fatal("book deleted", err)
	}
	if _, err = store.GetSeries(context.Background(), series.ID); err != nil {
		t.Fatal("series deleted", err)
	}
	if _, err = store.DB().Exec(`UPDATE libraries SET scan_mode='interval' WHERE id=?`, lib.ID); err != nil {
		t.Fatal(err)
	}
	gotLib, _ = store.GetLibrary(context.Background(), lib.ID)
	if _, err = c.startLibraryScanRun(gotLib, false, task.TriggerScheduled); err == nil {
		t.Fatal("protected automatic scan admitted")
	}
	if err = c.launchSeriesScanTask(series.ID, false, task.TriggerResumed); err == nil {
		t.Fatal("protected series resume admitted")
	}
	edit := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"name": "renamed", "path": target, "scan_mode": "watch", "scan_formats": "cbz"})
	c.updateLibrary(edit, requestWithRouteParam(http.MethodPut, "/api/libraries/1", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if edit.Code != http.StatusConflict {
		t.Fatalf("ordinary edit bypass: %d %s", edit.Code, edit.Body.String())
	}
}

func acceptRelocationAnomalies(t *testing.T, c *Controller, planID int64) RelocationOperation {
	t.Helper()
	rec := httptest.NewRecorder()
	c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{"accept_anomalies":true}`), "planId", strconv.FormatInt(planID, 10)))
	var op RelocationOperation
	if rec.Code != http.StatusAccepted {
		t.Fatalf("accept: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	return awaitRelocationOperation(t, c, op.ID)
}

func TestRelocationProtectionSurvivesFreshStartupAndAllCleanupEntrypoints(t *testing.T) {
	c, store, _, root := newTestController(t)
	ctx := context.Background()
	lib, series, book := seedBookFixture(t, store, root, "source", "surviving", "missing.cbz", 1)
	if _, err := store.DB().Exec(`UPDATE libraries SET scan_mode='watch',scan_interval=17,scan_formats='cbz' WHERE id=?`, lib.ID); err != nil {
		t.Fatal(err)
	}
	missing, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "missing", Path: filepath.Join(lib.Path, "missing")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "third", Path: filepath.Join(lib.Path, "third")}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`INSERT INTO users(id,username,role) VALUES(101,'reader','regular')`); err != nil {
		t.Fatal(err)
	}
	if err = store.SetUserBookProgress(ctx, 101, book.ID, 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB().Exec(`INSERT INTO collections(id,name) VALUES(100,'keep'); INSERT INTO collection_series(collection_id,series_id) VALUES(100,?)`, missing.ID); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	for _, dir := range []string{"surviving", "third"} {
		if err = os.MkdirAll(filepath.Join(target, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	op := acceptRelocationAnomalies(t, c, plan.ID)
	if op.Status != "completed" || !op.Protected {
		t.Fatalf("protected commit: %+v", op)
	}
	for _, trigger := range []task.Trigger{task.TriggerManual, task.TriggerScheduled, task.TriggerWatch, task.TriggerResumed} {
		if _, err = c.startCleanupLibraryRun(lib.ID, trigger); !errors.Is(err, database.ErrLibraryProtected) {
			t.Fatalf("cleanup %s: %v", trigger, err)
		}
	}
	if err = c.scanner.CleanupLibrary(ctx, lib.ID); !errors.Is(err, database.ErrLibraryProtected) {
		t.Fatalf("direct cleanup: %v", err)
	}
	c.triggerGlobalScan(ctx)
	if p, e := store.LibraryRelocationProtection(ctx, lib.ID); e != nil || p == nil {
		t.Fatalf("global scan cleared protection: %+v %v", p, e)
	}
	rename := httptest.NewRecorder()
	body, _ := json.Marshal(map[string]any{"name": "Protected renamed", "path": target, "scan_mode": "none", "scan_formats": "cbz", "scan_interval": 17})
	c.updateLibrary(rename, requestWithRouteParam(http.MethodPut, "/api/libraries/1", body, "libraryId", strconv.FormatInt(lib.ID, 10)))
	if rename.Code != http.StatusOK {
		t.Fatalf("harmless edit rejected: %d %s", rename.Code, rename.Body.String())
	}
	// A present root and only one missing series would permit ordinary cleanup.
	if _, err = store.GetSeries(ctx, missing.ID); err != nil {
		t.Fatal("missing series was cascade-deleted", err)
	}
	if _, err = store.GetBook(ctx, book.ID); err != nil {
		t.Fatal("book in surviving series deleted", err)
	}
	if progress, ok, e := store.GetUserBookProgress(ctx, 101, book.ID); e != nil || !ok || progress.LastReadPage.Int64 != 1 {
		t.Fatalf("progress: %+v %v %v", progress, ok, e)
	}
	var members int
	if err = store.DB().QueryRow(`SELECT COUNT(*) FROM collection_series WHERE collection_id=100 AND series_id=?`, missing.ID).Scan(&members); err != nil || members != 1 {
		t.Fatalf("collection association: %d %v", members, err)
	}
	if err = c.config.Save(c.configPath, nil, func(*config.Config) error { return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err = store.DB().Exec(`DELETE FROM runs`); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	freshStore, err := database.NewStore(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = freshStore.Close() })
	freshConfig, err := config.LoadConfigFile(filepath.Join(root, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manager := config.NewManager(freshConfig)
	fresh := newControllerCore(freshStore, scanner.NewScanner(freshStore, manager), manager, filepath.Join(root, "config.yaml"), controllerCacheSizes{imageBytes: 8192, page: 8, bookPageSource: 8, progressWrite: 8})
	t.Cleanup(fresh.Close)
	gotPlan, err := fresh.relocation.Get(ctx, plan.ID)
	if err != nil || gotPlan.Protection == nil || gotPlan.Protection.ScanMode != "watch" || gotPlan.Protection.ScanInterval != 17 || gotPlan.Protection.ScanFormats != "cbz" {
		t.Fatalf("durable protection after fresh startup/history prune: %+v %v", gotPlan.Protection, err)
	}
	if err = fresh.scanner.CleanupLibrary(ctx, lib.ID); !errors.Is(err, database.ErrLibraryProtected) {
		t.Fatal(err)
	}
	if got, err := freshStore.GetSeries(ctx, series.ID); err != nil || got.Path != filepath.Join(target, "surviving") {
		t.Fatalf("surviving identity: %+v %v", got, err)
	}
}

func TestRelocationAnomalyConsentExpiresWhenOriginalSettingsChange(t *testing.T) {
	c, store, _, root := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if _, err := store.DB().Exec(`UPDATE libraries SET scan_mode='watch',scan_interval=17 WHERE id=?`, lib.ID); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.commitRelocationPlan(rec, requestWithRouteParam(http.MethodPost, "/api/relocation-plans/1/commit", []byte(`{"accept_anomalies":true}`), "planId", strconv.FormatInt(plan.ID, 10)))
	var op RelocationOperation
	if rec.Code != http.StatusAccepted {
		t.Fatalf("admission: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "failed" || op.Code != "relocation_stale_plan" {
		t.Fatalf("stale settings accepted: %+v", op)
	}
	got, _ := store.GetLibrary(context.Background(), lib.ID)
	if got.Path != lib.Path || got.ScanMode != "watch" {
		t.Fatalf("stale commit mutated library: %+v", got)
	}
}

func TestProtectedLibraryCleanRelocationRetainsOriginalSettings(t *testing.T) {
	c, store, _, root := newTestController(t)
	ctx := context.Background()
	lib, _, _ := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
	if _, err := store.DB().Exec(`UPDATE libraries SET scan_mode='interval',scan_interval=17,scan_formats='cbz' WHERE id=?`, lib.ID); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "series"), 0755); err != nil {
		t.Fatal(err)
	}
	plan := startRelocationPreview(t, c, lib.ID, target)
	if op := acceptRelocationAnomalies(t, c, plan.ID); op.Status != "completed" {
		t.Fatalf("first commit: %+v", op)
	}
	if err := os.WriteFile(filepath.Join(target, "series", "book.cbz"), make([]byte, 1024), 0644); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(root, "second-target")
	if err := os.Rename(target, second); err != nil {
		t.Fatal(err)
	}
	plan = startRelocationPreview(t, c, lib.ID, second)
	if plan.AnomalyCount != 0 || plan.BlockerCount != 0 {
		t.Fatalf("clean preview: %+v", plan)
	}
	op := submitRelocation(t, c, plan.ID)
	op = awaitRelocationOperation(t, c, op.ID)
	if op.Status != "completed" || !op.Protected {
		t.Fatalf("clean relocation lost protection: %+v", op)
	}
	p, err := store.LibraryRelocationProtection(ctx, lib.ID)
	if err != nil || p == nil || p.ScanMode != "interval" || p.ScanInterval != 17 || p.PlanID != plan.ID || p.AnomalyCount != 0 {
		t.Fatalf("current facts/original settings: %+v %v", p, err)
	}
	if err = c.scanner.CleanupLibrary(ctx, lib.ID); !errors.Is(err, database.ErrLibraryProtected) {
		t.Fatalf("clean migration cleared protection: %v", err)
	}
}

func TestRelocationRetainsConfirmedAnomaliesAfterFileChanges(t *testing.T) {
	for _, reason := range []string{"missing", "unreadable", "not_regular", "size_mismatch"} {
		for _, stale := range []bool{false, true} {
			t.Run(reason+"/stale="+strconv.FormatBool(stale), func(t *testing.T) {
				c, store, _, root := newTestController(t)
				lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
				target := filepath.Join(root, "target")
				path := filepath.Join(target, "series", "book.cbz")
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if reason == "not_regular" {
					if err := os.Mkdir(path, 0755); err != nil {
						t.Fatal(err)
					}
				} else if reason != "missing" {
					if err := os.WriteFile(path, []byte("smaller"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				if reason == "unreadable" {
					c.relocation.Files = unreadableRelocationFiles{path: path}
				}
				plan := startRelocationPreview(t, c, lib.ID, target)
				if plan.AnomalyCounts[reason] != 1 || plan.BlockerCount != 0 {
					t.Fatalf("classification: %+v", plan)
				}
				if stale {
					if reason == "not_regular" {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(path, make([]byte, 1024), 0644); err != nil {
						t.Fatal(err)
					}
					c.relocation.Files = relocation.HostFiles{}
				}
				op := acceptRelocationAnomalies(t, c, plan.ID)
				got, err := store.GetBook(context.Background(), book.ID)
				if err != nil {
					t.Fatal(err)
				}
				if op.Status != "completed" || !op.Protected || got.Path != path || got.ID != book.ID {
					t.Fatalf("anomalous record not mapped: %+v %+v", op, got)
				}
				p, e := store.LibraryRelocationProtection(context.Background(), lib.ID)
				if e != nil || p == nil {
					t.Fatalf("confirmed anomalies lost protection: %+v %v", p, e)
				}
			})
		}
	}
}

func TestRelocationProtectionAndPathsShareFailureBoundary(t *testing.T) {
	for _, failure := range []string{"protection_write", "effects"} {
		t.Run(failure, func(t *testing.T) {
			c, store, _, root := newTestController(t)
			ctx := context.Background()
			lib, _, book := seedBookFixture(t, store, root, "source", "series", "book.cbz", 1)
			if _, err := store.DB().Exec(`UPDATE libraries SET scan_mode='watch' WHERE id=?`, lib.ID); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(root, "target")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			plan := startRelocationPreview(t, c, lib.ID, target)
			if failure == "protection_write" {
				if _, err := store.DB().Exec(`CREATE TRIGGER fail_protection BEFORE INSERT ON library_relocation_protection BEGIN SELECT RAISE(ABORT,'protection unavailable'); END`); err != nil {
					t.Fatal(err)
				}
			} else {
				c.relocation.Effects = func(context.Context, RelocationOperation) error { return errors.New("watcher unavailable") }
			}
			op := acceptRelocationAnomalies(t, c, plan.ID)
			got, _ := store.GetLibrary(ctx, lib.ID)
			p, err := store.LibraryRelocationProtection(ctx, lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			if failure == "protection_write" {
				if op.Status != "failed" || got.Path != lib.Path || got.ScanMode != "watch" || p != nil {
					t.Fatalf("partial commit: %+v %+v %+v", op, got, p)
				}
				if b, e := store.GetBook(ctx, book.ID); e != nil || b.Path != book.Path {
					t.Fatalf("changed book: %+v %v", b, e)
				}
			} else {
				if op.Status != "paths_committed" || got.Path != target || got.ScanMode != "none" || p == nil {
					t.Fatalf("untruthful recovery state: %+v %+v %+v", op, got, p)
				}
				read := httptest.NewRecorder()
				c.servePageImage(read, requestWithRouteParams(http.MethodGet, "/api/books/page/1/1", nil, map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "pageNumber": "1"}))
				if read.Code != http.StatusServiceUnavailable {
					t.Fatalf("half commit readable: %d", read.Code)
				}
				c.Close()
				fresh := restartController(t, c, store, root)
				if recovered := awaitRelocationOperation(t, fresh, op.ID); recovered.Status != "completed" {
					t.Fatalf("recovery: %+v", recovered)
				}
				if err = fresh.scanner.CleanupLibrary(ctx, lib.ID); !errors.Is(err, database.ErrLibraryProtected) {
					t.Fatalf("recovered protection: %v", err)
				}
			}
		})
	}
}
