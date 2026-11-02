package runtime_test

import (
	"context"
	"errors"
	"manga-manager/internal/application/runtime"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/parser"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/runtimecfg"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssemblyRegistersRestartCapabilitiesBeforeAnyLaunch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cfg := config.Config{}
	cfg.Database.Path = path
	cfg.Cache.Dir = t.TempDir()
	cfg.LLM.Model = "controlled"
	config.NormalizeConfig(&cfg)
	app, err := runtime.New(store, config.NewManager(&cfg), runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	for _, id := range []task.Identity{{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: 1}, {Type: "rebuild_book_hashes", Scope: task.ScopeSystem, Variant: "foreground"}, {Type: "rebuild_book_hashes", Scope: task.ScopeSystem, Variant: "low_priority_backfill"}, {Type: "scrape", Scope: task.ScopeSystem, Variant: "all_libraries"}} {
		if !app.Registry.Retryable(id.Type, id.Variant) {
			t.Fatalf("first launch capability absent: %+v", id)
		}
	}
	if app.Registry.Resumable("rebuild_index", "") || app.Registry.Resumable("scrape", "all_libraries") {
		t.Fatal("attended-only work may resume")
	}
	if err := app.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestFailedConsistencyRecoveryNeverOpensBackgroundInputs(t *testing.T) {
	store, cfg := runtimeFixture(t)
	opened := make(chan struct{}, 1)
	app, err := runtime.New(store, cfg, runtime.Options{Effects: runtime.Effects{RunEvents: func(<-chan struct{}) { opened <- struct{}{} }}})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = store.DB().Close(); err != nil {
		t.Fatal(err)
	}
	if err = app.Start(context.Background()); err == nil {
		t.Fatal("failed consistency recovery opened the application")
	}
	select {
	case <-opened:
		t.Fatal("transport opened before recovery succeeded")
	default:
	}
}

func TestShutdownWaitsForAlreadyAdmittedBackgroundHandoff(t *testing.T) {
	store, cfg := runtimeFixture(t)
	handingOff := make(chan struct{})
	release := make(chan struct{})
	bodyExited := make(chan struct{})
	closed := make(chan struct{})
	var app *runtime.Runtime
	app, err := runtime.New(store, cfg, runtime.Options{Tasks: task.Config{RunBackground: func(fn func()) { close(handingOff); <-release; app.Go(fn) }}})
	if err != nil {
		t.Fatal(err)
	}
	launchDone := make(chan error, 1)
	go func() {
		_, err := app.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "handoff", Scope: task.ScopeSystem}, Key: "handoff", Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
			defer close(bodyExited)
			return task.Result{}, ctx.Err()
		})
		launchDone <- err
	}()
	<-handingOff
	go func() { app.Close(); close(closed) }()
	select {
	case <-app.Engine.Stopping():
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not seal launch admission and cancel admitted work")
	}
	select {
	case <-closed:
		t.Fatal("shutdown returned before admitted body was handed over")
	default:
	}
	close(release)
	if err = <-launchDone; err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown failed to join the admitted task")
	}
	select {
	case <-bodyExited:
	default:
		t.Fatal("admitted body was discarded")
	}
	runs, err := app.Engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"handoff"}})
	if err != nil || len(runs) != 1 || runs[0].Run.Status != task.StatusCancelled {
		t.Fatalf("task was not cancelled: %+v err=%v", runs, err)
	}
	if _, err = app.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "late", Scope: task.ScopeSystem}, Key: "late", Trigger: task.TriggerManual}, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil }); !errors.Is(err, task.ErrEngineStopped) {
		t.Fatalf("late task admitted: %v", err)
	}
}

