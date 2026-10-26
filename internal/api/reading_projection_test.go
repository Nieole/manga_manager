package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"manga-manager/internal/database"
	"manga-manager/internal/reading"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestNavigationProjectsTargetForCurrentReader(t *testing.T) {
	c, store, _, _ := newTestController(t)
	lib, series, first := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 20)
	second, err := store.CreateBook(context.Background(), database.CreateBookParams{Name: "02.cbz", Path: first.Path + "2", LibraryID: lib.ID, SeriesID: series.ID, PageCount: 20, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	user := mkTestUser(t, store, "reader", "regular")
	if _, err = reading.New(store, nil).MarkBooks(context.Background(), reading.MarkBooksInput{BookIDs: []int64{first.ID, second.ID}, IsRead: true}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		handler    http.HandlerFunc
		from, want int64
	}{{"next", c.getNextBook, first.ID, second.ID}, {"prev", c.getPrevBook, second.ID, first.ID}} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := withUserContext(requestWithRouteParam(http.MethodGet, "/", nil, "bookId", strconv.FormatInt(test.from, 10)), user)
			test.handler(rec, req)
			var got database.Book
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if rec.Code != 200 || got.ID != test.want || got.IsRead || got.LastReadPage.Valid || got.LastReadAt.Valid {
				t.Fatalf("navigation leaked global: %d %+v", rec.Code, got)
			}
		})
	}
}

func TestReadingProjectionHTTPAndProtocolMatrix(t *testing.T) {
	for _, pages := range []int64{0, 20} {
		t.Run(strconv.FormatInt(pages, 10), func(t *testing.T) { readingProjectionMatrix(t, pages) })
	}
}

