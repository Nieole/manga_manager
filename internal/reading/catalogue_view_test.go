package reading_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination(t *testing.T) {
	store, uid, bid := positionFixture(t)
	ctx := context.Background()
	first, err := store.GetBook(ctx, bid)
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, database.CreateUserParams{Username: "other", PasswordHash: "x", Role: "regular"})
	if err != nil {
		t.Fatal(err)
	}
	subject, _ := reading.UserSubject(uid)
	otherSubject, _ := reading.UserSubject(other.ID)
	queries := reading.NewCatalogueQueries(store)
	service := reading.New(store, nil)
	seriesIDs := make([]int64, 4)
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	for i, name := range []string{"reader A unknown", "reader B other", "reader C complete", "reader D anonymous"} {
		series, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: name, Path: first.Path + name, LibraryID: first.LibraryID})
		if err != nil {
			t.Fatal(err)
		}
		pages := int64(10)
		if i == 0 {
			pages = 0
		}
		book, err := store.CreateBook(ctx, database.CreateBookParams{Name: "book", Path: first.Path + name + "/book", LibraryID: first.LibraryID, SeriesID: series.ID, PageCount: pages, FileModifiedAt: at})
		if err != nil {
			t.Fatal(err)
		}
		actor := uid
		if i == 1 {
			actor = other.ID
		}
		if i == 3 {
			actor = 0
		}
		if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: actor, BookIDs: []int64{book.ID}, IsRead: true, OccurredAt: &at}); err != nil {
			t.Fatal(err)
		}
		if err := store.RefreshSeriesDerivedData(ctx, series.ID); err != nil {
			t.Fatal(err)
		}
		seriesIDs[i] = series.ID
	}
	if _, err := service.RecordPosition(ctx, reading.Position{UserID: uid, BookID: bid, Page: 4, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	input := reading.CatalogueSearch{LibraryID: first.LibraryID, Filters: reading.CatalogueFilters{Keyword: "reader", ReadState: "completed"}, Limit: 1, SortBy: "name_asc"}
	page, err := queries.Search(ctx, subject, input)
	if err != nil {
		t.Fatal(err)
	}
	if page.Subject != subject || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != seriesIDs[0] || page.Items[0].ReadCount != 0 || page.Items[0].TotalPages.Float64 != 0 || !page.HasMore || page.NextCursor == "" {
		t.Fatalf("first page=%+v", page)
	}
	input.Cursor = page.NextCursor
	next, err := queries.Search(ctx, subject, input)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != seriesIDs[2] || next.HasMore || next.Total != 0 {
		t.Fatalf("cursor page=%+v err=%v", next, err)
	}
	input.Cursor = "invalid cursor"
	input.Offset = 1
	fallback, err := queries.Search(ctx, subject, input)
	if err != nil || len(fallback.Items) != 1 || fallback.Items[0].ID != seriesIDs[2] || fallback.Total != 2 || fallback.HasMore {
		t.Fatalf("fallback=%+v err=%v", fallback, err)
	}
	input.Cursor = ""
	input.Offset = 0
	for _, test := range []struct {
		subject reading.Subject
		want    int64
	}{{otherSubject, seriesIDs[1]}, {reading.AnonymousSubject(), seriesIDs[3]}} {
		page, err := queries.Search(ctx, test.subject, input)
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != test.want {
			t.Fatalf("isolated=%+v err=%v", page, err)
		}
	}
	input.Filters = reading.CatalogueFilters{}
	input.SortBy = "read_desc"
	input.Offset = 1
	sorted, err := queries.Search(ctx, subject, input)
	if err != nil || len(sorted.Items) != 1 || sorted.Items[0].ID != first.SeriesID || sorted.Items[0].ReadCount != 4 || sorted.Total != 5 {
		t.Fatalf("sorted=%+v err=%v", sorted, err)
	}
	if _, err := queries.Search(ctx, reading.Subject{}, input); !errors.Is(err, reading.ErrInvalidSubject) {
		t.Fatalf("invalid subject=%v", err)
	}
}
