package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
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
	if !rows[0].LastReadAt.Valid {
		t.Fatalf("expected last_read_at to be populated, got %+v", rows[0].LastReadAt)
	}
	if !rows[0].LastReadBookID.Valid || rows[0].LastReadBookID.Int64 != book.ID {
		t.Fatalf("expected last_read_book_id=%d, got %+v", book.ID, rows[0].LastReadBookID)
	}
	if !rows[0].LastReadPage.Valid || rows[0].LastReadPage.Int64 != 7 {
		t.Fatalf("expected last_read_page=7, got %+v", rows[0].LastReadPage)
	}
}

// TestSearchSeriesPagedReturnsNullLastReadForUnreadSeries 验证未阅读系列三个 last_read 字段都为 NULL。
func TestSearchSeriesPagedReturnsNullLastReadForUnreadSeries(t *testing.T) {
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
		t.Fatalf("create library: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Untouched",
		Path:        filepath.Join(lib.Path, "Untouched"),
		NameInitial: "U",
	})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}
	if _, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "u01.cbz",
		Path:           filepath.Join(series.Path, "u01.cbz"),
		Size:           1,
		FileModifiedAt: time.Now(),
		PageCount:      10,
	}); err != nil {
		t.Fatalf("create book: %v", err)
	}

	rows, _, err := store.SearchSeriesPaged(ctx, lib.ID, "", "", "", nil, nil, 10, 0, "name_asc")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].LastReadAt.Valid || rows[0].LastReadBookID.Valid || rows[0].LastReadPage.Valid {
		t.Fatalf("expected all last_read_* fields to be NULL, got %+v", rows[0])
	}
}

func TestSearchSeriesCursorSupportsKeysetSorts(t *testing.T) {
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

	names := []string{"Alpha", "Beta", "Gamma", "Delta"}
	createdBase := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	for idx, name := range names {
		series, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        filepath.Join(lib.Path, name),
			NameInitial: SeriesInitial("", name),
		})
		if err != nil {
			t.Fatalf("create series %s failed: %v", name, err)
		}
		favorite := 0
		if name == "Gamma" || name == "Delta" {
			favorite = 1
		}
		if _, err := store.(*SqlStore).db.ExecContext(ctx,
			`UPDATE series SET created_at = ?, updated_at = ?, is_favorite = ? WHERE id = ?`,
			createdBase.Add(time.Duration(idx)*time.Hour),
			createdBase.Add(time.Duration(10-idx)*time.Hour),
			favorite,
			series.ID,
		); err != nil {
			t.Fatalf("update series %s ordering fields failed: %v", name, err)
		}
	}

	assertCursorOrder := func(sortBy string, expected []string) {
		t.Helper()
		var got []string
		cursor := ""
		for {
			rows, nextCursor, hasMore, err := store.SearchSeriesCursor(ctx, lib.ID, "", "", "", nil, nil, 2, sortBy, cursor)
			if err != nil {
				t.Fatalf("cursor search %s failed: %v", sortBy, err)
			}
			for _, row := range rows {
				got = append(got, row.Name)
			}
			if !hasMore {
				break
			}
			if nextCursor == "" {
				t.Fatalf("expected next cursor for %s", sortBy)
			}
			cursor = nextCursor
		}
		if strings.Join(got, ",") != strings.Join(expected, ",") {
			t.Fatalf("unexpected order for %s: got %v want %v", sortBy, got, expected)
		}
	}

	assertCursorOrder("name_asc", []string{"Alpha", "Beta", "Delta", "Gamma"})
	assertCursorOrder("updated_desc", []string{"Alpha", "Beta", "Gamma", "Delta"})
	assertCursorOrder("created_asc", []string{"Alpha", "Beta", "Gamma", "Delta"})
	assertCursorOrder("favorite_desc", []string{"Delta", "Gamma", "Alpha", "Beta"})
}

func TestSearchTagsAndAuthorsReturnsPopularAndQueryMatches(t *testing.T) {
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
	for _, name := range []string{"Alpha", "Beta"} {
		series, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        filepath.Join(lib.Path, name),
			NameInitial: SeriesInitial("", name),
		})
		if err != nil {
			t.Fatalf("create series failed: %v", err)
		}
		action, err := store.UpsertTag(ctx, "Action")
		if err != nil {
			t.Fatalf("upsert action tag failed: %v", err)
		}
		if err := store.LinkSeriesTag(ctx, LinkSeriesTagParams{SeriesID: series.ID, TagID: action.ID}); err != nil {
			t.Fatalf("link action tag failed: %v", err)
		}
		writer, err := store.UpsertAuthor(ctx, UpsertAuthorParams{Name: "Writer A", Role: "writer"})
		if err != nil {
			t.Fatalf("upsert writer failed: %v", err)
		}
		if err := store.LinkSeriesAuthor(ctx, LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: writer.ID}); err != nil {
			t.Fatalf("link writer failed: %v", err)
		}
	}
	mystery, err := store.UpsertTag(ctx, "Mystery")
	if err != nil {
		t.Fatalf("upsert mystery tag failed: %v", err)
	}
	artist, err := store.UpsertAuthor(ctx, UpsertAuthorParams{Name: "Artist B", Role: "artist"})
	if err != nil {
		t.Fatalf("upsert artist failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:   lib.ID,
		Name:        "Gamma",
		Path:        filepath.Join(lib.Path, "Gamma"),
		NameInitial: "G",
	})
	if err != nil {
		t.Fatalf("create gamma failed: %v", err)
	}
	if err := store.LinkSeriesTag(ctx, LinkSeriesTagParams{SeriesID: series.ID, TagID: mystery.ID}); err != nil {
		t.Fatalf("link mystery failed: %v", err)
	}
	if err := store.LinkSeriesAuthor(ctx, LinkSeriesAuthorParams{SeriesID: series.ID, AuthorID: artist.ID}); err != nil {
		t.Fatalf("link artist failed: %v", err)
	}

	tags, err := store.SearchTags(ctx, "", 1)
	if err != nil {
		t.Fatalf("search popular tags failed: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "Action" {
		t.Fatalf("expected most used Action tag, got %+v", tags)
	}
	tags, err = store.SearchTags(ctx, "mys", 10)
	if err != nil {
		t.Fatalf("search tags failed: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "Mystery" {
		t.Fatalf("expected Mystery tag match, got %+v", tags)
	}

	authors, err := store.SearchAuthors(ctx, "", 1)
	if err != nil {
		t.Fatalf("search popular authors failed: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "Writer A" {
		t.Fatalf("expected most used Writer A author, got %+v", authors)
	}
	authors, err = store.SearchAuthors(ctx, "artist", 10)
	if err != nil {
		t.Fatalf("search authors failed: %v", err)
	}
	if len(authors) != 1 || authors[0].Name != "Artist B" {
		t.Fatalf("expected Artist B author match, got %+v", authors)
	}
}
