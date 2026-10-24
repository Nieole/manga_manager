package scanning_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"manga-manager/internal/application"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/logger"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCoverBackfillUsesItsOwnRun(t *testing.T) {
	service, _, lib, engine := newScanService(t)
	launched, err := service.BackfillCovers(lib.ID, task.TriggerResumed)
	if err != nil {
		t.Fatal(err)
	}
	run, err := engine.Await(context.Background(), launched.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCompleted || run.Trigger != task.TriggerResumed || run.MessageParams["generated"] != "0" {
		t.Fatalf("cover run: %+v", run)
	}
}

func newCoverService(t *testing.T, background func(func()), slots func() int) (*scanning.Service, database.Store, database.Library, *scanner.Scanner, *task.Engine, *config.Manager) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "covers.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Covers", Path: t.TempDir(), ScanMode: "manual", ScanFormats: config.DefaultScanFormatsCSV, ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.Scanner.ScanProfile = config.ScanProfileMetadata
	cfg.Scanner.Workers = 1
	cfg.Scanner.ThumbnailFormat = "png"
	cfg.Cache.Dir = t.TempDir()
	config.NormalizeConfig(&cfg)
	manager := config.NewManager(&cfg)
	scan := scanner.NewScanner(store, manager)
	engine := task.New(task.Config{Store: taskstore.New(store.(*database.SqlStore).DB()), RunBackground: background, Slots: slots, DecorateRunContext: application.DecorateRunContext})
	effects := scanning.Effects{PurgeReadingPaths: func() {}, InvalidateStats: func(string) {}, WarmStats: func(string) {}, Backfill: func(context.Context, string) {}, Publish: func(string) {}}
	service := scanning.New(application.NewRuns(engine, nil), store, scan, manager.Snapshot, effects)
	scan.SetCoverRunLauncher(service.DispatchCoverBatch)
	scan.SetBatchCallback(service.BatchEvent)
	return service, store, lib, scan, engine, manager
}

func coverArchive(t *testing.T, lib database.Library, name string) {
	t.Helper()
	dir := filepath.Join(lib.Path, "Series")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("001.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write(encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func coverCount(t *testing.T, store database.Store, libraryID int64) int {
	t.Helper()
	books, err := store.ListBooksByLibrary(context.Background(), libraryID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, book := range books {
		if book.CoverPath.Valid && book.CoverPath.String != "" {
			count++
		}
	}
	return count
}

func TestQueuedAndMergedCoverBatchesAreClaimedOnlyAtExecution(t *testing.T) {
	var pending []func()
	_, store, lib, scan, engine, _ := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, nil)
	coverArchive(t, lib, "one.cbz")
	coverArchive(t, lib, "two.cbz")
	ctx := context.Background()
	for range 3 {
		if err := scan.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
			t.Fatal(err)
		}
	}
	// All scans returned, although no cover body has been allowed to run.
	if got := coverCount(t, store, lib.ID); got != 0 {
		t.Fatalf("unstarted covers: %d", got)
	}
	before, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("coalesced cover launches should have two runs: %+v", before)
	}
	// A second library uses the same worker pool but owns independent counts.
	other, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Other", Path: t.TempDir(), ScanMode: "manual", ScanFormats: config.DefaultScanFormatsCSV, ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	coverArchive(t, other, "other.cbz")
	if err = scan.ScanLibrary(ctx, other.ID, other.Path, true, nil); err != nil {
		t.Fatal(err)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	if got := coverCount(t, store, lib.ID); got != 2 {
		t.Fatalf("generated covers: %d", got)
	}
	if got := coverCount(t, store, other.ID); got != 1 {
		t.Fatalf("other covers: %d", got)
	}
	snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil {
		t.Fatal(err)
	}
	var first, second task.Snapshot
	for _, snap := range snapshots {
		if snap.Side.Metrics["queued_covers"] == 6 {
			first = snap
		}
		if snap.Side.Metrics["queued_covers"] == 1 {
			second = snap
		}
	}
	if first.Side.Metrics["generated_covers"] != 2 || first.Run.Current != 6 || first.Run.Total != 6 || first.Run.Status != task.StatusCompleted {
		t.Fatalf("merged batch totals: %+v", first)
	}
	if second.Side.Metrics["generated_covers"] != 1 || second.Run.Status != task.StatusCompleted {
		t.Fatalf("separate library totals: %+v", second)
	}
}

