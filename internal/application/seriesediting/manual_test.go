package seriesediting_test

import (
	"errors"
	"path/filepath"
	"testing"

	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/database"
	"manga-manager/internal/seriesmeta"
)

func fixture(t *testing.T) (database.Store, database.Series) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manual.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	lib, err := store.CreateLibrary(t.Context(), database.CreateLibraryParams{Name: "Library", Path: t.TempDir(), ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(t.Context(), database.CreateSeriesParams{LibraryID: lib.ID, Name: "Alpha", Path: filepath.Join(lib.Path, "Alpha"), NameInitial: "A"})
	if err != nil {
		t.Fatal(err)
	}
	return store, series
}

func TestManualSaveRollsBackRequiredStatisticsAndConflictingVersion(t *testing.T) {
	store, series := fixture(t)
	editor := seriesediting.New(seriesediting.NewSQLStore(store))
	_, err := editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Title: "Original", Tags: []string{"Original"}})
	if err != nil {
		t.Fatal(err)
	}
	version, err := seriesmeta.LoadVersion(t.Context(), store, series.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Title: "Another writer"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Title: "Stale", ExpectedVersion: &version})
	if !errors.Is(err, seriesmeta.ErrConflict) || result.CurrentVersion == "" || result.Series.ID != 0 {
		t.Fatalf("conflict result: %+v %v", result, err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_statistics BEFORE INSERT ON series_stats BEGIN SELECT RAISE(ABORT, 'stats unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	result, err = editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Title: "Must roll back", Tags: []string{"Replacement"}})
	if err == nil || result.Series.ID != 0 {
		t.Fatalf("failed save reported committed: %+v %v", result, err)
	}
	saved, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Title.String != "Another writer" || len(tags) != 1 || tags[0].Name != "Original" {
		t.Fatalf("failed save leaked edits: %+v %+v", saved, tags)
	}
}

func TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections(t *testing.T) {
	store, series := fixture(t)
	editor := seriesediting.New(seriesediting.NewSQLStore(store))
	result, err := editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Title: "Beta", LockedFields: "title", Tags: []string{"Keep"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Series.ID != series.ID || result.Series.Title.String != "Beta" || result.Series.NameInitial != "B" {
		t.Fatalf("saved view: %+v", result)
	}
	result, err = editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Title: "Gamma"})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "Keep" {
		t.Fatalf("absent tags replaced: %+v", tags)
	}
	result, err = editor.Save(t.Context(), series.ID, seriesmeta.ManualInput{Tags: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	tags, err = store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 0 || result.Series.Title.Valid {
		t.Fatalf("explicit clear failed: %+v %+v", result, tags)
	}
}
