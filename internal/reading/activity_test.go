package reading_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestBulkReadActivityUsesOriginalDayAndRetryDoesNotAddAnotherDay(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	service := reading.New(store, nil)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	input := reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at, OperationID: "read"}
	if _, err := service.MarkBooks(ctx, input); err != nil {
		t.Fatal(err)
	}
	stats, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || stats.Pages != 20 || stats.ActiveDays != 1 || stats.BooksTouched != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	tomorrow := at.Add(24 * time.Hour)
	input.OccurredAt = &tomorrow
	if _, err := service.MarkBooks(ctx, input); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || after.Pages != 20 || after.ActiveDays != 1 {
		t.Fatalf("retry stats=%+v err=%v", after, err)
	}
}

func TestUnknownReadStateHasActivityWithoutInventedPages(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	if _, err := store.DB().Exec(`UPDATE books SET page_count=0 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err := reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || !p.IsRead || p.LastReadPage.Int64 != 0 {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
	stats, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || stats.Pages != 0 || stats.ActiveDays != 1 || stats.BooksTouched != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestBulkReadActivityFailuresRemainWarningsAndProjectionsAreIndependent(t *testing.T) {
	for _, table := range []string{"reading_activity", "user_reading_activity"} {
		t.Run(table, func(t *testing.T) {
			store, uid, bid := positionFixture(t)
			ctx := context.Background()
			if _, err := store.DB().Exec(`CREATE TRIGGER fail_bulk_activity BEFORE INSERT ON ` + table + ` BEGIN SELECT RAISE(ABORT,'activity unavailable'); END`); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
			result, err := reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at})
			if err != nil || len(result.CommittedBookIDs) != 1 || len(result.Warnings) != 1 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			p, found, err := store.GetUserBookProgress(ctx, uid, bid)
			if err != nil || !found || p.LastReadPage.Int64 != 20 {
				t.Fatalf("p=%+v found=%v err=%v", p, found, err)
			}
			if table == "reading_activity" {
				stats, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
				if err != nil || stats.Pages != 20 || stats.BooksTouched != 1 {
					t.Fatalf("user stats=%+v err=%v", stats, err)
				}
			} else {
				days, err := store.GetActivityHeatmap(ctx, "2026-10-01")
				if err != nil || len(days) != 1 || days[0].PageCount.Float64 != 20 {
					t.Fatalf("global days=%+v err=%v", days, err)
				}
			}
		})
	}
}

func TestPartialBulkActivityOnlyUsesCommittedBooksAndKeepsOriginalDayOnRetry(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	ids := markFixtureBooks(t, store, bid, 501)
	if _, err := store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER fail_last_activity_batch BEFORE INSERT ON reading_position_basis WHEN NEW.book_id=%d BEGIN SELECT RAISE(ABORT,'later batch failed'); END`, ids[500])); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	input := reading.MarkBooksInput{UserID: uid, BookIDs: ids, IsRead: true, OccurredAt: &at, OperationID: "partial-activity"}
	service := reading.New(store, nil)
	result, err := service.MarkBooks(ctx, input)
	if err == nil || len(result.CommittedBookIDs) != 500 {
		t.Fatalf("commits=%d err=%v", len(result.CommittedBookIDs), err)
	}
	stats, err := store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || stats.Pages != 10000 || stats.BooksTouched != 500 || stats.ActiveDays != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER fail_last_activity_batch`); err != nil {
		t.Fatal(err)
	}
	result, err = service.MarkBooks(ctx, input)
	if err != nil || len(result.CommittedBookIDs) != 1 {
		t.Fatalf("retry=%+v err=%v", result, err)
	}
	stats, err = store.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || stats.Pages != 10020 || stats.BooksTouched != 501 || stats.ActiveDays != 1 {
		t.Fatalf("retry stats=%+v err=%v", stats, err)
	}
}

func TestLegacyCompletionMigrationPreservesHistoryAndDoesNotInventCurrentPages(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	inserted, err := store.DB().Exec(`INSERT INTO books(series_id,library_id,name,path,size,file_modified_at,page_count,last_read_page) SELECT series_id,library_id,'large','/large',0,CURRENT_TIMESTAMP,100000,99999 FROM books WHERE id=?`, bid)
	if err != nil {
		t.Fatal(err)
	}
	legitimateID, err := inserted.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		fmt.Sprintf(`UPDATE books SET page_count=0,last_read_page=99999,last_read_at='2026-10-20 10:00:00 +0000 UTC' WHERE id=%d`, bid),
		fmt.Sprintf(`INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at) VALUES(%d,%d,99999,'2026-10-20 10:00:00 +0000 UTC')`, uid, bid),
		fmt.Sprintf(`INSERT INTO reading_activity(book_id,date,pages_read) VALUES(%d,'2026-10-20',99999)`, bid),
		fmt.Sprintf(`INSERT INTO user_reading_activity(user_id,book_id,date,pages_read) VALUES(%d,%d,'2026-10-20',99999)`, uid, bid),
		`ALTER TABLE books DROP COLUMN is_read`,
		`ALTER TABLE user_book_progress DROP COLUMN is_read`,
		`PRAGMA user_version=3`,
	}
	for _, statement := range statements {
		if _, err := store.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
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
	legitimate, err := reopened.GetBook(ctx, legitimateID)
	if err != nil || legitimate.IsRead || !legitimate.LastReadPage.Valid || legitimate.LastReadPage.Int64 != 99999 {
		t.Fatalf("legitimate large page changed: %+v err=%v", legitimate, err)
	}
	p, found, err := reopened.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || !p.IsRead || p.LastReadPage.Valid {
		t.Fatalf("p=%+v found=%v err=%v", p, found, err)
	}
	book, err := reopened.GetBook(ctx, bid)
	if err != nil || !book.IsRead || book.LastReadPage.Valid {
		t.Fatalf("book=%+v err=%v", book, err)
	}
	if state := reading.InspectBook(book); !state.Completed || state.ReadPages != 0 {
		t.Fatalf("state=%+v", state)
	}
	stats, err := reopened.GetUserPeriodStats(ctx, uid, 2026, 10)
	if err != nil || stats.Pages != 99999 {
		t.Fatalf("historical activity must be unchanged: stats=%+v err=%v", stats, err)
	}
	global, err := reopened.GetActivityHeatmap(ctx, "2026-10-01")
	if err != nil || len(global) != 1 || global[0].PageCount.Float64 != 99999 {
		t.Fatalf("global history=%+v err=%v", global, err)
	}
	if _, err := reopened.DB().Exec(`UPDATE books SET page_count=30 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	if err := reopened.RefreshSeriesDerivedData(ctx, book.SeriesID); err != nil {
		t.Fatal(err)
	}
	book, err = reopened.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	if state := reading.InspectBook(book); !state.Completed || state.ReadPages != 0 {
		t.Fatalf("discovered pages changed position: %+v", state)
	}
	if count, err := reopened.GetUserReadBooksCount(ctx, uid); err != nil || count != 1 {
		t.Fatalf("read count=%d err=%v", count, err)
	}
}

