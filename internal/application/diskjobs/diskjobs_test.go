package diskjobs_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"manga-manager/internal/parser"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/application"
	"manga-manager/internal/application/diskjobs"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/external"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

func TestCreateExternalSessionCompletesEmptyScan(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }})
	manager := external.NewManager(store, time.Hour)
	service := diskjobs.New(application.NewRuns(engine, nil), store, manager, diskjobs.Effects{})
	result, err := service.CreateExternalSession(ctx, lib.ID, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := manager.GetSession(lib.ID, result.Session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.LoadRun(ctx, result.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if session.Status != "ready" || run.Status != task.StatusCompleted || run.MessageCode != "task.msg.scan_external_library.complete_empty" {
		t.Fatalf("session=%+v run=%+v", session, run)
	}
}

func TestTransferExternalCopiesAndRefreshesSession(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root, destination := t.TempDir(), t.TempDir()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root, "Series")
	if err = os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: seriesDir})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(seriesDir, "book.cbz")
	if err = os.WriteFile(source, []byte("book content"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateBook(ctx, database.CreateBookParams{SeriesID: series.ID, LibraryID: lib.ID, Name: "book.cbz", Path: source, Size: 12, FileModifiedAt: time.Now(), PageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }})
	manager := external.NewManager(store, time.Hour)
	service := diskjobs.New(application.NewRuns(engine, nil), store, manager, diskjobs.Effects{})
	session, err := service.CreateExternalSession(ctx, lib.ID, destination, false)
	if err != nil {
		t.Fatal(err)
	}
	transfer, err := service.TransferExternal(ctx, lib.ID, session.Session.SessionID, []int64{series.ID})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "Series", "book.cbz"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.LoadRun(ctx, transfer.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	coverage, err := manager.GetSession(lib.ID, session.Session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "book content" || run.Status != task.StatusCompleted || coverage.MatchedBooks != 1 {
		t.Fatalf("content=%q run=%+v coverage=%+v", got, run, coverage)
	}
}

func TestWriteSeriesComicInfoUsesPersistedMetadata(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: root})
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	writer := zip.NewWriter(&data)
	entry, _ := writer.Create("001.jpg")
	_, _ = entry.Write([]byte("page"))
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "book.cbz")
	if err = os.WriteFile(archive, data.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateBook(ctx, database.CreateBookParams{SeriesID: series.ID, LibraryID: lib.ID, Name: "book.cbz", Path: archive, Size: int64(data.Len()), FileModifiedAt: time.Now(), PageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	cfg := config.Config{}
	config.NormalizeConfig(&cfg)
	engine := task.New(task.Config{Store: tasks, DiskWork: diskwork.NewRunner(func() config.Config { return cfg }, storageio.NewScheduler()), RunBackground: func(fn func()) { fn() }})
	service := diskjobs.New(application.NewRuns(engine, nil), store, external.NewManager(store, time.Hour), diskjobs.Effects{})
	result, err := service.WriteSeriesComicInfo(ctx, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	run, err := tasks.LoadRun(ctx, result.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	found := false
	for _, file := range reader.File {
		if file.Name == "ComicInfo.xml" {
			found = true
			entry, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(entry)
			entry.Close()
			if err != nil {
				t.Fatal(err)
			}
			var info parser.ComicInfo
			if err = xml.Unmarshal(data, &info); err != nil {
				t.Fatal(err)
			}
			if info.Series != "Series" || info.Title != "book.cbz" || info.PageCount != 1 {
				t.Fatalf("metadata=%+v", info)
			}
		}
	}
	if !found || run.Status != task.StatusCompleted || run.MessageParams["written"] != "1" {
		t.Fatalf("found=%v run=%+v", found, run)
	}
}

type diskRig struct {
	service     *diskjobs.Service
	store       database.Store
	tasks       *taskstore.Store
	engine      *task.Engine
	manager     *external.Manager
	lib         database.Library
	series      database.Series
	book        database.Book
	destination string
}

func newDiskRig(t *testing.T, background func(func())) *diskRig {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	seriesDir := filepath.Join(root, "Series")
	if err = os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: seriesDir})
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(seriesDir, "book.cbz")
	if err = os.WriteFile(source, []byte("book content"), 0644); err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{SeriesID: series.ID, LibraryID: lib.ID, Name: "book.cbz", Path: source, Size: 12, FileModifiedAt: time.Now(), PageCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	cfg := config.Config{}
	config.NormalizeConfig(&cfg)
	engine := task.New(task.Config{Store: tasks, Slots: func() int { return 1 }, DiskWork: diskwork.NewRunner(func() config.Config { return cfg }, storageio.NewScheduler()), RunBackground: background})
	manager := external.NewManager(store, time.Hour)
	service := diskjobs.New(application.NewRuns(engine, nil), store, manager, diskjobs.Effects{})
	return &diskRig{service, store, tasks, engine, manager, lib, series, book, t.TempDir()}
}
func (r *diskRig) ready(t *testing.T) external.SessionSnapshot {
	t.Helper()
	session, err := r.manager.CreateSession(context.Background(), r.lib.ID, r.destination, false)
	if err != nil {
		t.Fatal(err)
	}
	session, err = r.manager.ScanSession(context.Background(), session.SessionID, discardProgress{})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

type discardProgress struct{}

func (discardProgress) Advance(int, int) {}
func TestExternalDestinationSharesMigrationAccess(t *testing.T) {
	rig := newDiskRig(t, func(fn func()) { fn() })
	ctx := context.Background()
	session := rig.ready(t)
	target, err := rig.store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Other", Path: rig.destination, ScanMode: "watch", ScanFormats: "cbz", ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	release, err := rig.engine.AcquireLibraryExclusive(ctx, target.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = rig.service.TransferExternal(ctx, rig.lib.ID, session.SessionID, []int64{rig.series.ID})
	if !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("transfer accessed protected destination: %v", err)
	}
	if _, err = os.Stat(filepath.Join(rig.destination, "Series", "book.cbz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination was written: %v", err)
	}
}

func TestQueuedExportRetainsCoverageAndCancellationNeverCopies(t *testing.T) {
	var pending []func()
	rig := newDiskRig(t, func(fn func()) { pending = append(pending, fn) })
	ctx := context.Background()
	session := rig.ready(t)
	blocker, err := rig.engine.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "block", Scope: task.ScopeSystem}, Trigger: task.TriggerManual, Key: "block"}, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err := rig.service.TransferExternal(ctx, rig.lib.ID, session.SessionID, []int64{rig.series.ID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Run.Run.Status != task.StatusQueued {
		t.Fatalf("run=%+v", result.Run)
	}
	if release, err := rig.engine.AcquireLibraryExclusive(ctx, rig.lib.ID); !errors.Is(err, task.ErrLibraryUnavailable) {
		if release != nil {
			release()
		}
		t.Fatalf("queued closure not protected: %v", err)
	}
	if err = rig.engine.Cancel(result.Run.Run.ID); err != nil {
		t.Fatal(err)
	}
	pending[0]()
	run, err := rig.tasks.LoadRun(ctx, result.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCancelled {
		t.Fatalf("cancelled run=%+v blocker=%+v", run, blocker)
	}
	if _, err = os.Stat(filepath.Join(rig.destination, "Series", "book.cbz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled export wrote a file: %v", err)
	}
	release, err := rig.engine.AcquireLibraryExclusive(ctx, rig.lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestInvalidatedQueuedExportDoesNotUseRetainedPlan(t *testing.T) {
	var pending []func()
	rig := newDiskRig(t, func(fn func()) { pending = append(pending, fn) })
	ctx := context.Background()
	session := rig.ready(t)
	_, err := rig.engine.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "block", Scope: task.ScopeSystem}, Trigger: task.TriggerManual, Key: "block"}, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	result, err := rig.service.TransferExternal(ctx, rig.lib.ID, session.SessionID, []int64{rig.series.ID})
	if err != nil {
		t.Fatal(err)
	}
	rig.manager.InvalidateLibrary(rig.lib.ID)
	pending[0]()
	if len(pending) != 2 {
		t.Fatalf("pending=%d", len(pending))
	}
	pending[1]()
	run, err := rig.tasks.LoadRun(ctx, result.Run.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusFailed {
		t.Fatalf("run=%+v", run)
	}
	if _, err = os.Stat(filepath.Join(rig.destination, "Series", "book.cbz")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalidated plan wrote a file: %v", err)
	}
}

func TestExternalScanHonorsPauseAndCancellation(t *testing.T) {
	var body func()
	rig := newDiskRig(t, func(fn func()) { body = fn })
	ctx := context.Background()
	session, err := rig.manager.CreateSession(ctx, rig.lib.ID, rig.destination, false)
	if err != nil {
		t.Fatal(err)
	}
	launch, err := rig.service.ScanExternal(ctx, rig.lib.ID, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = rig.engine.Pause(launch.Run.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { body(); close(done) }()
	select {
	case <-done:
		t.Fatal("paused scan completed without resuming")
	case <-time.After(75 * time.Millisecond):
	}
	if err = rig.engine.Cancel(launch.Run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled paused scan did not exit")
	}
	run, err := rig.tasks.LoadRun(ctx, launch.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCancelled {
		t.Fatalf("run=%+v", run)
	}
}

func TestComicInfoRejectsAPathSnapshotChangedBeforeAdmission(t *testing.T) {
	rig := newDiskRig(t, func(fn func()) { fn() })
	ctx := context.Background()
	_, err := rig.store.(*database.SqlStore).DB().ExecContext(ctx, "UPDATE books SET path = ? WHERE id = ?", filepath.Join(t.TempDir(), "new.cbz"), rig.book.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rig.service.WriteComicInfoPrepared(ctx, rig.series, []database.Book{rig.book}, nil, nil)
	if !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("old write snapshot admitted: %v", err)
	}
}
