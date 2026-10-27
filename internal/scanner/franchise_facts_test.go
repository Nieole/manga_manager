package scanner

import (
	"context"
	"path/filepath"
	"testing"

	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/seriesmeta"
)

func TestScannerIngestionAndCleanupPersistFranchiseFactChanges(t *testing.T) {
	s, store, lib, libraryPath, _ := newSeriesLockScanner(t)
	defer s.Close()
	ctx := context.Background()
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatal(err)
	}
	waitForScannerCoverQueue(t, s)
	first := seriesUnderTest(t, store, lib.ID)
	missing, err := store.CreateSeries(ctx, database.CreateSeriesParams{LibraryID: lib.ID, Name: "Missing companion", Path: filepath.Join(libraryPath, "Missing companion"), NameInitial: "M"})
	if err != nil {
		t.Fatal(err)
	}
	service := organize.NewRelations(store, func(func()) {})
	if _, err := service.Create(ctx, first.ID, missing.ID, "sequel"); err != nil {
		t.Fatal(err)
	}
	if _, err := seriesediting.New(seriesediting.NewSQLStore(store)).Save(ctx, first.ID, seriesmeta.ManualInput{Title: "", LockedFields: ""}); err != nil {
		t.Fatal(err)
	}
	if err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := service.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatal(err)
	}
	waitForScannerCoverQueue(t, s)
	after, err := service.FranchiseState(ctx)
	if err != nil || !after.Stale || after.DesiredVersion <= before.DesiredVersion {
		t.Fatalf("ingestion responsibility=%+v %v", after, err)
	}
	if err := service.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	stable, err := service.FranchiseState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ScanLibrary(ctx, lib.ID, libraryPath, true, nil); err != nil {
		t.Fatal(err)
	}
	waitForScannerCoverQueue(t, s)
	unchanged, err := service.FranchiseState(ctx)
	if err != nil || unchanged != stable {
		t.Fatalf("unchanged ingestion dirtied projection: %+v %v", unchanged, err)
	}
	if err := s.CleanupLibrary(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	cleaned, err := service.FranchiseState(ctx)
	if err != nil || !cleaned.Stale || cleaned.DesiredVersion <= after.DesiredVersion {
		t.Fatalf("cleanup responsibility=%+v %v", cleaned, err)
	}
	relations, err := service.List(ctx, first.ID)
	if err != nil || len(relations) != 0 {
		t.Fatalf("cleanup relations=%+v %v", relations, err)
	}
}
