package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestGetHealthReport(t *testing.T) {
	store := newHealthTestStore(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name:                "Library",
		Path:                filepath.Join(t.TempDir(), "library"),
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "cbz,cbr",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID:    lib.ID,
		Name:         "Series A",
		Path:         filepath.Join(lib.Path, "Series A"),
		LockedFields: sql.NullString{String: "[]", Valid: true},
		NameInitial:  "S",
	})
	if err != nil {
		t.Fatalf("create series failed: %v", err)
	}
	book, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Book A.cbz",
		Path:           filepath.Join(series.Path, "Book A.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		PageCount:      0,
	})
	if err != nil {
		t.Fatalf("create book failed: %v", err)
	}
	_, err = store.CreateBook(ctx, CreateBookParams{
		SeriesID:       series.ID,
		LibraryID:      lib.ID,
		Name:           "Book B.cbz",
		Path:           filepath.Join(series.Path, "Book B.cbz"),
		Size:           1024,
		FileModifiedAt: time.Now(),
		PageCount:      120,
	})
	if err != nil {
		t.Fatalf("create second book failed: %v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET file_hash = 'dup' WHERE series_id = ?`, series.ID); err != nil {
		t.Fatalf("update duplicate hashes failed: %v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET quick_hash = 'qdup' WHERE series_id = ?`, series.ID); err != nil {
		t.Fatalf("update duplicate quick hashes failed: %v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET cover_path = 'cover.webp' WHERE id = ?`, book.ID); err != nil {
		t.Fatalf("update cover path failed: %v", err)
	}
	if _, err := store.DB().Exec(`INSERT INTO koreader_progress (username, document, progress, percentage, device, device_id, book_id) VALUES ('reader', 'missing.cbz', '{}', 0.5, 'device', 'id', NULL)`); err != nil {
		t.Fatalf("insert unmatched progress failed: %v", err)
	}

	report, err := store.GetHealthReport(ctx, HealthIssueFilters{LibraryID: lib.ID, Limit: 10})
	if err != nil {
		t.Fatalf("get health report failed: %v", err)
	}
	summary := healthSummaryMap(report.Summary)
	if summary["empty_pages"] != 1 {
		t.Fatalf("expected one empty page book, got %+v", summary)
	}
	if summary["missing_cover"] != 1 {
		t.Fatalf("expected one missing cover book, got %+v", summary)
	}
	if summary["missing_metadata"] != 1 {
		t.Fatalf("expected one missing metadata series, got %+v", summary)
	}
	if summary["missing_page_manifest"] != 1 {
		t.Fatalf("expected one missing manifest book, got %+v", summary)
	}
	if summary["duplicate_file_hash"] != 2 {
		t.Fatalf("expected two duplicate hash book entries, got %+v", summary)
	}
	if summary["duplicate_quick_hash"] != 2 {
		t.Fatalf("expected two duplicate quick hash book entries, got %+v", summary)
	}
	if summary["unmatched_koreader"] != 1 {
		t.Fatalf("expected one unmatched koreader item, got %+v", summary)
	}

	filtered, err := store.GetHealthReport(ctx, HealthIssueFilters{LibraryID: lib.ID, Type: "missing_cover", Limit: 10})
	if err != nil {
		t.Fatalf("get filtered health report failed: %v", err)
	}
	if len(filtered.Summary) != 1 || filtered.Summary[0].Type != "missing_cover" {
		t.Fatalf("expected only missing_cover summary, got %+v", filtered.Summary)
	}
	if len(filtered.Issues) != 1 || filtered.Issues[0].BookID == nil {
		t.Fatalf("expected one missing cover book issue, got %+v", filtered.Issues)
	}
}

func newHealthTestStore(t *testing.T) *SqlStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "health.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store.(*SqlStore)
}

func healthSummaryMap(items []HealthIssueSummary) map[string]int64 {
	result := make(map[string]int64, len(items))
	for _, item := range items {
		result[item.Type] = item.Count
	}
	return result
}
