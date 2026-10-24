package scanning_test

import (
	"archive/zip"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"manga-manager/internal/application"
	"manga-manager/internal/application/scanning"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"manga-manager/internal/task"
	"manga-manager/internal/taskstore"
)

func newScanService(t *testing.T) (*scanning.Service, database.Store, database.Library, *task.Engine) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "library.db")
	if err := database.Migrate(dbPath); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root := t.TempDir()
	lib, err := store.CreateLibrary(context.Background(), database.CreateLibraryParams{Name: "Library", Path: root, ScanMode: "watch", ScanFormats: config.DefaultScanFormatsCSV, ScanInterval: 60})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = t.TempDir()
	manager := config.NewManager(&cfg)
	scan := scanner.NewScanner(store, manager)
	engine := task.New(task.Config{Store: taskstore.New(store.(*database.SqlStore).DB()), RunBackground: func(fn func()) { fn() }, DecorateRunContext: application.DecorateRunContext})
	effects := scanning.Effects{PurgeReadingPaths: func() {}, InvalidateStats: func(string) {}, WarmStats: func(string) {}, Backfill: func(context.Context, string) {}, Publish: func(string) {}}
	service := scanning.New(application.NewRuns(engine, nil), store, scan, manager.Snapshot, effects)
	scan.SetBatchCallback(service.BatchEvent)
	return service, store, lib, engine
}

func TestWatchRenameCompletesBeforeCleanup(t *testing.T) {
	service, store, lib, engine := newScanService(t)
	ctx := context.Background()
	seriesDir := filepath.Join(lib.Path, "Series")
	if err := os.MkdirAll(seriesDir, 0755); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(seriesDir, "one.cbz")
	file, err := os.Create(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("ComicInfo.xml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte("<ComicInfo><Title>First</Title></ComicInfo>")); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = service.WatchLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil || len(books) != 1 {
		t.Fatalf("initial books: %v %v", books, err)
	}
	id := books[0].ID
	if err = store.UpdateBookProgress(ctx, database.UpdateBookProgressParams{ID: id, LastReadPage: sql.NullInt64{Int64: 7, Valid: true}, LastReadAt: sql.NullTime{Time: time.Now(), Valid: true}}); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(seriesDir, "renamed.cbz")
	if err = os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if err = service.WatchLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	if err = service.WatchCleanup(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	book, err := store.GetBook(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if book.Path != newPath || book.LastReadPage.Int64 != 7 {
		t.Fatalf("identity/progress lost: %+v", book)
	}
	snapshots, err := engine.ListSnapshots(ctx, task.RunFilter{Types: []task.Type{"scan_library"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].Run.Trigger != task.TriggerWatch || snapshots[0].Side.Args["force"] != "false" {
		t.Fatalf("scan declaration: %+v", snapshots)
	}
}

func TestProtectedLibraryRejectsAutomaticScansAndEveryCleanup(t *testing.T) {
	service, store, lib, _ := newScanService(t)
	_, err := store.(*database.SqlStore).DB().Exec(`INSERT INTO library_relocation_protection (library_id,operation_id,plan_id,facts_digest,anomaly_count,scan_mode,scan_interval,scan_formats) VALUES (?,1,1,'accepted',1,'watch',60,'cbz')`, lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []task.Trigger{task.TriggerManual, task.TriggerWatch, task.TriggerScheduled, task.TriggerResumed} {
		if _, err = service.CleanupLibrary(lib.ID, trigger); !errors.Is(err, database.ErrLibraryProtected) {
			t.Fatalf("cleanup %s: %v", trigger, err)
		}
		if trigger != task.TriggerManual {
			if _, err = service.ScanLibrary(lib, false, trigger); !errors.Is(err, database.ErrLibraryProtected) {
				t.Fatalf("scan %s: %v", trigger, err)
			}
		}
	}
}

func TestBatchEventKeepsAllEffects(t *testing.T) {
	var mu sync.Mutex
	var events []string
	add := func(value string) { mu.Lock(); defer mu.Unlock(); events = append(events, value) }
	service := scanning.New(nil, nil, nil, nil, scanning.Effects{InvalidateStats: func(s string) { add("invalidate:" + s) }, WarmStats: func(s string) { add("warm:" + s) }, Publish: func(s string) { add("publish:" + s) }})
	service.BatchEvent("scan_completed")
	want := []string{"invalidate:scanner_scan_completed", "warm:scanner_scan_completed", "publish:scan_completed"}
	for i, value := range want {
		if len(events) != 3 || events[i] != value {
			t.Fatalf("effects: %v", events)
		}
	}
}
