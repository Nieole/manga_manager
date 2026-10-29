package scanner

import (
	"context"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestSeriesIngestionAlsoBlocksLibraryCleanupQualification(t *testing.T) {
	_, store, lib, root := newScannerTestLibrary(t)
	seriesPath := filepath.Join(root, "Series")
	if err := os.Mkdir(seriesPath, 0755); err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: seriesPath})
	if err != nil {
		t.Fatal(err)
	}
	scan := NewScanner(store, config.NewManager(&config.Config{}))
	defer scan.Close()
	var active atomic.Bool
	observer := &activeProbeObserver{active: func() bool { return scan.LibraryScanActive(lib.ID) }, saw: &active}
	if _, err := scan.IngestSeries(context.Background(), series.ID, true, observer); err != nil {
		t.Fatal(err)
	}
	if !active.Load() {
		t.Fatal("series ingestion left its library eligible for destructive cleanup")
	}
	if scan.LibraryScanActive(lib.ID) {
		t.Fatal("series ingestion did not release its completion claim")
	}
}
