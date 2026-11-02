package bootstrap_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"manga-manager/internal/application/libraries"
	appruntime "manga-manager/internal/application/runtime"
	"manga-manager/internal/bootstrap"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/parser"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/task"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func fixture(t *testing.T) (database.Store, *config.Manager) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "process.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.Config{}
	cfg.Database.Path = path
	cfg.Cache.Dir = t.TempDir()
	cfg.LLM.Model = "controlled"
	config.NormalizeConfig(&cfg)
	cfg.Tasks.RunSlots = 1
	return store, config.NewManager(&cfg)
}

func TestOpenUsesOneManagerQueueAndServesBoundHTTP(t *testing.T) {
	store, cfg := fixture(t)
	var watching atomic.Int32
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), WatchConfig: func(string, *config.Manager) (appruntime.ConfigWatch, error) { watching.Add(1); return &watch{}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	if watching.Load() != 1 {
		t.Fatal("startup inputs were not opened exactly once after assembly")
	}
	entered := make(chan struct{}, 2)
	ids := make(map[task.Type]int64)
	for _, kind := range []task.Type{"first", "second"} {
		launched, err := process.Application.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: kind, Scope: task.ScopeSystem}, Key: string(kind), Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
			entered <- struct{}{}
			<-ctx.Done()
			return task.Result{}, ctx.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[kind] = launched.Run.ID
	}
	<-entered
	runs, err := process.Application.Tasks.ListRuns(context.Background(), task.RunFilter{Statuses: task.LiveStatuses(), Types: []task.Type{"first", "second"}})
	if err != nil {
		t.Fatal(err)
	}
	if live := process.Application.Engine.Summarize(runs); live.Active != 1 || live.Queued != 1 {
		t.Fatalf("live=%+v", live)
	}
	if err := cfg.Save(filepath.Join(t.TempDir(), "config.yaml"), nil, func(next *config.Config) error { next.Tasks.RunSlots = 2; return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("published Manager did not release the HTTP process queue")
	}
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("bound HTTP=%d %s", rec.Code, rec.Body.String())
	}
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`)))
	if setup.Code != http.StatusOK || len(setup.Result().Cookies()) == 0 {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/system/tasks?type=second", nil)
	req.AddCookie(setup.Result().Cookies()[0])
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, req)
	var runsView []struct {
		RunID     int64  `json:"run_id"`
		Status    string `json:"status"`
		CanCancel bool   `json:"can_cancel"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &runsView); err != nil {
		t.Fatal(err)
	}
	if listed.Code != http.StatusOK || len(runsView) != 1 || runsView[0].RunID != ids["second"] || runsView[0].Status != string(task.StatusRunning) || !runsView[0].CanCancel {
		t.Fatalf("different HTTP queue/projection: %d %s", listed.Code, listed.Body.String())
	}
	if !process.Application.Registry.Retryable("scan_library", "") {
		t.Fatal("production restart declarations absent")
	}
}

type watch struct{ stopped atomic.Bool }

func (w *watch) Stop() { w.stopped.Store(true) }

func TestFailedRecoveryReturnsNoHostAndOpensNoInputs(t *testing.T) {
	store, cfg := fixture(t)
	if err := store.DB().Close(); err != nil {
		t.Fatal(err)
	}
	var opened atomic.Int32
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), WatchConfig: func(string, *config.Manager) (appruntime.ConfigWatch, error) { opened.Add(1); return &watch{}, nil }})
	if err == nil || process != nil || opened.Load() != 0 {
		t.Fatalf("failed recovery exposed process=%v err=%v inputs=%d", process, err, opened.Load())
	}
}

