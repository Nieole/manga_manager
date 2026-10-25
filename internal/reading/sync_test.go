package reading_test

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestSyncPositionsKeepsOrderAndIndependentResults(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	at := time.Date(2026, 10, 20, 23, 0, 0, 0, time.UTC)
	later := at.Add(time.Hour)
	results := service.SyncPositions(context.Background(), []reading.Position{
		{UserID: uid, BookID: bid, Page: 10, OccurredAt: &at, FactID: "first"},
		{UserID: uid, BookID: 0, Page: 3},
		{UserID: uid, BookID: 999999, Page: 3},
		{UserID: uid, BookID: bid, Page: 10, OccurredAt: &later, FactID: "same-page"},
		{UserID: uid, BookID: bid, Page: 15, OccurredAt: &at},
		{UserID: uid, BookID: bid, Page: 10, OccurredAt: &later, FactID: "same-page"},
	})
	want := []string{"updated", "invalid", "not_found", "updated", "skipped_stale", "skipped_unchanged"}
	if len(results) != len(want) {
		t.Fatalf("results=%+v", results)
	}
	for i, result := range results {
		if result.Status != want[i] {
			t.Fatalf("item %d=%+v want %s", i, result, want[i])
		}
	}
	progress, _, err := store.GetUserBookProgress(context.Background(), uid, bid)
	if err != nil || !progress.LastReadAt.Time.Equal(later) || progress.LastReadPage.Int64 != 10 {
		t.Fatalf("progress=%+v err=%v", progress, err)
	}
}

func TestSyncClampedRetryKeepsUnchangedProtocolResult(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	results := service.SyncPositions(context.Background(), []reading.Position{
		{UserID: uid, BookID: bid, Page: 999, OccurredAt: &at},
		{UserID: uid, BookID: bid, Page: 999, OccurredAt: &at},
	})
	if results[0].Status != "updated" || results[1].Status != "skipped_unchanged" || results[1].Page != 20 {
		t.Fatalf("results=%+v", results)
	}
}

func TestSyncPositionWriteFailureKeepsEarlierAndLaterResults(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	book, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	next, err := store.CreateBook(ctx, database.CreateBookParams{Name: "other", Path: "/library/series/other", LibraryID: book.LibraryID, SeriesID: book.SeriesID, PageCount: 20, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_one_position BEFORE INSERT ON reading_position_receipts WHEN NEW.fact_id='fail' BEGIN SELECT RAISE(ABORT,'one failure'); END`); err != nil {
		t.Fatal(err)
	}
	invalidations := 0
	service := reading.New(store, func() { invalidations++ })
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	later := at.Add(time.Hour)
	results := service.SyncPositions(ctx, []reading.Position{
		{UserID: uid, BookID: bid, Page: 7, OccurredAt: &at, FactID: "first"},
		{UserID: uid, BookID: bid, Page: 15, OccurredAt: &later, FactID: "fail"},
		{UserID: uid, BookID: next.ID, Page: 3, OccurredAt: &at, FactID: "last"},
	})
	if len(results) != 3 || results[0].Status != "updated" || results[1].Status != "invalid" || results[2].Status != "updated" || invalidations != 2 {
		t.Fatalf("results=%+v invalidations=%d", results, invalidations)
	}
	p2, found, err := store.GetUserBookProgress(ctx, uid, next.ID)
	if err != nil || !found || p2.LastReadPage.Int64 != 3 {
		t.Fatalf("later progress=%+v err=%v", p2, err)
	}
	p, _, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || p.LastReadPage.Int64 != 7 {
		t.Fatalf("progress=%+v err=%v", p, err)
	}
}
