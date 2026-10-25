package relocating_test

import (
	"context"
	"database/sql"
	"errors"
	"gopkg.in/yaml.v3"
	"manga-manager/internal/application"
	"manga-manager/internal/application/relocating"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/relocation"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplicationChecksAnEmptyLibrary(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "move.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source, target := t.TempDir(), t.TempDir()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Books", Path: source, ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "test-model"
	cfg.LLM.Timeout = 30
	cfg.Database.Path = dbPath
	cfg.Cache.Dir = t.TempDir()
	domain := relocation.NewService(store, config.NewManager(&cfg))
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, DiskWork: diskwork.NewRunner(func() config.Config { return cfg }, storageio.Default), RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
	service := relocating.New(domain, application.NewRuns(engine, nil), engine, tasks, store, relocating.Effects{PurgeReadingPaths: func() {}, InvalidateStats: func(string) {}, InvalidateExternal: func(int64) {}, Watcher: func() (relocating.Watcher, error) { return nil, nil }})
	plan, err := service.Preview(context.Background(), lib.ID, source, target)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := domain.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Status != "checked" || plan.RunID == 0 {
		t.Fatalf("checked plan: %+v", checked)
	}
	if _, err = os.Stat(source); err != nil {
		t.Fatal(err)
	}
}

type testWatcher struct {
	fail   bool
	during func()
}

func (w *testWatcher) UnwatchLibrary(string) {}
func (w *testWatcher) WatchLibrary(int64, string, string) error {
	if w.during != nil {
		w.during()
	}
	if w.fail {
		return errors.New("watcher unavailable")
	}
	return nil
}

type moveRig struct {
	service                    *relocating.Service
	domain                     *relocation.Service
	store                      database.Store
	engine                     *task.Engine
	manager                    *config.Manager
	library                    database.Library
	book                       database.Book
	source, target, configPath string
	watcher                    *testWatcher
	failWrite                  bool
	effects                    []string
}

func newMoveRig(t *testing.T) *moveRig {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "library.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rig := &moveRig{store: store, source: filepath.Join(root, "source"), target: filepath.Join(root, "target"), configPath: filepath.Join(root, "config.yaml"), watcher: &testWatcher{}}
	seriesPath := filepath.Join(rig.source, "Series")
	if err = os.MkdirAll(seriesPath, 0755); err != nil {
		t.Fatal(err)
	}
	rig.library, err = store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Books", Path: rig.source, ScanMode: "watch", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: rig.library.ID, Name: "Series", Path: seriesPath})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(seriesPath, "book.cbz")
	if err = os.WriteFile(path, make([]byte, 1024), 0644); err != nil {
		t.Fatal(err)
	}
	rig.book, err = store.CreateBook(ctx, database.CreateBookParams{SeriesID: series.ID, LibraryID: rig.library.ID, Name: "book.cbz", Path: path, Size: 1024, FileModifiedAt: time.Now(), PageCount: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{ID: rig.book.ID, LastReadPage: sql.NullInt64{Int64: 7, Valid: true}, LastReadAt: sql.NullTime{Time: time.Now(), Valid: true}}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.LLM.Provider = "ollama"
	cfg.LLM.BaseURL = "http://localhost:11434"
	cfg.LLM.Model = "test-model"
	cfg.LLM.Timeout = 30
	cfg.Database.Path = dbPath
	cfg.Cache.Dir = t.TempDir()
	cfg.Library.StorageProfile = "hdd_external"
	cfg.Library.StoragePolicies = []config.LibraryStoragePolicy{{Path: rig.source, StorageProfile: "hdd_external"}}
	config.NormalizeConfig(&cfg)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = config.AtomicWriteFile(rig.configPath, data, config.ConfigFilePerm); err != nil {
		t.Fatal(err)
	}
	rig.manager = config.NewManager(&cfg, func(path string, data []byte, mode os.FileMode) error {
		if rig.failWrite {
			return os.ErrPermission
		}
		return config.AtomicWriteFile(path, data, mode)
	})
	rig.attach(t)
	if err = os.Rename(rig.source, rig.target); err != nil {
		t.Fatal(err)
	}
	return rig
}

func (r *moveRig) attach(t *testing.T) {
	t.Helper()
	tasks := taskstore.New(r.store.(*database.SqlStore).DB())
	r.engine = task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }, DiskWork: diskwork.NewRunner(r.manager.Snapshot, storageio.Default), DecorateRunContext: application.DecorateRunContext})
	r.domain = relocation.NewService(r.store, r.manager)
	r.domain.ConfigPath = r.configPath
	r.domain.ApplyConfig = func(*config.Config) error { return nil }
	r.service = relocating.New(r.domain, application.NewRuns(r.engine, nil), r.engine, tasks, r.store, relocating.Effects{
		PurgeReadingPaths: func() { r.effects = append(r.effects, "reading") }, InvalidateStats: func(string) { r.effects = append(r.effects, "stats") }, InvalidateExternal: func(int64) { r.effects = append(r.effects, "external") }, Watcher: func() (relocating.Watcher, error) { return r.watcher, nil },
	})
}