func TestIdentifiedBulkRetryWithoutTimestampRetainsOriginalDay(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	ids := markFixtureBooks(t, store, bid, 501)
	if _, err := store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER fail_no_time_batch BEFORE INSERT ON reading_position_basis WHEN NEW.book_id=%d BEGIN SELECT RAISE(ABORT,'later batch failed'); END`, ids[500])); err != nil {
		t.Fatal(err)
	}
	original := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	input := reading.MarkBooksInput{UserID: uid, BookIDs: ids, IsRead: true, OperationID: "no-time"}
	result, err := reading.New(store, nil, reading.WithClock(func() time.Time { return original })).MarkBooks(ctx, input)
	if err == nil || len(result.CommittedBookIDs) != 500 {
		t.Fatalf("commits=%d err=%v", len(result.CommittedBookIDs), err)
	}
	if _, err := store.DB().Exec(`DROP TRIGGER fail_no_time_batch`); err != nil {
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
	result, err = reading.New(reopened, nil, reading.WithClock(func() time.Time { return original.Add(24 * time.Hour) })).MarkBooks(ctx, input)
	if err != nil || len(result.CommittedBookIDs) != 1 {
		t.Fatalf("retry=%+v err=%v", result, err)
	}
	stats, err := reopened.GetUserPeriodStats(ctx, uid, 2026, 10)
	days, dayErr := reopened.GetUserActivityHeatmap(ctx, uid, "-1000 days")
	if err != nil || stats.Pages != 10020 || stats.ActiveDays != 1 || dayErr != nil || len(days) != 1 || days[0].Date != "2026-10-20" {
		t.Fatalf("stats=%+v days=%+v err=%v dayErr=%v", stats, days, err, dayErr)
	}
}

func TestKnownPageMarksFollowRescannedPageCounts(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err := reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	book, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	p, found, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found {
		t.Fatalf("progress=%+v found=%v err=%v", p, found, err)
	}
	book.LastReadPage = p.LastReadPage
	book.IsRead = p.IsRead
	if state := reading.InspectBook(book); !state.Completed || state.ReadPages != 20 {
		t.Fatalf("known completion=%+v", state)
	}
	if _, err := store.DB().Exec(`UPDATE books SET page_count=30 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	if err := store.RefreshSeriesDerivedData(ctx, book.SeriesID); err != nil {
		t.Fatal(err)
	}
	book.PageCount = 30
	if state := reading.InspectBook(book); state.Completed || state.ReadPages != 20 {
		t.Fatalf("rescanned completion=%+v", state)
	}
}