func TestFullSlotsDoNotStartCoverGeneration(t *testing.T) {
	var pending []func()
	_, store, lib, scan, engine, _ := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, func() int { return 1 })
	ctx := context.Background()
	_, err := engine.Start(ctx, task.RunSpec{Identity: task.Identity{Type: "scan_library", Scope: task.ScopeSystem}, Trigger: task.TriggerManual}, func(context.Context, *runhandle.Handle) (task.Result, error) { return task.Result{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	coverArchive(t, lib, "queued.cbz")
	if err = scan.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
		t.Fatal(err)
	}
	snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].Run.Status != task.StatusQueued || coverCount(t, store, lib.ID) != 0 {
		t.Fatalf("slot admission: %+v", snapshots)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	if got := coverCount(t, store, lib.ID); got != 1 {
		t.Fatalf("slot release did not generate: %d", got)
	}
}

func TestApplicationScanCompletesWhileItsCoverRunStillWaits(t *testing.T) {
	var pending []func()
	service, store, lib, _, engine, _ := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, nil)
	coverArchive(t, lib, "independent.cbz")
	launched, err := service.ScanLibrary(lib, true, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	scanBody := pending[0]
	pending = pending[1:]
	scanBody()
	scanRun, err := engine.Await(context.Background(), launched.Run.ID)
	if err != nil || scanRun.Status != task.StatusCompleted {
		t.Fatalf("scan did not complete: %+v %v", scanRun, err)
	}
	if coverCount(t, store, lib.ID) != 0 {
		t.Fatal("cover generated before its own run body executed")
	}
	covers, err := engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(covers) != 1 || covers[0].Run.ID == scanRun.ID || !covers[0].Run.Status.IsLive() {
		t.Fatalf("independent cover run: %+v", covers)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	if coverCount(t, store, lib.ID) != 1 {
		t.Fatal("cover did not finish in its own run")
	}
}

func TestCoverPauseCancellationAndRestartBackfill(t *testing.T) {
	var pending []func()
	_, store, lib, scan, engine, manager := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, nil)
	ctx := context.Background()
	coverArchive(t, lib, "cancelled.cbz")
	if err := scan.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
		t.Fatal(err)
	}
	snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil || len(snapshots) != 1 {
		t.Fatalf("cover snapshot: %+v %v", snapshots, err)
	}
	id := snapshots[0].Run.ID
	if err = engine.Pause(id); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() { defer close(finished); pending[0]() }()
	wait, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err = engine.Await(wait, id); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("paused run finished early: %v", err)
	}
	if got := coverCount(t, store, lib.ID); got != 0 {
		t.Fatalf("paused work generated %d", got)
	}
	if err = engine.Cancel(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled cover body did not exit")
	}
	run, err := engine.Await(ctx, id)
	if err != nil || run.Status != task.StatusCancelled {
		t.Fatalf("cancelled run: %+v %v", run, err)
	}
	// Fresh orchestration and engine have no surviving in-memory batch.
	restartedScan := scanner.NewScanner(store, manager)
	restartedEngine := task.New(task.Config{Store: taskstore.New(store.(*database.SqlStore).DB()), RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
	restarted := scanning.New(application.NewRuns(restartedEngine, nil), store, restartedScan, manager.Snapshot, scanning.Effects{Publish: func(string) {}})
	launched, err := restarted.BackfillCovers(lib.ID, task.TriggerResumed)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restartedEngine.Await(ctx, launched.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != task.StatusCompleted || recovered.Trigger != task.TriggerResumed || coverCount(t, store, lib.ID) != 1 {
		t.Fatalf("restart backfill: %+v", recovered)
	}
}

func TestFailedCoverAdmissionLeavesOtherBatchesAndDoesNotGenerateWithoutARun(t *testing.T) {
	var pending []func()
	service, store, lib, scan, engine, _ := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, nil)
	ctx := context.Background()
	coverArchive(t, lib, "accepted.cbz")
	if err := scan.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
		t.Fatal(err)
	}
	db := store.(*database.SqlStore).DB()
	if _, err := db.Exec(`CREATE TRIGGER fail_cover_admission BEFORE INSERT ON runs BEGIN SELECT RAISE(ABORT, 'admission unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	coverArchive(t, lib, "failed-admission.cbz")
	if err := scan.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
		t.Fatal(err)
	}
	if got := coverCount(t, store, lib.ID); got != 0 {
		t.Fatalf("unadmitted work generated covers: %d", got)
	}
	if _, err := db.Exec(`DROP TRIGGER fail_cover_admission`); err != nil {
		t.Fatal(err)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	if got := coverCount(t, store, lib.ID); got != 1 {
		t.Fatalf("accepted batch lost or failed batch executed: %d", got)
	}
	snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].Side.Metrics["queued_covers"] != 1 {
		t.Fatalf("accepted batch observation: %+v", snapshots)
	}
	launched, err := service.BackfillCovers(lib.ID, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	run, err := engine.Await(ctx, launched.Run.ID)
	if err != nil || run.Status != task.StatusCompleted || coverCount(t, store, lib.ID) != 2 {
		t.Fatalf("missing-cover recovery: %+v %v", run, err)
	}
}

func captureCoverLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(logger.NewContextHandler(slog.NewTextHandler(&output, nil))))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &output
}

func TestCoverAdmissionWarningKeepsTheOriginatingScanContext(t *testing.T) {
	var pending []func()
	service, store, lib, _, engine, _ := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, nil)
	output := captureCoverLogs(t)
	coverArchive(t, lib, "admission-log.cbz")
	_, err := store.(*database.SqlStore).DB().Exec(`CREATE TRIGGER fail_cover_launch BEFORE INSERT ON runs WHEN (SELECT type FROM tasks WHERE id=NEW.task_id)='generate_covers' BEGIN SELECT RAISE(ABORT, 'cover admission unavailable'); END`)
	if err != nil {
		t.Fatal(err)
	}
	launched, err := service.ScanLibrary(lib, true, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	pending[0]()
	if _, err = engine.Await(context.Background(), launched.Run.ID); err != nil {
		t.Fatal(err)
	}
	var warning string
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "Failed to launch cover run;") {
			warning = line
		}
	}
	if !strings.Contains(warning, "run_id="+strconv.FormatInt(launched.Run.ID, 10)) || !strings.Contains(warning, "task_key=scan_library_") {
		t.Fatalf("cover launch warning lost scan context: %s", warning)
	}
}

func TestCoverWorkerLogsUseItsOwnRunAfterTheScanFinishes(t *testing.T) {
	var pending []func()
	service, _, lib, _, engine, _ := newCoverService(t, func(fn func()) { pending = append(pending, fn) }, nil)
	output := captureCoverLogs(t)
	coverArchive(t, lib, "worker-log.cbz")
	launched, err := service.ScanLibrary(lib, true, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	scanBody := pending[0]
	pending = pending[1:]
	scanBody()
	if _, err = engine.Await(context.Background(), launched.Run.ID); err != nil {
		t.Fatal(err)
	}
	covers, err := engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil || len(covers) != 1 {
		t.Fatalf("cover run: %+v %v", covers, err)
	}
	if err = os.Remove(filepath.Join(lib.Path, "Series", "worker-log.cbz")); err != nil {
		t.Fatal(err)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	var warning string
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.Contains(line, "Failed to generate queued thumbnail") {
			warning = line
		}
	}
	if !strings.Contains(warning, "run_id="+strconv.FormatInt(covers[0].Run.ID, 10)) || !strings.Contains(warning, "task_key=generate_covers_") {
		t.Fatalf("cover worker lost its own context: %s", warning)
	}
}
