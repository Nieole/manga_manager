package scanner

import (
	"context"
	"testing"
)

func TestScanMetadataFailurePathsKeepTheirBatchSemantics(t *testing.T) {
	for _, tc := range []struct {
		name, trigger         string
		existing              bool
		wantSeries, wantBooks int
	}{
		{name: "first series failure skips book", trigger: `CREATE TRIGGER reject_series BEFORE INSERT ON series BEGIN SELECT RAISE(FAIL, 'reject initial series'); END`, wantSeries: 0, wantBooks: 0},
		{name: "existing metadata failure keeps book", existing: true, trigger: `CREATE TRIGGER reject_metadata BEFORE UPDATE OF summary ON series BEGIN SELECT RAISE(FAIL, 'reject update'); END`, wantSeries: 1, wantBooks: 1},
		{name: "book transaction failure rolls back series", trigger: `CREATE TRIGGER reject_book BEFORE INSERT ON books BEGIN SELECT RAISE(ROLLBACK, 'reject batch'); END`, wantSeries: 0, wantBooks: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, lib, path, _ := newSeriesLockScanner(t)
			if tc.existing {
				if err := s.ScanLibrary(t.Context(), lib.ID, path, true, nil); err != nil {
					t.Fatal(err)
				}
				waitForScannerCoverQueue(t, s)
				if _, err := store.DB().Exec(`DELETE FROM books; UPDATE series SET summary = 'Manual summary'`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.DB().Exec(tc.trigger); err != nil {
				t.Fatal(err)
			}
			// Scanner tolerates these failures; final committed rows distinguish their effects.
			if err := s.ScanLibrary(t.Context(), lib.ID, path, true, nil); err != nil {
				t.Fatal(err)
			}
			waitForScannerCoverQueue(t, s)
			series, err := store.ListSeriesByLibraryLite(context.Background(), lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			books, err := store.ListBooksByLibrary(context.Background(), lib.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(series) != tc.wantSeries || len(books) != tc.wantBooks {
				t.Fatalf("series=%d books=%d, want %d %d", len(series), len(books), tc.wantSeries, tc.wantBooks)
			}
			if tc.existing && series[0].Summary.String != "Manual summary" {
				t.Fatalf("metadata changed despite rejected update: %+v", series[0])
			}
		})
	}
}