func (r *moveRig) preview(t *testing.T) relocation.RelocationPlan {
	t.Helper()
	plan, err := r.service.Preview(context.Background(), r.library.ID, r.source, r.target)
	if err != nil {
		t.Fatal(err)
	}
	checked, err := r.domain.Get(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Status != "checked" || checked.BlockerCount != 0 {
		details, _ := r.domain.Details(context.Background(), plan.ID, "blocker", 0, 50)
		t.Fatalf("preview blockers: %+v", details.Items)
	}
	return checked
}

func TestApplicationRelocationPreservesIdentityAndReleasesOnlyAfterEffects(t *testing.T) {
	r := newMoveRig(t)
	ctx := context.Background()
	nested, err := r.store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Nested", Path: filepath.Join(r.source, "Series"), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	disjoint, err := r.store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Independent", Path: filepath.Join(r.source, "Independent"), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	r.watcher.fail = true
	plan := r.preview(t)
	op, err := r.service.Confirm(ctx, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != "paths_committed" {
		run, _ := r.engine.Await(ctx, op.RunID)
		t.Fatalf("effects failure opened operation: %+v run=%+v", op, run)
	}
	for _, id := range []int64{r.library.ID, nested.ID} {
		if release, e := r.engine.AcquireLibraryRead(id); !errors.Is(e, task.ErrLibraryUnavailable) {
			if release != nil {
				release()
			}
			t.Fatalf("related library %d open: %v", id, e)
		}
	}
	if release, e := r.engine.AcquireLibraryRead(disjoint.ID); e != nil {
		t.Fatal(e)
	} else {
		release()
	}
	for _, path := range []string{filepath.Join(r.source, "new"), filepath.Join(r.target, "new")} {
		if release, e := r.service.GuardRoot(path); !errors.Is(e, task.ErrLibraryUnavailable) {
			if release != nil {
				release()
			}
			t.Fatalf("root open during recovery: %s %v", path, e)
		}
	}
	book, err := r.store.GetBook(ctx, r.book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if book.Path != filepath.Join(r.target, "Series", "book.cbz") || book.LastReadPage.Int64 != 7 {
		t.Fatalf("identity/progress: %+v", book)
	}
	if _, err = os.Stat(r.source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("application unexpectedly moved or recreated source: %v", err)
	}
	r.watcher.fail = false
	// A fresh engine/service recreates all access reservations before replaying effects.
	r.attach(t)
	r.watcher.during = func() {
		if release, e := r.engine.AcquireLibraryRead(r.library.ID); !errors.Is(e, task.ErrLibraryUnavailable) {
			if release != nil {
				release()
			}
			t.Error("startup effects ran without exclusive protection")
		}
	}
	if err = r.service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := r.domain.Operation(ctx, op.ID)
	if err != nil || recovered.Status != "completed" {
		t.Fatalf("recovery: %+v %v", recovered, err)
	}
	for _, id := range []int64{r.library.ID, nested.ID} {
		release, e := r.engine.AcquireLibraryRead(id)
		if e != nil {
			t.Fatal(e)
		}
		release()
	}
	if len(r.effects) < 6 {
		t.Fatalf("missing derived effects: %v", r.effects)
	}
	disk, err := config.LoadConfigFile(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Library.StoragePolicies) != 1 || disk.Library.StoragePolicies[0].Path != r.target {
		t.Fatalf("path policy not committed: %+v", disk.Library.StoragePolicies)
	}
}

func TestConfigurationWriteFailureCompensatesBeforeOpeningAccess(t *testing.T) {
	r := newMoveRig(t)
	ctx := context.Background()
	plan := r.preview(t)
	r.failWrite = true
	op, err := r.service.Confirm(ctx, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != "failed" {
		t.Fatalf("configuration rollback boundary: %+v", op)
	}
	book, err := r.store.GetBook(ctx, r.book.ID)
	if err != nil {
		t.Fatal(err)
	}
	disk, err := config.LoadConfigFile(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if book.Path != r.book.Path || disk.Library.StoragePolicies[0].Path != r.source {
		t.Fatalf("uncompensated paths/policies: %s %+v", book.Path, disk.Library.StoragePolicies)
	}
	release, err := r.engine.AcquireLibraryRead(r.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
	r.failWrite = false
	retry := r.preview(t)
	recovered, err := r.service.Confirm(ctx, retry.ID, false)
	if err != nil || recovered.Status != "completed" {
		t.Fatalf("retry after compensation: %+v %v", recovered, err)
	}
}

func TestRuntimePolicyFailureKeepsAccessProtectedUntilRecovery(t *testing.T) {
	r := newMoveRig(t)
	ctx := context.Background()
	plan := r.preview(t)
	r.domain.ApplyConfig = func(*config.Config) error { return errors.New("runtime unavailable") }
	op, err := r.service.Confirm(ctx, plan.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if op.Status != "paths_committed" {
		t.Fatalf("runtime failure boundary: %+v", op)
	}
	if release, e := r.engine.AcquireLibraryRead(r.library.ID); !errors.Is(e, task.ErrLibraryUnavailable) {
		if release != nil {
			release()
		}
		t.Fatalf("incomplete runtime publish opened access: %v", e)
	}
	r.domain.ApplyConfig = func(*config.Config) error { return nil }
	if err = r.service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := r.domain.Operation(ctx, op.ID)
	if err != nil || recovered.Status != "completed" {
		t.Fatalf("runtime recovery: %+v %v", recovered, err)
	}
	disk, err := config.LoadConfigFile(r.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Library.StoragePolicies[0].Path != r.target {
		t.Fatalf("disk policy not recovered: %+v", disk.Library.StoragePolicies)
	}
}

func TestRestorationRequiresTheExplicitAdministratorDecision(t *testing.T) {
	r := newMoveRig(t)
	ctx := context.Background()
	if err := os.Remove(filepath.Join(r.target, "Series", "book.cbz")); err != nil {
		t.Fatal(err)
	}
	plan := r.preview(t)
	if plan.AnomalyCount == 0 {
		t.Fatal("missing file was not recorded")
	}
	op, err := r.service.Confirm(ctx, plan.ID, true)
	if err != nil || !op.Protected || op.Status != "completed" {
		run, _ := r.engine.Await(ctx, op.RunID)
		t.Fatalf("accepted anomaly: %+v %v run=%+v", op, err, run)
	}
	check, err := r.service.CheckRestoration(ctx, r.library.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := relocation.RestorationRequest{PlanID: check.ID, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz", AcceptLossRisk: true}
	if _, err = r.service.Restore(ctx, r.library.ID, request); !errors.Is(err, relocation.ErrPlanNotReady) {
		t.Fatalf("release without confirmation: %v", err)
	}
	protection, err := r.store.LibraryRelocationProtection(ctx, r.library.ID)
	if err != nil || protection == nil {
		t.Fatalf("protection lost without confirmation: %+v %v", protection, err)
	}
	request.ConfirmRelease = true
	restored, err := r.service.Restore(ctx, r.library.ID, request)
	if err != nil || restored.Status != "completed" {
		t.Fatalf("explicit release: %+v %v", restored, err)
	}
	protection, err = r.store.LibraryRelocationProtection(ctx, r.library.ID)
	if err != nil || protection != nil {
		t.Fatalf("completed release: %+v %v", protection, err)
	}
}
