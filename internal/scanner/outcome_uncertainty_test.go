package scanner

import (
	"context"
	"errors"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/storageio"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type unavailableSeriesBookSnapshot struct{ database.Store }

func (unavailableSeriesBookSnapshot) ListBooksBySeries(context.Context, int64) ([]database.Book, error) {
	return nil, errors.New("snapshot unavailable")
}

func TestSeriesSnapshotFailureCannotQualifyCleanup(t *testing.T) {
	_, store, lib, root := newScannerTestLibrary(t)
	dir := filepath.Join(root, "Series")
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(context.Background(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Series", Path: dir})
	if err != nil {
		t.Fatal(err)
	}
	scan := NewScanner(unavailableSeriesBookSnapshot{store}, config.NewManager(&config.Config{}))
	defer scan.Close()
	outcome, err := scan.IngestSeries(context.Background(), series.ID, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.CleanupEligible() || outcome.Status != ScanPartial {
		t.Fatalf("unavailable snapshot became complete: %+v", outcome)
	}
}

func TestLostFingerprintSourceCannotQualifyCleanup(t *testing.T) {
	_, store, lib, root := newScannerTestLibrary(t)
	path := filepath.Join(root, "Series", "old.cbz")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeScannerTestCBZ(path, map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileIdentity
	cfg.Cache.Dir = t.TempDir()
	cfg.Library.StorageProfile = config.StorageProfileCustom
	cfg.Library.IOPolicy = config.StorageIOPolicy{ArchiveOpenConcurrency: 2, HashConcurrency: 1, CoverConcurrency: 2}
	config.NormalizeConfig(&cfg)
	scheduler := storageio.NewScheduler()
	prior := storageio.Default
	storageio.Default = scheduler
	defer func() { storageio.Default = prior }()
	scan := NewScanner(store, config.NewManager(&cfg))
	defer scan.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	lease, err := scheduler.Acquire(ctx, storageio.Request{VolumeKey: config.VolumeKey(path), Limit: 1, Kind: storageio.WorkKindIdentityHash})
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	type result struct {
		outcome ScanOutcome
		err     error
	}
	done := make(chan result, 1)
	go func() {
		outcome, err := scan.IngestLibrary(ctx, lib.ID, root, LibraryScanOptions{}, nil)
		done <- result{outcome, err}
	}()
	waiting := false
	for !waiting {
		for _, volume := range scheduler.Snapshot() {
			waiting = waiting || volume.BackgroundWaiting > 0
		}
		select {
		case result := <-done:
			t.Fatalf("copy never reached hash barrier: %+v", result)
		default:
		}
		if ctx.Err() != nil {
			t.Fatal("hash barrier was not reached")
		}
		if !waiting {
			time.Sleep(time.Millisecond)
		}
	}
	// Archive metadata is already read; only the complete and quick fingerprint IO is blocked.
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatal(err)
	}
	lease.Release()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.outcome.CleanupEligible() || result.outcome.Status != ScanPartial {
			t.Fatalf("failed fingerprints qualified cleanup: %+v", result.outcome)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
