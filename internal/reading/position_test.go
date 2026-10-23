package reading_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func positionFixture(t *testing.T) (database.Store, int64, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "reading.db")
	if err := database.Migrate(path); err != nil {
		t.Fatal(err)
	}
	store, err := database.NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{Name: "library", Path: "/library", ScanMode: "none"})
	if err != nil {
		t.Fatal(err)
	}
	series, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: "series", Path: "/library/series", LibraryID: lib.ID})
	if err != nil {
		t.Fatal(err)
	}
	book, err := store.CreateBook(ctx, database.CreateBookParams{Name: "book", Path: "/library/series/book", LibraryID: lib.ID, SeriesID: series.ID, PageCount: 20, FileModifiedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, database.CreateUserParams{Username: "reader", PasswordHash: "x", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	return store, user.ID, book.ID
}

func TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	for _, report := range []struct {
		page    int64
		at      time.Time
		applied bool
	}{{15, at, true}, {5, at.Add(time.Hour), true}, {18, at.Add(time.Minute), false}} {
		result, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: report.page, OccurredAt: &report.at})
		if err != nil || result.Applied != report.applied {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	progress, found, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || progress.LastReadPage.Int64 != 5 {
		t.Fatalf("progress=%+v found=%v err=%v", progress, found, err)
	}
}

func TestSamePageNewFactRefreshesButIdentifiedRetryDoesNot(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	ctx := context.Background()
	first := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	later := first.Add(time.Hour)
	for _, input := range []struct {
		at      time.Time
		id      string
		applied bool
	}{{first, "first", true}, {later, "second", true}, {later.Add(time.Hour), "second", false}, {later, "", false}} {
		got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 5, OccurredAt: &input.at, FactID: input.id})
		if err != nil || got.Applied != input.applied {
			p, _, _ := store.GetUserBookProgress(ctx, uid, bid)
			t.Fatalf("input=%+v result=%+v err=%v prior=%v now=%v", input, got, err, p.LastReadAt, time.Now())
		}
	}
	p, _, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !p.LastReadAt.Time.Equal(later) {
		t.Fatalf("last read=%v err=%v", p.LastReadAt, err)
	}
}

func TestPositionFailureRollsBackStateAndDoesNotInvalidate(t *testing.T) {
	store, uid, bid := positionFixture(t)
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_aggregate BEFORE INSERT ON user_series_progress BEGIN SELECT RAISE(ABORT,'aggregate unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	service := reading.New(store, func() { invalidated = true })
	got, err := service.RecordPosition(context.Background(), reading.Position{UserID: uid, BookID: bid, Page: 5})
	if err == nil || got.Applied || invalidated {
		t.Fatalf("result=%+v error=%v invalidated=%v", got, err, invalidated)
	}
	if _, found, err := store.GetUserBookProgress(context.Background(), uid, bid); err != nil || found {
		t.Fatalf("rollback found=%v err=%v", found, err)
	}
}

func TestActivityFailureKeepsCommittedPositionAndAttemptsUserProjection(t *testing.T) {
	store, uid, bid := positionFixture(t)
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_activity BEFORE INSERT ON reading_activity BEGIN SELECT RAISE(ABORT,'activity unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	service := reading.New(store, func() { invalidated = true })
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	got, err := service.RecordPosition(context.Background(), reading.Position{UserID: uid, BookID: bid, Page: 5, OccurredAt: &at})
	if err != nil || !got.Applied || len(got.Warnings) != 1 || !invalidated {
		t.Fatalf("result=%+v error=%v invalidated=%v", got, err, invalidated)
	}
	progress, found, err := store.GetUserBookProgress(context.Background(), uid, bid)
	if err != nil || !found || progress.LastReadPage.Int64 != 5 {
		t.Fatalf("progress=%+v found=%v err=%v", progress, found, err)
	}
	stats, err := store.GetUserPeriodStats(context.Background(), uid, 2026, 10)
	if err != nil || stats.Pages != 5 || stats.BooksTouched != 1 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestPositionsAreIsolatedClampedAndSupportUnknownPageCounts(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	service := reading.New(store, nil)
	if _, err := service.RecordPosition(ctx, reading.Position{BookID: bid, Page: 10}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.GetUserBookProgress(ctx, uid, bid); err != nil || found {
		t.Fatalf("anonymous progress leaked: found=%v err=%v", found, err)
	}
	for _, input := range []struct{ page, want int64 }{{99, 20}, {-3, 1}} {
		got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: input.page})
		if err != nil || !got.Applied || got.Page != input.want {
			p, _, _ := store.GetUserBookProgress(ctx, uid, bid)
			t.Fatalf("input=%+v result=%+v err=%v prior=%v now=%v", input, got, err, p.LastReadAt, time.Now())
		}
	}
	if _, err := store.DB().Exec(`UPDATE books SET page_count=0 WHERE id=?`, bid); err != nil {
		t.Fatal(err)
	}
	got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 40})
	if err != nil || got.Page != 40 {
		t.Fatalf("unknown pages result=%+v err=%v", got, err)
	}
}

