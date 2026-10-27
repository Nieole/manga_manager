package reading_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestRecentBooksBindsReaderAndPreservesAnonymousCompatibility(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	other, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other", PasswordHash: "x", Role: "regular"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 27, 6, 0, 0, 0, time.UTC)
	writes := reading.New(store, nil)
	if _, err := writes.RecordPosition(ctx, reading.Position{BookID: bid, Page: 7, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	if _, err := writes.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	owner, _ := reading.UserSubject(uid)
	another, _ := reading.UserSubject(other.ID)
	for _, test := range []struct {
		subject reading.Subject
		count   int
		page    int64
	}{{owner, 1, 3}, {another, 0, 0}, {reading.AnonymousSubject(), 1, 7}} {
		view, err := reading.RecentBooks(ctx, store, test.subject, 1)
		if err != nil || view.Subject != test.subject || len(view.Items) != test.count {
			t.Fatalf("view=%+v err=%v", view, err)
		}
		if len(view.Items) > 0 && (view.Items[0].BookID != bid || view.Items[0].LastReadPage.Int64 != test.page) {
			t.Fatalf("position=%+v", view)
		}
	}
	if _, err := reading.RecentBooks(ctx, store, reading.Subject{}, 1); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("invalid subject=%v", err)
	}
}
