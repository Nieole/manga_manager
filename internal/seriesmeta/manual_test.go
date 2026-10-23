package seriesmeta_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/seriesmeta"
)

func fixture(t *testing.T) (database.Store, database.Series) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
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

func TestManualPreservesRawValuesAndAbsentCollections(t *testing.T) {
	store, series := fixture(t)
	save := func(input seriesmeta.ManualInput) {
		t.Helper()
		if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
			_, err := seriesmeta.SaveManual(t.Context(), q, series, input)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	save(seriesmeta.ManualInput{Title: "Beta", LockedFields: "title,tags", Tags: []string{" Tag ", " "}, Authors: []seriesmeta.Author{{Name: " Author ", Role: "story"}}, Links: []seriesmeta.Link{{Name: " Site ", Url: " https://example.com "}}})
	save(seriesmeta.ManualInput{Title: "Charlie", Rating: -1})
	got, err := store.GetSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title.String != "Charlie" || got.NameInitial != "C" || got.Summary.Valid || got.Rating.Valid || got.LockedFields.String != "" {
		t.Fatalf("unexpected metadata: %+v", got)
	}
	tags, err := store.GetTagsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 || tags[0].Name != " Tag " {
		t.Fatalf("tags: %+v", tags)
	}
	authors, err := store.GetAuthorsForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(authors) != 1 || authors[0].Name != " Author " || authors[0].Role != "story" {
		t.Fatalf("authors: %+v", authors)
	}
	links, err := store.GetLinksForSeries(t.Context(), series.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Name != " Site " || links[0].Url != " https://example.com " {
		t.Fatalf("links: %+v", links)
	}
	save(seriesmeta.ManualInput{Tags: []string{}, Authors: []seriesmeta.Author{}, Links: []seriesmeta.Link{}})
	tags, _ = store.GetTagsForSeries(t.Context(), series.ID)
	authors, _ = store.GetAuthorsForSeries(t.Context(), series.ID)
	links, _ = store.GetLinksForSeries(t.Context(), series.ID)
	if len(tags)+len(authors)+len(links) != 0 {
		t.Fatal("explicit empty collections were not cleared")
	}
}

func TestManualConflictAndAssociationFailurePreserveContent(t *testing.T) {
	for _, table := range []string{"series_tags", "series_authors", "series_links"} {
		t.Run(table, func(t *testing.T) {
			store, series := fixture(t)
			input := seriesmeta.ManualInput{Title: "Original", Summary: "Summary", Publisher: "Publisher", Status: "ongoing", Rating: 8.5, Language: "ja", LockedFields: "title", Tags: []string{"Old"}, Authors: []seriesmeta.Author{{Name: "Writer", Role: "story"}}, Links: []seriesmeta.Link{{Name: "Site", Url: "https://example.com"}}}
			save := func() error {
				return store.ExecTx(t.Context(), func(q *database.Queries) error {
					_, err := seriesmeta.SaveManual(t.Context(), q, series, input)
					return err
				})
			}
			if err := save(); err != nil {
				t.Fatal(err)
			}
			version, err := seriesmeta.LoadVersion(t.Context(), store, series.ID)
			if err != nil {
				t.Fatal(err)
			}
			stale := "stale"
			input.ExpectedVersion = &stale
			input.Title = "Changed"
			var current string
			err = store.ExecTx(t.Context(), func(q *database.Queries) error {
				var e error
				current, e = seriesmeta.SaveManual(t.Context(), q, series, input)
				return e
			})
			if !errors.Is(err, seriesmeta.ErrConflict) || current != version {
				t.Fatalf("conflict: version=%q error=%v", current, err)
			}
			input.ExpectedVersion = &version
			_, err = store.DB().Exec(fmt.Sprintf("CREATE TRIGGER reject_association BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT, 'rejected'); END", table))
			if err != nil {
				t.Fatal(err)
			}
			if err = save(); err == nil {
				t.Fatal("association failure succeeded")
			}
			after, err := seriesmeta.LoadVersion(t.Context(), store, series.ID)
			if err != nil {
				t.Fatal(err)
			}
			if after != version {
				t.Fatalf("failed save changed content: %q => %q", version, after)
			}
		})
	}
}
