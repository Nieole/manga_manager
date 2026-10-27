package seriesediting_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"manga-manager/internal/application/seriesediting"
	"manga-manager/internal/database"
	"manga-manager/internal/seriesmeta"
)

func TestSelectionRetainsCommittedMetadataWhenStatisticsFail(t *testing.T) {
	store, series := fixture(t)
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_selection_stats BEFORE INSERT ON series_stats BEGIN SELECT RAISE(ABORT, 'stats unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	status := " completed "
	ids := make([]int64, 502)
	for i := range ids {
		ids[i] = series.ID
	}
	ids[501] = 99999
	result, err := seriesediting.NewSelection(seriesediting.NewSQLStore(store), nil).Edit(t.Context(), ids, seriesmeta.SelectionInput{Status: &status, AddTags: []string{" Keep ", "Remove"}, RemoveTags: []string{"Remove"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || result.Updated != 502 || len(result.SeriesIDs) != 1 || result.SeriesIDs[0] != series.ID || len(result.RefreshFailedIDs) != 1 || result.RefreshFailedIDs[0] != series.ID {
		t.Fatalf("committed result: %+v", result)
	}
	saved, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status.String != " completed " || len(tags) != 1 || tags[0].Name != "Keep" {
		t.Fatalf("selection semantics: %+v %+v", saved, tags)
	}
}

func TestSelectionFailureInLaterSQLBatchRollsBackAllMetadata(t *testing.T) {
	store, first := fixture(t)
	second, err := store.CreateSeries(t.Context(), database.CreateSeriesParams{LibraryID: first.LibraryID, Name: "Second", Path: filepath.Join(filepath.Dir(first.Path), "Second"), NameInitial: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER reject_later_selection BEFORE INSERT ON series_tags WHEN NEW.series_id = %d BEGIN SELECT RAISE(ABORT, 'later link unavailable'); END;`, second.ID)); err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 501)
	for i := range ids {
		ids[i] = first.ID
	}
	ids[500] = second.ID
	status := "completed"
	result, err := seriesediting.NewSelection(seriesediting.NewSQLStore(store), nil).Edit(t.Context(), ids, seriesmeta.SelectionInput{Status: &status, AddTags: []string{"Must roll back"}})
	if err == nil || result.Committed || result.Updated != 0 || len(result.SeriesIDs) != 0 {
		t.Fatalf("failed selection result: %+v %v", result, err)
	}
	for _, id := range []int64{first.ID, second.ID} {
		saved, err := store.GetSeries(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		tags, err := store.GetTagsForSeries(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Status.Valid || len(tags) != 0 {
			t.Fatalf("failed selection leaked metadata: %+v %+v", saved, tags)
		}
	}
}

func TestSelectionRefreshesRemainingSeriesAfterOneStatisticsFailure(t *testing.T) {
	store, first := fixture(t)
	second, err := store.CreateSeries(t.Context(), database.CreateSeriesParams{LibraryID: first.LibraryID, Name: "Second", Path: filepath.Join(filepath.Dir(first.Path), "Second"), NameInitial: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER reject_first_selection_stats BEFORE INSERT ON series_stats WHEN NEW.series_id = %d BEGIN SELECT RAISE(ABORT, 'stats unavailable'); END;`, first.ID)); err != nil {
		t.Fatal(err)
	}
	empty := ""
	result, err := seriesediting.NewSelection(seriesediting.NewSQLStore(store), nil).Edit(t.Context(), []int64{first.ID, second.ID}, seriesmeta.SelectionInput{Publisher: &empty, AddTags: []string{"Published"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Committed || len(result.SeriesIDs) != 2 || len(result.RefreshFailedIDs) != 1 || result.RefreshFailedIDs[0] != first.ID {
		t.Fatalf("partial statistics result: %+v", result)
	}
	saved, err := store.GetSeries(t.Context(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Publisher.Valid || saved.Publisher.String != "" || saved.Status.Valid {
		t.Fatalf("empty and absent scalars: %+v", saved)
	}
	var cache string
	if err := store.DB().QueryRow("SELECT tag_names_cache FROM series_stats WHERE series_id = ?", second.ID).Scan(&cache); err != nil {
		t.Fatal(err)
	}
	if cache != "Published" {
		t.Fatalf("later statistics not refreshed: %q", cache)
	}
}
