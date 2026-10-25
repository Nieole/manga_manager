package reading_test

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/reading"
)

func TestReadingTimeAccumulatesOnlyForItsReaderAndReceiptDay(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.Local)
	service := reading.New(store, nil, reading.WithClock(func() time.Time { return now }))
	for _, seconds := range []int64{30, 45} {
		if err := service.RecordTime(ctx, reading.TimeReport{UserID: uid, BookID: bid, Seconds: seconds}); err != nil {
			t.Fatal(err)
		}
	}
	total, err := store.GetUserTotalReadingTime(ctx, uid)
	if err != nil || total != 75 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	other, err := store.GetUserTotalReadingTime(ctx, uid+1)
	if err != nil || other != 0 {
		t.Fatalf("other=%d err=%v", other, err)
	}
	stats, err := store.GetUserPeriodStats(ctx, uid, now.Year(), int(now.Month()))
	if err != nil || stats.ReadSeconds != 75 || stats.Pages != 0 {
		t.Fatalf("stats=%+v err=%v", stats, err)
	}
}

func TestReadingTimeCompatibilityAndCap(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	ctx := context.Background()
	for _, report := range []reading.TimeReport{
		{BookID: bid, Seconds: 30}, {UserID: uid, BookID: bid, Seconds: 0},
		{UserID: uid, BookID: bid, Seconds: -5}, {UserID: uid, BookID: 999999, Seconds: 30},
		{UserID: uid, BookID: bid, Seconds: 99999},
	} {
		if err := service.RecordTime(ctx, report); err != nil {
			t.Fatal(err)
		}
	}
	total, err := store.GetUserTotalReadingTime(ctx, uid)
	if err != nil || total != 3600 {
		t.Fatalf("total=%d err=%v", total, err)
	}
}

func TestReadingTimeActivityFailureRollsBackTimeButKeepsSavedPosition(t *testing.T) {
	store, uid, bid := positionFixture(t)
	service := reading.New(store, nil)
	ctx := context.Background()
	if _, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 7}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().Exec(`CREATE TRIGGER fail_time_activity BEFORE INSERT ON user_reading_activity BEGIN SELECT RAISE(ABORT,'activity unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	if err := service.RecordTime(ctx, reading.TimeReport{UserID: uid, BookID: bid, Seconds: 30}); err == nil {
		t.Fatal("expected observable failure")
	}
	total, err := store.GetUserTotalReadingTime(ctx, uid)
	if err != nil || total != 0 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	position, found, err := store.GetUserBookProgress(ctx, uid, bid)
	if err != nil || !found || position.LastReadPage.Int64 != 7 {
		t.Fatalf("position=%+v found=%v err=%v", position, found, err)
	}
}