func readingProjectionMatrix(t *testing.T, pageCount int64) {
	c, store, _, _ := newTestController(t)
	lib, series, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", pageCount)
	owner := mkTestUser(t, store, "owner", "regular")
	other := mkTestUser(t, store, "other", "regular")
	ctx := context.Background()
	service := reading.New(store, nil)
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{BookIDs: []int64{book.ID}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	run := func(handler http.HandlerFunc, user database.User, param string, id int64, url string) []byte {
		t.Helper()
		rec := httptest.NewRecorder()
		handler(rec, withUserContext(requestWithRouteParam(http.MethodGet, url, nil, param, strconv.FormatInt(id, 10)), user))
		if rec.Code != 200 {
			t.Fatalf("GET %s: %d %s", url, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	stages := []struct {
		name      string
		page      int64
		mark      bool
		clear     bool
		wantPage  int64
		completed bool
		recent    int
	}{
		{name: "no record"},
		{name: "forward", page: 12, wantPage: 12, recent: 1},
		{name: "backward", page: 3, wantPage: 3, recent: 1},
		{name: "same page new time", page: 3, wantPage: 3, recent: 1},
		{name: "completed", mark: true, wantPage: pageCount, completed: true, recent: 1},
		{name: "cleared", clear: true},
	}
	for i, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			when := at.Add(time.Duration(i) * time.Hour)
			if stage.page > 0 {
				if _, err := service.RecordPosition(ctx, reading.Position{UserID: owner.ID, BookID: book.ID, Page: stage.page, OccurredAt: &when}); err != nil {
					t.Fatal(err)
				}
			}
			if stage.mark || stage.clear {
				if _, err := service.MarkBooks(ctx, reading.MarkBooksInput{UserID: owner.ID, BookIDs: []int64{book.ID}, IsRead: stage.mark, OccurredAt: &when}); err != nil {
					t.Fatal(err)
				}
			}
			// Global compatibility progress must not leak into the other authenticated subject.
			for _, user := range []database.User{owner, other} {
				wantPage, wantCompleted, wantRecent := stage.wantPage, stage.completed, stage.recent
				if user.ID == other.ID {
					wantPage = 0
					wantCompleted = false
					wantRecent = 0
				}
				activityBefore, activityErr := store.GetUserActivityHeatmap(ctx, user.ID, "-365 days")
				if activityErr != nil {
					t.Fatal(activityErr)
				}
				secondsBefore, timeErr := store.GetUserTotalReadingTime(ctx, user.ID)
				if timeErr != nil {
					t.Fatal(timeErr)
				}
				before, found, err := store.GetUserBookProgress(ctx, user.ID, book.ID)
				if err != nil {
					t.Fatal(err)
				}
				var detail database.Book
				if err = json.Unmarshal(run(c.getBookInfo, user, "bookId", book.ID, "/"), &detail); err != nil {
					t.Fatal(err)
				}
				var books []database.Book
				if err = json.Unmarshal(run(c.getBooksBySeries, user, "seriesId", series.ID, "/"), &books); err != nil {
					t.Fatal(err)
				}
				var summary SeriesContinue
				if err = json.Unmarshal(run(c.getSeriesContinueEndpoint, user, "seriesId", series.ID, "/"), &summary); err != nil {
					t.Fatal(err)
				}
				if len(books) != 1 || books[0].LastReadPage != detail.LastReadPage || books[0].IsRead != detail.IsRead || detail.LastReadPage.Int64 != wantPage || reading.InspectBook(detail).Completed != wantCompleted || summary.ReadPages != wantPage || (summary.ReadBooks == 1) != wantCompleted {
					t.Fatalf("subject=%d detail=%+v list=%+v summary=%+v", user.ID, detail, books, summary)
				}
				if wantCompleted && summary.NextUnreadBookID != 0 {
					t.Fatalf("all read must preserve fallback: %+v", summary)
				}
				var mihon []MihonBookResponse
				if err = json.Unmarshal(run(c.mihonSeriesBooks, user, "seriesId", series.ID, "/"), &mihon); err != nil {
					t.Fatal(err)
				}
				if len(mihon) != 1 || mihon[0].LastReadPage != wantPage {
					t.Fatalf("mihon=%+v", mihon)
				}
				for _, endpoint := range []http.HandlerFunc{c.getRecentReadAll, c.mihonContinueReading} {
					var items []json.RawMessage
					if err = json.Unmarshal(run(endpoint, user, "", 0, "/?limit=10"), &items); err != nil {
						t.Fatal(err)
					}
					if len(items) != wantRecent {
						t.Fatalf("recent subject=%d: %s", user.ID, items)
					}
				}
				var feed OPDSFeed
				if err = xml.Unmarshal(run(c.opdsContinueReading, user, "", 0, "/?limit=10"), &feed); err != nil {
					t.Fatal(err)
				}
				if len(feed.Entries) != wantRecent {
					t.Fatalf("OPDS recent=%+v", feed.Entries)
				}
				feed = OPDSFeed{}
				if err = xml.Unmarshal(run(c.opdsSeriesBooks, user, "seriesId", series.ID, "/"), &feed); err != nil {
					t.Fatal(err)
				}
				if len(feed.Entries) != 1 {
					t.Fatalf("OPDS books=%+v", feed.Entries)
				}
				var search struct {
					Items []database.SearchSeriesPagedRow `json:"items"`
				}
				if err = json.Unmarshal(run(c.searchSeriesPaged, user, "", 0, "/?libraryId="+strconv.FormatInt(lib.ID, 10)), &search); err != nil {
					t.Fatal(err)
				}
				if len(search.Items) != 1 || int64(search.Items[0].ReadCount) != wantPage {
					t.Fatalf("search=%+v", search.Items)
				}
				activityAfter, activityErr := store.GetUserActivityHeatmap(ctx, user.ID, "-365 days")
				if activityErr != nil || !reflect.DeepEqual(activityBefore, activityAfter) {
					t.Fatalf("reads changed activity: before=%+v after=%+v err=%v", activityBefore, activityAfter, activityErr)
				}
				secondsAfter, timeErr := store.GetUserTotalReadingTime(ctx, user.ID)
				if timeErr != nil || secondsBefore != secondsAfter {
					t.Fatalf("reads changed duration: before=%d after=%d err=%v", secondsBefore, secondsAfter, timeErr)
				}
				after, afterFound, err := store.GetUserBookProgress(ctx, user.ID, book.ID)
				if err != nil || found != afterFound || before != after {
					t.Fatalf("reads changed position: before=%+v after=%+v err=%v", before, after, err)
				}
			}
		})
	}
}
