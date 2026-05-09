package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkSearchSeriesPaged(b *testing.B) {
	ctx := context.Background()
	store := newBenchmarkStore(b)
	seedBenchmarkLibrary(b, store, 1000, 2)

	cases := []struct {
		name    string
		keyword string
		letter  string
		status  string
		sortBy  string
	}{
		{name: "name_page", sortBy: "name_asc"},
		{name: "updated_desc", sortBy: "updated_desc"},
		{name: "letter_filter", letter: "A", sortBy: "name_asc"},
		{name: "status_filter", status: "ongoing", sortBy: "books_desc"},
		{name: "keyword", keyword: "Series 09", sortBy: "name_asc"},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rows, total, err := store.SearchSeriesPaged(ctx, 1, tc.keyword, tc.letter, tc.status, nil, nil, 50, 0, tc.sortBy)
				if err != nil {
					b.Fatalf("search series failed: %v", err)
				}
				if total == 0 || len(rows) == 0 {
					b.Fatalf("expected non-empty result, got rows=%d total=%d", len(rows), total)
				}
			}
		})
	}
}

func newBenchmarkStore(b *testing.B) *SqlStore {
	b.Helper()

	dbPath := filepath.Join(b.TempDir(), "bench.db")
	if err := Migrate(dbPath); err != nil {
		b.Fatalf("migrate failed: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		b.Fatalf("new store failed: %v", err)
	}
	b.Cleanup(func() { _ = store.Close() })
	return store.(*SqlStore)
}

func seedBenchmarkLibrary(b *testing.B, store *SqlStore, seriesCount, booksPerSeries int) {
	b.Helper()

	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name:                "Benchmark",
		Path:                filepath.Join(b.TempDir(), "library"),
		ScanMode:            "none",
		KoreaderSyncEnabled: true,
		ScanInterval:        60,
		ScanFormats:         "cbz,cbr,zip,rar",
	})
	if err != nil {
		b.Fatalf("create library failed: %v", err)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatalf("begin tx failed: %v", err)
	}
	q := store.Queries.WithTx(tx)
	now := time.Now()
	for i := 0; i < seriesCount; i++ {
		status := "completed"
		if i%3 == 0 {
			status = "ongoing"
		}
		series, err := q.CreateSeries(ctx, CreateSeriesParams{
			LibraryID:    lib.ID,
			Name:         fmt.Sprintf("Series %04d", i),
			Path:         fmt.Sprintf("/benchmark/Series %04d", i),
			Title:        sql.NullString{String: fmt.Sprintf("Benchmark Series %04d", i), Valid: true},
			Status:       sql.NullString{String: status, Valid: true},
			Rating:       sql.NullFloat64{Float64: float64(i % 10), Valid: true},
			Language:     sql.NullString{String: "zh", Valid: true},
			LockedFields: sql.NullString{String: "[]", Valid: true},
			NameInitial:  string(rune('A' + i%26)),
		})
		if err != nil {
			_ = tx.Rollback()
			b.Fatalf("create series failed: %v", err)
		}
		for j := 0; j < booksPerSeries; j++ {
			if _, err := q.CreateBook(ctx, CreateBookParams{
				SeriesID:       series.ID,
				LibraryID:      lib.ID,
				Name:           fmt.Sprintf("Book %04d-%02d.cbz", i, j),
				Path:           fmt.Sprintf("/benchmark/Series %04d/Book %02d.cbz", i, j),
				Size:           int64(1024 + i + j),
				FileModifiedAt: now.Add(-time.Duration(i+j) * time.Minute),
				Volume:         fmt.Sprintf("%02d", j+1),
				Title:          sql.NullString{String: fmt.Sprintf("Book %02d", j+1), Valid: true},
				Number:         sql.NullString{String: fmt.Sprintf("%d", j+1), Valid: true},
				SortNumber:     sql.NullFloat64{Float64: float64(j + 1), Valid: true},
				PageCount:      180,
			}); err != nil {
				_ = tx.Rollback()
				b.Fatalf("create book failed: %v", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit seed tx failed: %v", err)
	}
}
