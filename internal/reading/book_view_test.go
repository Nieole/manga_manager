package reading_test

import (
	"context"
	"errors"
	"manga-manager/internal/database"
	"testing"
	"time"

	"manga-manager/internal/reading"
)

func TestBookViewRequiresExplicitValidSubject(t *testing.T) {
	store, uid, bid := positionFixture(t)
	queries := reading.NewQueries(store)
	if _, err := queries.BookView(context.Background(), reading.Subject{}, bid); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("zero subject must be rejected: %v", err)
	}
	for _, id := range []int64{-1, 0} {
		if _, err := reading.UserSubject(id); !errors.Is(err, reading.ErrInvalidSubject) {
			t.Fatalf("user %d must not become anonymous: %v", id, err)
		}
	}
	subject, err := reading.UserSubject(uid)
	if err != nil {
		t.Fatal(err)
	}
	view, err := queries.BookView(context.Background(), subject, bid)
	if err != nil {
		t.Fatal(err)
	}
	if view.Subject != subject || view.Facts.ID != bid || view.Facts.PageCount != 20 || view.State.IsRead || view.State.LastReadPage.Valid {
		t.Fatalf("view=%+v", view)
	}
}

func TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	first, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateBook(ctx, database.CreateBookParams{Name: "second", Path: first.Path + "2", LibraryID: first.LibraryID, SeriesID: first.SeriesID, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other", PasswordHash: "x", Role: "regular"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	service := reading.New(store, nil)
	if _, err := service.MarkSeries(ctx, reading.MarkSeriesInput{SeriesIDs: []int64{first.SeriesID}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	subject, _ := reading.UserSubject(uid)
	otherSubject, _ := reading.UserSubject(other.ID)
	queries := reading.NewQueries(store)
	for _, sub := range []reading.Subject{subject, otherSubject, reading.AnonymousSubject()} {
		view, err := queries.SeriesBooks(ctx, sub, first.SeriesID)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Books) != 2 || view.Books[0].Facts.ID != bid || view.Books[1].Facts.ID != second.ID {
			t.Fatalf("order=%+v", view)
		}
		unknown, err := queries.AdjacentView(ctx, sub, bid, true)
		if err != nil {
			t.Fatal(err)
		}
		prev, err := queries.AdjacentView(ctx, sub, second.ID, false)
		if err != nil {
			t.Fatal(err)
		}
		if unknown.Subject != sub || unknown.Facts.ID != second.ID || unknown.Facts.PageCount != 0 || unknown.State.LastReadPage.Valid || prev.Facts.ID != bid {
			t.Fatalf("next=%+v prev=%+v", unknown, prev)
		}
		wantComplete := sub == reading.AnonymousSubject()
		if unknown.State.IsRead != wantComplete || (view.Continue.NextUnreadBookID == 0) != wantComplete || (view.Continue.ReadBooks == 2) != wantComplete {
			t.Fatalf("subject=%+v view=%+v", sub, view)
		}
	}
	for i, page := range []int64{12, 3} {
		when := at.Add(time.Duration(i+1) * time.Hour)
		if _, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: page, OccurredAt: &when}); err != nil {
			t.Fatal(err)
		}
	}
	view, err := queries.BookView(ctx, subject, bid)
	if err != nil || view.State.LastReadPage.Int64 != 3 || view.State.IsRead {
		t.Fatalf("backward=%+v err=%v", view, err)
	}
	series, err := queries.SeriesBooks(ctx, subject, first.SeriesID)
	if err != nil || series.Continue.ReadPages != 3 || series.Continue.LastReadPage != 3 {
		t.Fatalf("series=%+v err=%v", series, err)
	}
	otherView, err := queries.BookView(ctx, otherSubject, bid)
	if err != nil || otherView.Facts != view.Facts || otherView.State.LastReadPage.Valid {
		t.Fatalf("other=%+v err=%v", otherView, err)
	}
}
