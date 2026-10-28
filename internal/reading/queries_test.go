package reading_test

import (
	"context"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestReaderViewsIsolateSubjectAndReflectBackwardPositionAndClear(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	book, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err = reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	view := reading.NewQueries(store)
	isolated, err := view.BookView(ctx, commandSubject(uid), bid)
	if err != nil {
		t.Fatal(err)
	}
	if isolated.State.IsRead || isolated.State.LastReadPage.Valid || isolated.State.LastReadAt.Valid {
		t.Fatalf("reader inherited global state: %+v", isolated)
	}
	legacy, err := view.BookView(ctx, commandSubject(0), bid)
	if err != nil || !legacy.Inspect().Completed {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
	if _, err = reading.New(store, nil).RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 12, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	if _, err = reading.New(store, nil).RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	series, err := view.SeriesBooks(ctx, commandSubject(uid), book.SeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if series.Continue.ReadPages != 3 || series.Continue.LastReadPage != 3 || series.Continue.NextUnreadBookID != bid {
		t.Fatalf("backward series=%+v", series)
	}
	isolated, err = view.BookView(ctx, commandSubject(uid), bid)
	if err != nil || isolated.State.LastReadPage.Int64 != 3 || !isolated.State.LastReadAt.Time.Equal(later) {
		t.Fatalf("detail=%+v err=%v", isolated, err)
	}
	if _, err = reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: false, OccurredAt: timePointer(later.Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	series, err = view.SeriesBooks(ctx, commandSubject(uid), book.SeriesID)
	if err != nil || series.Continue.LastReadBookID != 0 || series.Continue.ReadPages != 0 {
		t.Fatalf("cleared=%+v err=%v", series, err)
	}
}
func timePointer(at time.Time) *time.Time { return &at }

func TestSeriesQueriesKeepNaturalOrderUnknownCompletionAndReadOnlyTimes(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	first, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := store.CreateBook(ctx, database.CreateBookParams{Name: "第2卷.cbz", Path: first.Path + "2", LibraryID: first.LibraryID, SeriesID: first.SeriesID, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.CreateBook(ctx, database.CreateBookParams{Name: "第10卷.cbz", Path: first.Path + "10", LibraryID: first.LibraryID, SeriesID: first.SeriesID, PageCount: 10, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	service := reading.New(store, nil)
	if _, err = service.MarkSeries(ctx, reading.MarkSeriesInput{UserID: uid, SeriesIDs: []int64{first.SeriesID}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	queries := reading.NewQueries(store)
	before, err := reading.RecentBooks(ctx, store, commandSubject(uid), 10)
	if err != nil {
		t.Fatal(err)
	}
	view, err := queries.SeriesBooks(ctx, commandSubject(uid), first.SeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Continue.ReadBooks != 3 || view.Continue.TotalPages != 30 || view.Continue.ReadPages != 30 || view.Continue.NextUnreadBookID != 0 {
		t.Fatalf("all read summary=%+v", view.Continue)
	}
	detail, err := queries.BookView(ctx, commandSubject(uid), unknown.ID)
	if err != nil || !detail.State.IsRead || detail.State.LastReadPage.Valid || !detail.Inspect().Completed {
		t.Fatalf("unknown=%+v err=%v", detail, err)
	}
	next, err := queries.AdjacentView(ctx, commandSubject(uid), unknown.ID, true)
	if err != nil || next.Facts.ID != third.ID {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	prev, err := queries.AdjacentView(ctx, commandSubject(uid), third.ID, false)
	if err != nil || prev.Facts.ID != unknown.ID {
		t.Fatalf("previous=%+v err=%v", prev, err)
	}
	after, err := reading.RecentBooks(ctx, store, commandSubject(uid), 10)
	if err != nil || len(before.Items) != 1 || len(after.Items) != 1 || !after.Items[0].LastReadAt.Time.Equal(at) || before.Items[0].LastReadAt != after.Items[0].LastReadAt {
		t.Fatalf("read modified time: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestCatalogueQueriesFilterAndPageUsingRequestedSubject(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	book, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other", PasswordHash: "x", Role: "regular"})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err = reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	if err = store.RefreshSeriesDerivedData(ctx, book.SeriesID); err != nil {
		t.Fatal(err)
	}
	queries := reading.NewCatalogueQueries(store)
	filters := reading.CatalogueFilters{ReadState: "completed"}
	for _, subject := range []int64{uid, other.ID} {
		reader, err := reading.UserSubject(subject)
		if err != nil {
			t.Fatal(err)
		}
		page, err := queries.Search(ctx, reader, reading.CatalogueSearch{LibraryID: book.LibraryID, Filters: filters, Limit: 1, SortBy: "name_asc"})
		rows, total := page.Items, page.Total
		if err != nil {
			t.Fatal(err)
		}
		recentView, err := queries.Recent(ctx, reader, book.LibraryID, 1)
		recent := recentView.Items
		if err != nil {
			t.Fatal(err)
		}
		if subject == uid {
			if total != 1 || len(rows) != 1 || rows[0].ReadCount != 20 || len(recent) != 1 || recent[0].LastReadPage.Int64 != 20 {
				t.Fatalf("owner rows=%+v total=%d recent=%+v", rows, total, recent)
			}
		} else if total != 0 || len(rows) != 0 || len(recent) != 0 {
			t.Fatalf("other inherited owner: rows=%+v total=%d recent=%+v", rows, total, recent)
		}
	}
}