func TestHTTPQueryCacheReceivesTheApplicationInvalidation(t *testing.T) {
	store, cfg := fixture(t)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	setup := httptest.NewRecorder()
	router.ServeHTTP(setup, httptest.NewRequest(http.MethodPost, "/api/auth/setup", bytes.NewBufferString(`{"username":"admin","password":"correct horse battery staple"}`)))
	if setup.Code != http.StatusOK || len(setup.Result().Cookies()) == 0 {
		t.Fatalf("setup=%d %s", setup.Code, setup.Body.String())
	}
	cookie := setup.Result().Cookies()[0]
	load := func() database.DashboardStats {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/stats/dashboard", nil)
		req.AddCookie(cookie)
		router.ServeHTTP(rec, req)
		var stats database.DashboardStats
		if rec.Code != http.StatusOK {
			t.Fatalf("dashboard=%d %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatal(err)
		}
		return stats
	}
	if got := load(); got.TotalSeries != 0 {
		t.Fatalf("initial=%+v", got)
	}
	root := t.TempDir()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "New series", Path: filepath.Join(root, "series")}); err != nil {
		t.Fatal(err)
	}
	if got := load(); got.TotalSeries != 0 {
		t.Fatal("fixture did not prime the HTTP cache")
	}
	if _, err = process.Application.LibraryManagement.Update(context.Background(), lib.ID, libraries.UpdateInput{Name: "Saved library", Path: root, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz"}); err != nil {
		t.Fatal(err)
	}
	if got := load(); got.TotalSeries != 1 {
		t.Fatalf("application invalidated a different query cache: %+v", got)
	}
}

func TestShutdownDrainsTransportBeforeCancellingReaderAndRetiringResources(t *testing.T) {
	store, cfg := fixture(t)
	watcher := &watch{}
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), WatchConfig: func(string, *config.Manager) (appruntime.ConfigWatch, error) { return watcher, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	path := filepath.Join(t.TempDir(), "reader.cbz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	page, err := archive.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = page.Write([]byte("borrow survives drain")); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	observed := make(chan error, 1)
	var drained atomic.Bool
	_, err = process.Application.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "drain_reader", Scope: task.ScopeSystem}, Key: "drain_reader", Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		book, release, err := parser.GetArchiveFromPool(path)
		if err != nil {
			close(entered)
			observed <- err
			return task.Result{}, err
		}
		defer release()
		close(entered)
		<-ctx.Done()
		data, readErr := book.ReadPage("001.png")
		if !drained.Load() || parser.PoolMaxSize() == 0 || string(data) != "borrow survives drain" {
			readErr = fmt.Errorf("resources or reader cancelled before transport drain")
		}
		observed <- readErr
		return task.Result{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := process.Shutdown(context.Background(), func(context.Context) error {
		if !watcher.stopped.Load() {
			t.Fatal("configuration input not stopped before drain")
		}
		select {
		case <-process.Application.Engine.Stopping():
			t.Fatal("engine cancelled before transport drain")
		default:
		}
		drained.Store(true)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-observed; err != nil {
		t.Fatal(err)
	}
	if parser.PoolMaxSize() != 0 {
		t.Fatal("final resources not retired")
	}
}

// Exercise the net/http drain used by cmd/server, not only a drain callback.
func TestShutdownWaitsForHTTPConsumerBeforeStoppingEngine(t *testing.T) {
	store, cfg := fixture(t)
	process, err := bootstrap.Open(context.Background(), store, cfg, bootstrap.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	router := chi.NewRouter()
	process.HTTP.SetupRoutes(router)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		router.ServeHTTP(w, r)
	}))
	defer server.Close()
	defer unblock()
	response := make(chan error, 1)
	go func() {
		res, err := server.Client().Get(server.URL + "/api/auth/status")
		if err == nil {
			_, err = io.Copy(io.Discard, res.Body)
			_ = res.Body.Close()
			if err == nil && res.StatusCode != http.StatusOK {
				err = fmt.Errorf("auth status during drain: %d", res.StatusCode)
			}
		}
		response <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP consumer did not enter")
	}
	draining := make(chan struct{})
	stopped := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		stopped <- process.Shutdown(ctx, func(ctx context.Context) error {
			close(draining)
			return server.Config.Shutdown(ctx)
		})
	}()
	<-draining
	select {
	case <-process.Application.Engine.Stopping():
		t.Fatal("engine stopped with an HTTP consumer still active")
	case err := <-stopped:
		t.Fatalf("shutdown passed active HTTP consumer: %v", err)
	default:
	}
	unblock()
	if err := <-response; err != nil {
		t.Fatal(err)
	}
	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	select {
	case <-process.Application.Engine.Stopping():
	default:
		t.Fatal("engine not stopped after HTTP drain")
	}
}
