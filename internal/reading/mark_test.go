package reading_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestMarkUnreadRejectsOldReplayAndAllowsNewReading(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	service := reading.New(store, nil)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 12, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	clearAt := at.Add(time.Hour)
	result, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: false, OccurredAt: &clearAt, OperationID: "clear"})
	if err != nil || len(result.CommittedBookIDs) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, found, err := store.GetUserBookProgress(ctx, uid, bid); err != nil || found {
		t.Fatalf("cleared found=%v err=%v", found, err)
	}
	replay, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 12, OccurredAt: &at})
	if err != nil || replay.Applied {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	newer := clearAt.Add(time.Hour)
	next, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &newer})
	if err != nil || !next.Applied {
		t.Fatalf("next=%+v err=%v", next, err)
	}
}

func TestMarkSeriesIsolatedAndUnreadPreservesActivity(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	service := reading.New(store, nil)
	book, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 7, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil {
		t.Fatal(err)
	}
	readAt := at.Add(time.Hour)
	got, err := service.MarkSeries(ctx, reading.MarkSeriesInput{UserID: uid, SeriesIDs: []int64{book.SeriesID, book.SeriesID}, IsRead: true, OccurredAt: &readAt, OperationID: "read-series"})
	if err != nil || len(got.CommittedBookIDs) != 1 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || p.LastReadPage.Int64 != 20 {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
	global, err := store.GetBook(ctx, bid)
	if err != nil || global.LastReadPage.Valid {
		t.Fatalf("global changed=%+v err=%v", global.LastReadPage, err)
	}
	clearAt := readAt.Add(time.Hour)
	if _, err := service.MarkSeries(ctx, reading.MarkSeriesInput{UserID: uid, SeriesIDs: []int64{book.SeriesID}, IsRead: false, OccurredAt: &clearAt, OperationID: "clear-series"}); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || after.Pages != before.Pages || after.ActiveDays != before.ActiveDays {
		t.Fatalf("before=%+v after=%+v err=%v", before, after, err)
	}
	recent, err := store.GetUserRecentReadAll(ctx, uid, 10)
	if err != nil || len(recent) != 0 {
		t.Fatalf("recent=%+v err=%v", recent, err)
	}
}

func markFixtureBooks(t *testing.T, store database.Store, bid int64, count int) []int64 {
	t.Helper()
	ids := []int64{bid}
	tx, err := store.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for i := 1; i < count; i++ {
		res, err := tx.Exec(`INSERT INTO books(series_id,library_id,name,path,size,file_modified_at,page_count) SELECT series_id,library_id,?, ?,0,CURRENT_TIMESTAMP,20 FROM books WHERE id=?`, fmt.Sprintf("book-%d", i), fmt.Sprintf("/books/%d", i), bid)
		if err != nil {
			t.Fatal(err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestMarkBatchFailureRollsBackProgressBasisAndAggregate(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	ids := markFixtureBooks(t, store, bid, 2)
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_mark_aggregate BEFORE INSERT ON user_series_progress BEGIN SELECT RAISE(ABORT,'aggregate unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	service := reading.New(store, func() { invalidated = true })
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	got, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: ids, IsRead: false, OccurredAt: &at, OperationID: "failed-clear"})
	if err == nil || len(got.CommittedBookIDs) != 0 || invalidated {
		t.Fatalf("result=%+v err=%v invalidated=%v", got, err, invalidated)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER fail_mark_aggregate`); err != nil {
		t.Fatal(err)
	}
	earlier := at.Add(-time.Hour)
	next, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 4, OccurredAt: &earlier})
	if err != nil || !next.Applied {
		t.Fatalf("basis must roll back: next=%+v err=%v", next, err)
	}
}

func TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	ids := markFixtureBooks(t, store, bid, 501)
	if _, err := store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER fail_later_mark BEFORE INSERT ON reading_position_basis WHEN NEW.book_id=%d BEGIN SELECT RAISE(ABORT,'later batch unavailable'); END`, ids[500])); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	service := reading.New(store, func() { invalidated = true })
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	input := reading.MarkBooksInput{UserID: uid, BookIDs: ids, IsRead: true, OccurredAt: &at, OperationID: "bulk-read"}
	got, err := service.MarkBooks(ctx, input)
	if err == nil || len(got.CommittedBookIDs) != 500 || !invalidated {
		t.Fatalf("committed=%d err=%v invalidated=%v", len(got.CommittedBookIDs), err, invalidated)
	}
	progress, err := store.GetUserBookProgressMap(ctx, uid, ids)
	if err != nil || len(progress) != 500 {
		t.Fatalf("persisted=%d err=%v", len(progress), err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER fail_later_mark`); err != nil {
		t.Fatal(err)
	}
	retried, err := service.MarkBooks(ctx, input)
	if err != nil || len(retried.CommittedBookIDs) != 1 || retried.CommittedBookIDs[0] != ids[500] {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
}

func TestClearEvidenceSurvivesRestartForUserAndAnonymousReader(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		t.Run(fmt.Sprint(anonymous), func(t *testing.T) {
			store, uid, bid := positionFixture(t)
			if anonymous {
				uid = 0
			}
			ctx := context.Background()
			at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
			if _, err := reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: false, OccurredAt: &at, OperationID: "clear"}); err != nil {
				t.Fatal(err)
			}
			var seq int
			var name, path string
			if err := store.DB().QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			if err := database.Migrate(path); err != nil {
				t.Fatal(err)
			}
			reopened, err := database.NewStore(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			service := reading.New(reopened, nil)
			older := at.Add(-time.Hour)
			got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 15, OccurredAt: &older})
			if err != nil || got.Applied {
				t.Fatalf("old replay=%+v err=%v", got, err)
			}
			newer := at.Add(time.Hour)
			got, err = service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &newer})
			if err != nil || !got.Applied {
				t.Fatalf("new reading=%+v err=%v", got, err)
			}
		})
	}
}

func TestMarkSelectionLimitsAndUnknownPages(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	invalidated := false
	service := reading.New(store, func() { invalidated = true })
	if result, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid}); err != nil || len(result.CommittedBookIDs) != 0 || invalidated {
		t.Fatalf("empty result=%+v err=%v invalidated=%v", result, err, invalidated)
	}
	tooMany := make([]int64, reading.MaxMarkBooks+1)
	if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: tooMany, IsRead: true}); !errors.Is(err, reading.ErrMarkLimit) {
		t.Fatalf("limit err=%v", err)
	}
	if _, err := service.MarkSeries(ctx, reading.MarkSeriesInput{UserID: uid, SeriesIDs: make([]int64, reading.MaxMarkSeries+1), IsRead: true}); !errors.Is(err, reading.ErrMarkLimit) {
		t.Fatalf("series limit err=%v", err)
	}
	if _, err := store.DB().Exec(`UPDATE books SET page_count=0 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	result, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid, bid, -1, 987654321}, IsRead: true})
	if err != nil || len(result.CommittedBookIDs) != 1 {
		t.Fatalf("unknown pages=%+v err=%v", result, err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || p.LastReadPage.Int64 != 99999 {
		t.Fatalf("unknown progress=%+v found=%v err=%v", p, found, err)
	}
	other, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other", PasswordHash: "x", Role: "regular"})
	if err != nil {
		t.Fatal(err)
	}
	clearAt := time.Now().Add(time.Hour)
	if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: other.ID, BookIDs: []int64{bid}, IsRead: false, OccurredAt: &clearAt}); err != nil {
		t.Fatal(err)
	}
	p, found, err = store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || p.LastReadPage.Int64 != 99999 {
		t.Fatalf("other user clear leaked: %+v %v %v", p, found, err)
	}
}
