package seriesmeta_test

import (
	"database/sql"
	"testing"

	"manga-manager/internal/database"
	"manga-manager/internal/parser"
	"manga-manager/internal/seriesmeta"
)

func TestScanInitialMetadataAndExistingLocks(t *testing.T) {
	info := &parser.ComicInfo{Summary: "Archive summary", Publisher: "Publisher", LanguageISO: "ja", CommunityRating: 4.5}
	initial := seriesmeta.PrepareScanInitial("Directory", info)
	if initial.Title.String != "Directory" || initial.LockedFields.String != "title" || initial.NameInitial != "D" || initial.Summary.String != "Archive summary" || initial.Status.Valid || initial.Rating.Float64 != 4.5 {
		t.Fatalf("initial metadata: %+v", initial)
	}
	store, current := fixture(t)
	current.Title = sql.NullString{String: "Custom", Valid: true}
	current.Summary = sql.NullString{String: "Manual summary", Valid: true}
	current.Publisher = sql.NullString{String: "Old publisher", Valid: true}
	current.Status = sql.NullString{String: "ongoing", Valid: true}
	current.Rating = sql.NullFloat64{Float64: 9, Valid: true}
	current.LockedFields = sql.NullString{String: " summary , rating ,tags,authors ", Valid: true}
	update := seriesmeta.PrepareScanUpdate(current, seriesmeta.ParseScanLocks(current.LockedFields), "Directory", &parser.ComicInfo{})
	if update.Title != current.Title || update.NameInitial != "C" || update.Summary != current.Summary || update.Rating != current.Rating || update.Publisher.Valid || update.Language.Valid || update.Status.Valid || update.LockedFields != current.LockedFields {
		t.Fatalf("updated metadata: %+v", update)
	}
	// The caller composes the metadata with identity and counts in its existing upsert.
	err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		_, err := q.UpsertSeriesByPath(t.Context(), database.UpsertSeriesByPathParams{LibraryID: current.LibraryID, Name: current.Name, Path: current.Path, Title: update.Title, Summary: update.Summary, Publisher: update.Publisher, Status: update.Status, Rating: update.Rating, Language: update.Language, LockedFields: update.LockedFields, NameInitial: update.NameInitial, BookCount: 7, TotalPages: 70})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSeries(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != current.ID || got.Path != current.Path || got.Name != "Alpha" || got.BookCount != 7 || got.TotalPages != 70 || got.Summary.String != "Manual summary" || got.Title.String != "Custom" {
		t.Fatalf("composed series: %+v", got)
	}
}

func TestScanAssociationsAppendDespiteCollectionLocksAndErrors(t *testing.T) {
	store, current := fixture(t)
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		_, err := seriesmeta.SaveManual(t.Context(), q, current, seriesmeta.ManualInput{LockedFields: "tags,authors", Tags: []string{"Old"}, Authors: []seriesmeta.Author{{Name: "Existing", Role: "writer"}}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER reject_tag BEFORE INSERT ON tags WHEN NEW.name = 'Rejected' BEGIN SELECT RAISE(FAIL, 'rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.ExecTx(t.Context(), func(q *database.Queries) error {
		seriesmeta.AppendScanAssociations(t.Context(), q, current.ID, &parser.ComicInfo{Genre: "Rejected, New, Old", Writer: "Writer", Penciller: "Artist"})
		seriesmeta.AppendScanAssociations(t.Context(), q, current.ID, nil)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tags, err := store.GetTagsForSeries(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Name != "New" || tags[1].Name != "Old" {
		t.Fatalf("tags: %+v", tags)
	}
	authors, err := store.GetAuthorsForSeries(t.Context(), current.ID)
	if err != nil {
		t.Fatal(err)
	}
	roles := make(map[string]string)
	for _, author := range authors {
		roles[author.Name] = author.Role
	}
	if len(authors) != 3 || roles["Existing"] != "writer" || roles["Writer"] != "writer" || roles["Artist"] != "penciller" {
		t.Fatalf("authors: %+v", authors)
	}
}

func TestScanTitleFallbackAndLockedScalarMatrix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		title     sql.NullString
		lock      string
		wantTitle sql.NullString
		initial   string
	}{
		{"missing unlocked title", sql.NullString{}, "", sql.NullString{String: "Directory", Valid: true}, "D"},
		{"blank unlocked title", sql.NullString{String: "  ", Valid: true}, "", sql.NullString{String: "Directory", Valid: true}, "D"},
		{"missing locked title", sql.NullString{}, "title", sql.NullString{}, "D"},
		{"existing custom title", sql.NullString{String: "Zulu", Valid: true}, "", sql.NullString{String: "Zulu", Valid: true}, "Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := database.Series{Title: tc.title, Summary: sql.NullString{String: "Summary", Valid: true}, Publisher: sql.NullString{String: "Publisher", Valid: true}, Status: sql.NullString{String: "ongoing", Valid: true}, Language: sql.NullString{String: "ja", Valid: true}, Rating: sql.NullFloat64{Float64: 8, Valid: true}, LockedFields: sql.NullString{String: tc.lock + ",summary,publisher,status,language,rating", Valid: true}}
			got := seriesmeta.PrepareScanUpdate(current, seriesmeta.ParseScanLocks(current.LockedFields), "Directory", &parser.ComicInfo{Summary: "Other", CommunityRating: -1})
			if got.Title != tc.wantTitle || got.NameInitial != tc.initial || got.Summary != current.Summary || got.Publisher != current.Publisher || got.Status != current.Status || got.Language != current.Language || got.Rating != current.Rating {
				t.Fatalf("locked update: %+v", got)
			}
		})
	}
	initial := seriesmeta.PrepareScanInitial("Directory", nil)
	if initial.Summary.Valid || initial.Publisher.Valid || initial.Status.Valid || initial.Language.Valid || initial.Rating.Valid {
		t.Fatalf("nil ComicInfo: %+v", initial)
	}
}
