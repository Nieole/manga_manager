package reading_test

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

// Atomic write consumers have no general transaction or database handle capability.
type atomicReadingWrites interface {
	ResolveReadingOperationTime(context.Context, int64, string, time.Time) (time.Time, error)
	MutateReadingPositions(context.Context, int64, []int64, string, func(int64, database.ReadingPositionState) database.ReadingPositionMutation) (map[int64]database.ReadingPositionMutation, error)
	MutateReadingPosition(context.Context, int64, int64, string, func(database.ReadingPositionState) database.ReadingPositionMutation) (database.ReadingPositionMutation, error)
	ListBooksBySeries(context.Context, int64) ([]database.Book, error)
	LogReadingActivity(context.Context, database.LogReadingActivityParams) error
	LogUserReadingActivityAt(context.Context, int64, int64, int64, time.Time) error
	AccumulateReadingTimeForBook(context.Context, int64, int64, int64, string) error
}

func TestReadingWritesUseOnlyAtomicStorageCapabilities(t *testing.T) {
	store, uid, bid := positionFixture(t)
	var port atomicReadingWrites = store
	ctx := context.Background()
	at := time.Date(2026, 10, 27, 6, 0, 0, 0, time.UTC)
	invalidated := 0
	service := reading.New(port, func() { invalidated++ }, reading.WithClock(func() time.Time { return at }))
	first, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 12, OccurredAt: &at, FactID: "first"})
	if err != nil || !first.Applied {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	later := at.Add(time.Hour)
	backward, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &later, FactID: "backward"})
	if err != nil || !backward.Applied || backward.Page != 3 {
		t.Fatalf("backward=%+v err=%v", backward, err)
	}
	later = later.Add(time.Hour)
	marked, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: false, OccurredAt: &later, OperationID: "clear"})
	if err != nil || len(marked.CommittedBookIDs) != 1 || invalidated != 3 {
		t.Fatalf("clear=%+v invalidated=%d err=%v", marked, invalidated, err)
	}
	replay, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 12, OccurredAt: &at, FactID: "first"})
	if err != nil || replay.Applied || invalidated != 3 {
		t.Fatalf("replay=%+v invalidated=%d err=%v", replay, invalidated, err)
	}
	if err := service.RecordTime(ctx, reading.TimeReport{UserID: uid, BookID: bid, Seconds: 30}); err != nil {
		t.Fatal(err)
	}
	seconds, err := store.GetUserTotalReadingTime(ctx, uid)
	if err != nil || seconds != 30 {
		t.Fatalf("seconds=%d err=%v", seconds, err)
	}
	subject, _ := reading.UserSubject(uid)
	view, err := reading.NewQueries(store).BookView(ctx, subject, bid)
	if err != nil || view.State.LastReadPage.Valid || view.State.IsRead {
		t.Fatalf("cleared view=%+v err=%v", view, err)
	}
}
