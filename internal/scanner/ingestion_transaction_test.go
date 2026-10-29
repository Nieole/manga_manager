package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"manga-manager/internal/config"
)

func TestScanRecoversNextBatchAfterSeriesCreationRollsBack(t *testing.T) {
	_, store, lib, root := newScannerTestLibrary(t)
	if err := os.MkdirAll(filepath.Join(root, "Series"), 0700); err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 101; n++ {
		if err := writeScannerTestCBZ(filepath.Join(root, "Series", fmt.Sprintf("%03d.cbz", n)), map[string][]byte{"001.png": testPNG1x1}); err != nil {
			t.Fatal(err)
		}
	}
	// A deferred constraint fails at commit while the transaction still owns every write.
	if _, err := store.DB().Exec(`CREATE TABLE ingestion_guard (book_id INTEGER REFERENCES books(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER reject_first_ingestion AFTER INSERT ON books WHEN NEW.name='000.cbz' BEGIN INSERT INTO ingestion_guard(book_id) VALUES(-1); END`); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = t.TempDir()
	s := NewScanner(store, config.NewManager(cfg))
	defer s.Close()
	observer := &spyObserver{}
	if err := s.ScanLibrary(context.Background(), lib.ID, root, false, observer); err != nil {
		t.Fatal(err)
	}
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || filepath.Base(books[0].Path) != "100.cbz" {
		t.Fatalf("rolled-back series leaked into next batch: books=%+v", books)
	}
	report := observer.lastMetrics()
	if report.FailedArchives != 100 {
		t.Fatalf("dropped batch facts=%+v", report)
	}
	book, err := store.GetBook(context.Background(), books[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.GetSeries(context.Background(), book.SeriesID)
	if err != nil || series.BookCount != 1 {
		t.Fatalf("committed series facts=%+v %v", series, err)
	}
}

func TestScanDoesNotReportRolledBackRenameAsCommitted(t *testing.T) {
	_, store, lib, root := newScannerTestLibrary(t)
	seriesRoot := filepath.Join(root, "Series Alpha")
	oldPath, newPath := filepath.Join(seriesRoot, "old.cbz"), filepath.Join(seriesRoot, "new.cbz")
	if err := writeScannerTestCBZ(oldPath, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatal(err)
	}
	s := newRehomeScanner(t, store)
	defer s.Close()
	if err := s.ScanLibrary(context.Background(), lib.ID, root, false, nil); err != nil {
		t.Fatal(err)
	}
	before, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("seed=%+v %v", before, err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TABLE ingestion_guard (book_id INTEGER REFERENCES books(id) DEFERRABLE INITIALLY DEFERRED); CREATE TRIGGER reject_renamed_ingestion AFTER UPDATE ON books WHEN NEW.name='new.cbz' BEGIN INSERT INTO ingestion_guard(book_id) VALUES(-1); END`); err != nil {
		t.Fatal(err)
	}
	observer := &spyObserver{}
	if err := s.ScanLibrary(context.Background(), lib.ID, root, false, observer); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetBook(context.Background(), before[0].ID)
	if err != nil || after.Path != oldPath {
		t.Fatalf("rollback did not preserve old identity: %+v %v", after, err)
	}
	metrics := observer.lastMetrics()
	if metrics.RehomedBooks != 0 || metrics.FailedArchives != 1 {
		t.Fatalf("uncommitted rename reported as fact: %+v", metrics)
	}
}
