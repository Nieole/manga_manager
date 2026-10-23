package seriesmeta_test

import (
	"fmt"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/seriesmeta"
)

func TestSelectionPreservesRawScalarsAndRemovesAfterAdding(t *testing.T) {
	store, series := fixture(t)
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		_, err := seriesmeta.SaveManual(t.Context(), q, series, seriesmeta.ManualInput{Title: "Original", Summary: "Summary", Publisher: "Publisher", Status: "ongoing", Language: "ja", LockedFields: "status,publisher,tags", Tags: []string{"Old"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	empty, raw := "", " Raw Publisher "
	ids := []int64{series.ID, series.ID, series.ID + 100}
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		return seriesmeta.EditSelection(t.Context(), q, ids, seriesmeta.SelectionInput{Status: &empty, Publisher: &raw, AddTags: []string{" Keep ", " Remove ", " "}, RemoveTags: []string{" Remove ", "Old", " "}})
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		return seriesmeta.EditSelection(t.Context(), q, ids, seriesmeta.SelectionInput{})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Status.Valid || got.Status.String != "" || got.Publisher.String != raw || got.Title.String != "Original" || got.Summary.String != "Summary" || got.Language.String != "ja" || got.LockedFields.String != "status,publisher,tags" {
		t.Fatalf("metadata changed: %+v", got)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != "Keep" {
		t.Fatalf("tags: %+v", tags)
	}
	var count int
	if err := store.DB().QueryRow("SELECT count(*) FROM series_metadata_provenance").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unexpected provenance: %d", count)
	}
}

func TestBulkOperationsExceedVariableLimit(t *testing.T) {
	store, series := fixture(t)
	ids := make([]int64, 40000)
	for i := range ids {
		ids[i] = series.ID + int64(i)
	}
	status, publisher := "completed", "Publisher"
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		return seriesmeta.EditSelection(t.Context(), q, ids, seriesmeta.SelectionInput{Status: &status, Publisher: &publisher, AddTags: []string{"Action", "Drama"}, RemoveTags: []string{"Drama"}})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.String != status || got.Publisher.String != publisher || len(tags) != 1 || tags[0].Name != "Action" {
		t.Fatalf("large selection: %+v %+v", got, tags)
	}
}

func TestSelectionCrossChunkFailureRollsBackAllChanges(t *testing.T) {
	store, first := fixture(t)
	last, err := store.CreateSeries(t.Context(), database.CreateSeriesParams{LibraryID: first.LibraryID, Name: "Last", Path: first.Path + "-last", NameInitial: "L"})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 40000)
	for i := range ids {
		ids[i] = int64(i) + 1000
	}
	ids[0] = first.ID
	ids[len(ids)-1] = last.ID
	_, err = store.DB().Exec(fmt.Sprintf("CREATE TRIGGER reject_last BEFORE INSERT ON series_tags WHEN NEW.series_id = %d BEGIN SELECT RAISE(ABORT,'last chunk failed'); END", last.ID))
	if err != nil {
		t.Fatal(err)
	}
	status, publisher := "completed", "Publisher"
	err = store.ExecTx(t.Context(), func(q *database.Queries) error {
		return seriesmeta.EditSelection(t.Context(), q, ids, seriesmeta.SelectionInput{Status: &status, Publisher: &publisher, AddTags: []string{"Action"}})
	})
	if err == nil {
		t.Fatal("last chunk succeeded")
	}
	for _, id := range []int64{first.ID, last.ID} {
		got, err := store.GetSeries(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		tags, err := store.GetTagsForSeries(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status.Valid || got.Publisher.Valid || len(tags) != 0 {
			t.Fatalf("partial commit: %+v %+v", got, tags)
		}
	}
	var count int
	if err := store.DB().QueryRow("SELECT count(*) FROM tags WHERE name='Action'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed selection retained newly created tag")
	}
}

func TestEmptySelectionDoesNotCreateTags(t *testing.T) {
	store, _ := fixture(t)
	err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		return seriesmeta.EditSelection(t.Context(), q, nil, seriesmeta.SelectionInput{AddTags: []string{"Unused"}})
	})
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.DB().QueryRow("SELECT count(*) FROM tags").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("empty selection created %d tags", count)
	}
}
