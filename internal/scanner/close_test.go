package scanner

import (
	"context"
	"manga-manager/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestCloseDrainsStandaloneCoversBeforeCacheRelease(t *testing.T) {
	_, store, lib, root := newScannerTestLibrary(t)
	series := filepath.Join(root, "Series")
	if err := os.MkdirAll(series, 0755); err != nil {
		t.Fatal(err)
	}
	writeScannerTestSeries(t, series, "One.cbz", "Two.cbz")
	cfg := newSlowDiskTestConfig(t)
	scan := NewScanner(store, config.NewManager(cfg))
	if err := scan.ScanLibrary(context.Background(), lib.ID, root, true, nil); err != nil {
		t.Fatal(err)
	}
	scan.Close()
	books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
	if err != nil || len(books) != 2 {
		t.Fatalf("books=%+v err=%v", books, err)
	}
	for _, book := range books {
		if !book.CoverPath.Valid {
			t.Fatalf("cover still pending after close: %+v", book)
		}
		if _, err = os.Stat(filepath.Join(config.ThumbnailDir(*cfg), book.CoverPath.String)); err != nil {
			t.Fatal(err)
		}
	}
	// A released cache must have no writer still creating output after Close returns.
	if err = os.RemoveAll(cfg.Cache.Dir); err != nil {
		t.Fatal(err)
	}
	scan.Close()
}
