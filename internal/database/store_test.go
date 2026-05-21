package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func newStoreForTest(t *testing.T) Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSeriesStatsRefreshDrivesSearchSeriesPaged(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name:                "Main",
		Path:                filepath.Join(t.TempDir(), "library"),
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "cbz",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Alpha",
		Path:        filepath.Join(lib.Path, "Alpha"),
		NameInitial: "A",
	})
	if err != nil {
		t.Fatalf("create series failed: %v", err)
	}
	book, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Alpha 01.cbz",
		Path:           filepath.Join(series.Path, "Alpha 01.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		SortNumber:     sql.NullFloat64{Float64: 1, Valid: true},
		PageCount:      20,
		CoverPath:      sql.NullString{String: "cover.webp", Valid: true},
	})
	if err != nil {
		t.Fatalf("create book failed: %v", err)
	}
	tag, err := store.UpsertTag(ctx, "Action")
	if err != nil {
		t.Fatalf("upsert tag failed: %v", err)
	}
	if err := store.LinkSeriesTag(ctx, LinkSeriesTagParams{SeriesID: series.ID, TagID: tag.ID}); err != nil {
		t.Fatalf("link tag failed: %v", err)
	}
	author, err := store.UpsertAuthor(ctx, UpsertAuthorParams{Name: "Writer A", Role: "writer"})
	if err != nil {
		t.Fatalf("upsert author failed: %v", err)
	}
	if err := store.LinkSeriesAuthor(ctx, LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: author.ID}); err != nil {
		t.Fatalf("link author failed: %v", err)
	}
	if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 7, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           book.ID,
	}); err != nil {
		t.Fatalf("update progress failed: %v", err)
	}

	rows, total, err := store.SearchSeriesPaged(ctx, lib.ID, "", "", "", []string{"Action"}, []string{"Writer A"}, 10, 0, "read_desc")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if total != 1 || len(rows) != 1 || rows[0].ID != series.ID {
		t.Fatalf("unexpected search result total=%d rows=%+v", total, rows)
	}
	if !rows[0].CoverPath.Valid || rows[0].CoverPath.String != "cover.webp" {
		t.Fatalf("expected cover path from stats, got %+v", rows[0].CoverPath)
	}
	if rows[0].TagsString == nil || *rows[0].TagsString != "Action" {
		t.Fatalf("expected tag cache from stats, got %+v", rows[0].TagsString)
	}
	if rows[0].ReadCount != 7 {
		t.Fatalf("expected read count from stats, got %d", rows[0].ReadCount)
	}
}
