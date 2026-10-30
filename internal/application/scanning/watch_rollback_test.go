package scanning_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/task"
)

func TestWatchDoesNotQualifyCleanupAfterRenameCommitFails(t *testing.T) {
	service, store, lib, engine := newScanService(t)
	coverArchive(t, lib, "one.cbz")
	ctx := context.Background()
	if err := service.WatchLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("seed: %+v %v", before, err)
	}
	oldPath := before[0].Path
	newPath := filepath.Join(filepath.Dir(oldPath), "new.cbz")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	_, err = store.(*database.SqlStore).DB().Exec(`CREATE TABLE ingestion_guard (book_id INTEGER REFERENCES books(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER reject_renamed_ingestion AFTER UPDATE ON books WHEN NEW.name='new.cbz' BEGIN INSERT INTO ingestion_guard(book_id) VALUES(-1); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.WatchLibrary(ctx, lib.ID); err == nil {
		t.Fatal("rolled-back rename qualified as a successful watch scan; cleanup would delete its old identity")
	}
	after, err := store.GetBook(ctx, before[0].ID)
	if err != nil || after.Path != oldPath {
		t.Fatalf("old identity: %+v %v", after, err)
	}
	if _, err := store.(*database.SqlStore).DB().Exec(`DROP TRIGGER reject_renamed_ingestion`); err != nil {
		t.Fatal(err)
	}
	launched, err := service.ScanLibrary(lib, false, task.TriggerManual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Await(ctx, launched.Run.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.WatchCleanup(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	after, err = store.GetBook(ctx, before[0].ID)
	if err != nil || after.Path != newPath {
		t.Fatalf("recovered identity: %+v %v", after, err)
	}
}