func TestStoppedRuntimeRejectsLateResourcePublication(t *testing.T) {
	store, cfg := runtimeFixture(t)
	app, err := runtime.New(store, cfg, runtime.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})
	if err != nil {
		t.Fatal(err)
	}
	app.Close()
	before := cfg.Snapshot()
	next := before
	next.Scanner.ArchivePoolSize = 17
	if _, err := app.Configuration.SaveExpected(&before, next); !errors.Is(err, runtimecfg.ErrClosed) {
		t.Fatalf("late resource publication=%v", err)
	}
	if cfg.Snapshot().Scanner.ArchivePoolSize != before.Scanner.ArchivePoolSize || parser.PoolMaxSize() != 0 {
		t.Fatal("stopped runtime reopened resources or published settings")
	}
}

func TestResourceInitializationFailureDoesNotOpenInputs(t *testing.T) {
	store, cfg := runtimeFixture(t)
	invalid := cfg.Snapshot()
	invalid.Logging.Level = "invalid-level"
	cfg.Replace(&invalid)
	watchOpened := false
	app, err := runtime.New(store, cfg, runtime.Options{ConfigPath: filepath.Join(t.TempDir(), "config.yaml"), WatchConfig: func(string, *config.Manager) (runtime.ConfigWatch, error) {
		watchOpened = true
		return nil, errors.New("must not open")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.Start(context.Background()); err == nil {
		t.Fatal("resource failure exposed a started runtime")
	}
	if watchOpened {
		t.Fatal("configuration input opened before resources were ready")
	}
}

func TestManagerPublicationReleasesTheSameEngineQueue(t *testing.T) {
	store, cfg := runtimeFixture(t)
	initial := cfg.Snapshot()
	initial.Tasks.RunSlots = 1
	cfg.Replace(&initial)
	app, err := runtime.New(store, cfg, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	entered := make(chan struct{}, 2)
	for _, kind := range []task.Type{"first", "second"} {
		if _, err = app.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: kind, Scope: task.ScopeSystem}, Key: string(kind), Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
			entered <- struct{}{}
			<-ctx.Done()
			return task.Result{}, ctx.Err()
		}); err != nil {
			t.Fatal(err)
		}
	}
	<-entered
	if err = cfg.Save(filepath.Join(t.TempDir(), "config.yaml"), nil, func(next *config.Config) error { next.Tasks.RunSlots = 2; return nil }, nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("Manager publication did not release the same engine queue")
	}
	if app.Config().Tasks.RunSlots != 2 || app.Engine.Slots() != 2 {
		t.Fatal("runtime cached a separate configuration")
	}
}

func runtimeFixture(t *testing.T) (database.Store, *config.Manager) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime.db")
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
	return store, config.NewManager(&cfg)
}

type configWatch struct{ stopped atomic.Bool }

