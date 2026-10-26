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
	isolated, err := view.Book(ctx, uid, bid)
	if err != nil {
		t.Fatal(err)
	}
	if isolated.IsRead || isolated.LastReadPage.Valid || isolated.LastReadAt.Valid {
		t.Fatalf("reader inherited global state: %+v", isolated)
	}
	legacy, err := view.Book(ctx, 0, bid)
	if err != nil || !reading.InspectBook(legacy).Completed {
		t.Fatalf("legacy=%+v err=%v", legacy, err)
	}
	if _, err = reading.New(store, nil).RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 12, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	if _, err = reading.New(store, nil).RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 3, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	series, err := view.Series(ctx, uid, book.SeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if series.Continue.ReadPages != 3 || series.Continue.LastReadPage != 3 || series.Continue.NextUnreadBookID != bid {
		t.Fatalf("backward series=%+v", series)
	}
	isolated, err = view.Book(ctx, uid, bid)
	if err != nil || isolated.LastReadPage.Int64 != 3 || !isolated.LastReadAt.Time.Equal(later) {
		t.Fatalf("detail=%+v err=%v", isolated, err)
	}
	if _, err = reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{bid}, IsRead: false, OccurredAt: timePointer(later.Add(time.Hour))}); err != nil {
		t.Fatal(err)
	}
	series, err = view.Series(ctx, uid, book.SeriesID)
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
	before, err := reading.RecentReads(ctx, store, uid, 10)
	if err != nil {
		t.Fatal(err)
	}
	view, err := queries.Series(ctx, uid, first.SeriesID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Continue.ReadBooks != 3 || view.Continue.TotalPages != 30 || view.Continue.ReadPages != 30 || view.Continue.NextUnreadBookID != 0 {
		t.Fatalf("all read summary=%+v", view.Continue)
	}
	detail, err := queries.Book(ctx, uid, unknown.ID)
	if err != nil || !detail.IsRead || detail.LastReadPage.Valid || !reading.InspectBook(detail).Completed {
		t.Fatalf("unknown=%+v err=%v", detail, err)
	}
	next, err := queries.Adjacent(ctx, uid, unknown.ID, true)
	if err != nil || next.ID != third.ID {
		t.Fatalf("next=%+v err=%v", next, err)
	}
	prev, err := queries.Adjacent(ctx, uid, third.ID, false)
	if err != nil || prev.ID != unknown.ID {
		t.Fatalf("previous=%+v err=%v", prev, err)
	}
	after, err := reading.RecentReads(ctx, store, uid, 10)
	if err != nil || len(before) != 1 || len(after) != 1 || !after[0].LastReadAt.Time.Equal(at) || before[0].LastReadAt != after[0].LastReadAt {
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
	filters := database.SeriesListFilters{UserID: uid, ReadState: "completed"}
	for _, subject := range []int64{uid, other.ID} {
		rows, total, err := queries.SearchPaged(ctx, subject, book.LibraryID, filters, 1, 0, "name_asc")
		if err != nil {
			t.Fatal(err)
		}
		recent, err := queries.RecentSeries(ctx, subject, book.LibraryID, 1)
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
