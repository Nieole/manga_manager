package maintenance_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"manga-manager/internal/application"
	"manga-manager/internal/application/maintenance"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/koreader"
	"manga-manager/internal/reading"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type rig struct {
	service *maintenance.Service
	store   database.Store
	cfg     *config.Manager
	engine  *task.Engine
	scans   *scanning.Service
	scan    *scanner.Scanner
}

func newRig(t *testing.T, background func(func()), slots ...int) rig {
	t.Helper()
	slotCount := 1
	if len(slots) > 0 {
		slotCount = slots[0]
	}
	path := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := config.Config{}
	cfg.Cache.Dir = t.TempDir()
	cfg.Database.Path = path
	cfg.KOReader.Enabled = true
	cfg.KOReader.MatchMode = config.KOReaderMatchModeBinaryHash
	config.NormalizeConfig(&cfg)
	manager := config.NewManager(&cfg)
	tasks := taskstore.New(store.(*database.SqlStore).DB())
	engine := task.New(task.Config{Store: tasks, Slots: func() int { return slotCount }, RunBackground: background, DecorateRunContext: application.DecorateRunContext, DiskWork: diskwork.NewRunner(manager.Snapshot, storageio.NewScheduler())})
	runs := application.NewRuns(engine, nil)
	scan := scanner.NewScanner(store, manager)
	scans := scanning.New(runs, store, scan, manager.Snapshot, scanning.Effects{PurgeReadingPaths: func() {}, InvalidateStats: func(string) {}, WarmStats: func(string) {}, Backfill: func(context.Context, string) {}, Publish: func(string) {}})
	scan.SetCoverRunLauncher(scans.DispatchCoverBatch)
	scan.SetBatchCallback(scans.BatchEvent)
	service := maintenance.New(runs, store, scans, koreader.NewService(store, manager), manager.Snapshot, maintenance.Effects{})
	return rig{service, store, manager, engine, scans, scan}
}
func (r rig) book(t *testing.T) (database.Library, database.Book) {
	t.Helper()
	ctx := context.Background()
	lib, err := r.store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none", ScanFormats: "cbz", ScanInterval: 60, KoreaderSyncEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(lib.Path, "Series")
	if err = os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	series, err := r.store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	book, err := r.store.CreateBook(ctx, database.CreateBookParams{LibraryID: lib.ID, SeriesID: series.ID, Name: "Book.cbz", Path: filepath.Join(dir, "Book.cbz"), Size: 16, FileModifiedAt: time.Now(), PageCount: 32})
	if err != nil {
		t.Fatal(err)
	}
	return lib, book
}
func TestIndexMaintenanceCompletesThroughApplicationAtOneSlot(t *testing.T) {
	r := newRig(t, func(fn func()) { fn() })
	r.book(t)
	launched, err := r.service.RebuildIndex(task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.engine.Await(context.Background(), launched.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != task.StatusCompleted {
		t.Fatalf("run=%+v", run)
	}
	if err = r.service.Restart(context.Background(), task.Identity{Type: "rebuild_index", Scope: task.ScopeSystem}, nil, task.TriggerResumed); !errors.Is(err, maintenance.ErrAutomaticResumeUnsupported) {
		t.Fatalf("unattended index replay: %v", err)
	}
}
func TestRestartPreservesForegroundMatchingArguments(t *testing.T) {
	r := newRig(t, func(fn func()) { fn() })
	_, book := r.book(t)
	err := r.service.Restart(context.Background(), task.Identity{Type: "rebuild_book_hashes", Scope: task.ScopeSystem, Variant: "foreground"}, map[string]string{"match_mode": config.KOReaderMatchModeFilePath, "path_ignore_extension": "true"}, task.TriggerResumed)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := r.store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.PathFingerprint.Valid || updated.FileHash.Valid {
		t.Fatalf("original path matching was not replayed: %+v", updated)
	}
	snapshots, err := r.engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"rebuild_book_hashes"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].Run.Trigger != task.TriggerResumed || snapshots[0].Side.Args["path_ignore_extension"] != "true" || snapshots[0].Side.Args["match_mode"] != config.KOReaderMatchModeFilePath {
		t.Fatalf("replay=%+v", snapshots)
	}

}
func TestCalibrationReplayUsesOriginalFactAndClearSurvivesReopen(t *testing.T) {
	for _, clear := range []bool{false, true} {
		t.Run(fmt.Sprint(clear), func(t *testing.T) {
			r := newRig(t, func(fn func()) { fn() })
			_, book := r.book(t)
			ctx := context.Background()
			occurred := time.Date(2026, 10, 20, 10, 0, 0, 123456789, time.UTC)
			if _, err := r.store.UpsertKOReaderProgress(ctx, database.UpsertKOReaderProgressParams{Username: "reader", Document: "Series/Book.zip", Percentage: .75, Timestamp: occurred.Unix(), ReceivedAtNS: occurred.UnixNano()}); err != nil {
				t.Fatal(err)
			}
			if clear {
				at := occurred.Add(time.Hour)
				if _, err := reading.New(r.store, nil).MarkBooks(ctx, reading.MarkBooksInput{BookIDs: []int64{book.ID}, IsRead: false, OccurredAt: &at, OperationID: "clear-before-calibration"}); err != nil {
					t.Fatal(err)
				}
			}
			path := r.cfg.Snapshot().Database.Path
			// The application is rebuilt from the reopened SQLite file, including its durable clear guard.
			if err := r.store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := database.NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			tasks := taskstore.New(reopened.(*database.SqlStore).DB())
			engine := task.New(task.Config{Store: tasks, RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
			service := maintenance.New(application.NewRuns(engine, nil), reopened, nil, koreader.NewService(reopened, r.cfg), r.cfg.Snapshot, maintenance.Effects{})
			args := map[string]string{"match_mode": config.KOReaderMatchModeFilePath, "path_ignore_extension": "true"}
			if err = service.Restart(ctx, task.Identity{Type: "refresh_koreader_matching", Scope: task.ScopeSystem}, args, task.TriggerResumed); err != nil {
				t.Fatal(err)
			}
			after, err := reopened.GetBook(ctx, book.ID)
			if err != nil {
				t.Fatal(err)
			}
			if clear {
				if reading.InspectBook(after).Started || after.LastReadAt.Valid {
					t.Fatalf("cleared position revived: %+v", after)
				}
			} else if after.LastReadPage.Int64 != 24 || !after.LastReadAt.Time.Equal(occurred) {
				t.Fatalf("original fact changed: page=%+v at=%v", after.LastReadPage, after.LastReadAt)
			}
			snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"refresh_koreader_matching"}})
			if err != nil || len(snapshots) != 1 || snapshots[0].Run.Status != task.StatusCompleted || snapshots[0].Run.Total != 2 {
				t.Fatalf("calibration=%+v err=%v", snapshots, err)
			}
		})
	}
}