func TestConcurrentPositionsKeepNewestOccurrence(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	errs := make(chan error, 20)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	for i := 1; i <= 20; i++ {
		go func(i int) {
			occurred := at.Add(time.Duration(i) * time.Minute)
			_, err := service.RecordPosition(context.Background(), reading.Position{UserID: uid, BookID: bid, Page: int64(i), OccurredAt: &occurred})
			errs <- err
		}(i)
	}
	for i := 0; i < 20; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := store.GetUserBookProgress(context.Background(), uid, bid)
	if err != nil || got.LastReadPage.Int64 != 20 || !got.LastReadAt.Time.Equal(at.Add(20*time.Minute)) {
		t.Fatalf("progress=%+v err=%v", got, err)
	}
}

func TestIdentifiedRetryRemainsDuplicateAfterAnotherFact(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	ctx := context.Background()
	for _, id := range []string{"first", "second"} {
		got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 5, FactID: id})
		if err != nil || !got.Applied {
			t.Fatalf("result=%+v err=%v", got, err)
		}
	}
	got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 10, FactID: "first"})
	if err != nil || got.Applied {
		t.Fatalf("retry=%+v err=%v", got, err)
	}
}

func TestLegacyEmptyProgressAcceptsReading(t *testing.T) {
	store, uid, bid := positionFixture(t)
	if _, err := store.DB().Exec(`INSERT INTO user_book_progress(user_id,book_id,last_read_page,last_read_at) VALUES(?,?,NULL,NULL)`, uid, bid); err != nil {
		t.Fatal(err)
	}
	got, err := reading.New(store, nil).RecordPosition(context.Background(), reading.Position{UserID: uid, BookID: bid, Page: 3})
	if err != nil || !got.Applied {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestMigrationRetainsLegacyIdentityAndRejectsOlderReading(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if err := store.SetUserBookProgress(ctx, uid, bid, 12, at); err != nil {
		t.Fatal(err)
	}
	var seq int
	var name, path string
	if err := store.DB().QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"reading_position_basis", "reading_position_receipts"} {
		if _, err := store.DB().Exec("DROP TABLE " + table); err != nil {
			t.Fatal(err)
		}
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
	stale := at.Add(-time.Hour)
	got, err := reading.New(reopened, nil).RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &stale})
	if err != nil || got.Applied || got.Page != 12 {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	book, err := reopened.GetBook(ctx, bid)
	if err != nil || book.ID != bid {
		t.Fatalf("book=%+v err=%v", book, err)
	}
	newer := at.Add(time.Hour)
	got, err = reading.New(reopened, nil).RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 4, OccurredAt: &newer, FactID: "after-upgrade"})
	if err != nil || !got.Applied {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestOffsetOccurrencesCompareAsInstantsAcrossLegacyAndNewPositions(t *testing.T) {
	original := time.Local
	time.Local = time.FixedZone("CST", 8*60*60)
	defer func() { time.Local = original }()
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	first := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if err := store.SetUserBookProgress(ctx, uid, bid, 15, first); err != nil {
		t.Fatal(err)
	}
	service := reading.New(store, nil)
	samePage := first.Add(time.Minute)
	backward := first.Add(time.Hour)
	for _, input := range []struct {
		at   time.Time
		page int64
	}{{samePage, 15}, {backward, 5}} {
		got, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: input.page, OccurredAt: &input.at})
		if err != nil || !got.Applied {
			t.Fatalf("input=%+v result=%+v err=%v", input, got, err)
		}
	}
	progress, _, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || progress.LastReadPage.Int64 != 5 || !progress.LastReadAt.Time.Equal(backward) {
		t.Fatalf("progress=%+v err=%v", progress, err)
	}
}