func (w *configWatch) Stop() { w.stopped.Store(true) }
func TestShutdownDrainsTransportBeforeTaskCancellationAndResourceRelease(t *testing.T) {
	store, cfg := runtimeFixture(t)
	watch := &configWatch{}
	var eventStopped atomic.Bool
	eventsJoined := make(chan struct{})
	bodyEntered := make(chan struct{})
	bodyExited := make(chan struct{})
	var earlyPoolRelease atomic.Bool
	parser.InitPool(5)
	app, err := runtime.New(store, cfg, runtime.Options{ConfigPath: "controlled.yaml", WatchConfig: func(_ string, manager *config.Manager) (runtime.ConfigWatch, error) {
		if manager != cfg {
			t.Error("watcher received a copied Manager")
		}
		return watch, nil
	}, Effects: runtime.Effects{RunEvents: func(done <-chan struct{}) { <-done; close(eventsJoined) }, StopEvents: func() { eventStopped.Store(true) }}})
	if err != nil {
		t.Fatal(err)
	}
	if err = app.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = app.Runs.Start(context.Background(), task.RunSpec{Identity: task.Identity{Type: "drain", Scope: task.ScopeSystem}, Key: "drain", Trigger: task.TriggerManual, CanCancel: true}, func(ctx context.Context, _ *runhandle.Handle) (task.Result, error) {
		close(bodyEntered)
		<-ctx.Done()
		if parser.PoolMaxSize() == 0 {
			earlyPoolRelease.Store(true)
		}
		close(bodyExited)
		return task.Result{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-bodyEntered
	if err = app.Shutdown(context.Background(), func(context.Context) error {
		if !watch.stopped.Load() || !eventStopped.Load() {
			t.Error("request drain preceded config/subscription shutdown")
		}
		select {
		case <-app.Done():
			t.Error("background resources stopped while requests were still draining")
		default:
		}
		select {
		case <-bodyExited:
			t.Error("task was cancelled before requests drained")
		default:
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-bodyExited:
	default:
		t.Fatal("shutdown did not join task")
	}
	select {
	case <-eventsJoined:
	default:
		t.Fatal("shutdown did not join event transport")
	}
	if earlyPoolRelease.Load() || parser.PoolMaxSize() != 0 {
		t.Fatal("archive pool lifetime did not enclose background work")
	}
}

func TestInterruptedHistoryReplaysThroughFirstProcessRegistry(t *testing.T) {
	store, cfg := runtimeFixture(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	old := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(func()) {}})
	previous, err := old.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "scan_library", Scope: task.ScopeLibrary, ScopeID: lib.ID}, Key: "old scan", Trigger: task.TriggerManual, Args: map[string]string{"force": "true"}}, func(context.Context, *runhandle.Handle) (task.Result, error) {
		t.Fatal("crashed process body must not run")
		return task.Result{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	app, err := runtime.New(store, cfg, runtime.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err = app.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if err = app.RecoverRuns(ctx); err != nil {
		t.Fatal(err)
	}
	snapshots, err := app.Engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"scan_library"}, Order: task.OrderSequenceDesc})
	if err != nil || len(snapshots) != 2 {
		t.Fatalf("history=%+v err=%v", snapshots, err)
	}
	timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	replayed, err := app.Engine.Await(timeout, snapshots[0].Run.ID)
	if err != nil || replayed.Status != task.StatusCompleted || replayed.Trigger != task.TriggerResumed || snapshots[0].Side.Args["force"] != "true" {
		t.Fatalf("replay=%+v err=%v", replayed, err)
	}
	interrupted, err := app.Tasks.LoadRun(ctx, previous.Run.ID)
	if err != nil || interrupted.Status != task.StatusInterrupted {
		t.Fatalf("original history=%+v err=%v", interrupted, err)
	}
}

func TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry(t *testing.T) {
	for _, backfill := range []string{"false", "true"} {
		t.Run(backfill, func(t *testing.T) {
			store, cfg := runtimeFixture(t)
			ctx := context.Background()
			lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
			if err != nil {
				t.Fatal(err)
			}
			old := task.New(task.Config{Store: taskstore.New(store.DB()), RunBackground: func(func()) {}})
			_, err = old.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "generate_covers", Scope: task.ScopeLibrary, ScopeID: lib.ID}, Trigger: task.TriggerChained, Args: map[string]string{"backfill": backfill}}, func(context.Context, *runhandle.Handle) (task.Result, error) {
				t.Fatal("crashed process body must not run")
				return task.Result{}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			app, err := runtime.New(store, cfg, runtime.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer app.Close()
			if err = app.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			if err = app.RecoverRuns(ctx); err != nil {
				t.Fatal(err)
			}
			snapshots, err := app.Engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers"}, Order: task.OrderSequenceDesc})
			if err != nil || len(snapshots) != 2 {
				t.Fatalf("history=%+v err=%v", snapshots, err)
			}
			timeout, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			replayed, err := app.Engine.Await(timeout, snapshots[0].Run.ID)
			if err != nil || replayed.Status != task.StatusCompleted || replayed.Trigger != task.TriggerResumed || snapshots[0].Side.Args["backfill"] != backfill {
				t.Fatalf("replay=%+v args=%v err=%v", replayed, snapshots[0].Side.Args, err)
			}
		})
	}
}