func TestForegroundAndBackfillRetainDistinctConcurrentIdentitiesAndReason(t *testing.T) {
	var pending []func()
	r := newRig(t, func(fn func()) { pending = append(pending, fn) }, 2)
	_, book := r.book(t)
	if err := os.WriteFile(book.Path, []byte("a real book payload"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.service.RebuildBookHashes(task.TriggerManual); err != nil {
		t.Fatal(err)
	}
	if err := r.service.Restart(context.Background(), task.Identity{Type: "rebuild_book_hashes", Scope: task.ScopeSystem, Variant: "low_priority_backfill"}, map[string]string{"reason": "scan_series"}, task.TriggerResumed); err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("distinct variants did not admit concurrently: bodies=%d", len(pending))
	}
	snapshots, err := r.engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"rebuild_book_hashes"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].Run.TaskID == snapshots[1].Run.TaskID {
		t.Fatalf("identities=%+v", snapshots)
	}
	reasons := map[string]bool{}
	for _, s := range snapshots {
		reasons[s.Side.Args["reason"]] = true
	}
	if !reasons["scan_series"] {
		t.Fatalf("original reason missing: %+v", snapshots)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	release, err := r.engine.AcquireLibraryExclusive(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err = r.service.RebuildBookHashes(task.TriggerManual); !errors.Is(err, task.ErrLibraryUnavailable) {
		t.Fatalf("maintenance bypassed exclusive access: %v", err)
	}
}
func TestThumbnailRebuildAggregatesLibrariesAndQueuesOwnCoverRunsAtOneSlot(t *testing.T) {
	var pending []func()
	r := newRig(t, func(fn func()) { pending = append(pending, fn) })
	libA, _ := r.book(t)
	libB, _ := r.book(t)
	coverArchive(t, libA, "Book.cbz")
	coverArchive(t, libB, "Book.cbz")
	if err := r.service.RebuildThumbnails(task.TriggerManual); err != nil {
		t.Fatal(err)
	}
	for len(pending) > 0 {
		fn := pending[0]
		pending = pending[1:]
		fn()
	}
	snapshots, err := r.engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"rebuild_thumbnails"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].Run.Status != task.StatusCompleted || snapshots[0].Side.Metrics["processed_archives"] != 2 {
		t.Fatalf("cross-library progress=%+v", snapshots)
	}
	covers, err := r.engine.ListSnapshots(context.Background(), task.RunFilter{Types: []task.Type{"generate_covers"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(covers) != 2 {
		t.Fatalf("cover runs=%+v", covers)
	}
	for _, lib := range []database.Library{libA, libB} {
		books, err := r.store.ListBooksByLibrary(context.Background(), lib.ID)
		if err != nil || len(books) != 1 || !books[0].CoverPath.Valid {
			t.Fatalf("missing regenerated cover: %+v err=%v", books, err)
		}
	}
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
