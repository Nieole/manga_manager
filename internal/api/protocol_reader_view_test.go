package api

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestProtocolReaderQueriesRejectInvalidSubject(t *testing.T) {
	c, store, _, _ := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 0)
	for _, test := range []struct {
		name          string
		handler       http.HandlerFunc
		target, param string
		id            int64
	}{
		{"opds books", c.reader.opdsSeriesBooks, "/", "seriesId", series.ID},
		{"mihon books", c.reader.mihonSeriesBooks, "/", "seriesId", series.ID},
		{"opds continue", c.reader.opdsContinueReading, "/", "", 0},
		{"mihon continue", c.reader.mihonContinueReading, "/", "", 0},
		{"mihon series", c.reader.mihonSeries, "/?libraryId=" + strconv.FormatInt(lib.ID, 10), "", 0},
		{"mihon search", c.reader.mihonSeries, "/?q=Series", "", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := withUserContext(requestWithRouteParam(http.MethodGet, test.target, nil, test.param, strconv.FormatInt(test.id, 10)), database.User{ID: -1})
			test.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid subject=%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestProtocolsPreserveReaderIsolationAnonymousProgressAndPagedLinks(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, series, first := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 20)
	ctx := context.Background()
	unknown, err := store.CreateBook(ctx, database.CreateBookParams{Name: "02.cbz", Path: first.Path + "2", LibraryID: first.LibraryID, SeriesID: series.ID, FileModifiedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	owner := mkTestUser(t, store, "owner", "regular")
	other := mkTestUser(t, store, "other", "regular")
	at := time.Date(2026, 10, 27, 6, 0, 0, 0, time.UTC)
	writes := reading.New(store, nil)
	if _, err := writes.RecordPosition(ctx, reading.Position{BookID: first.ID, Page: 7, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	later := at.Add(time.Hour)
	if _, err := writes.RecordPosition(ctx, reading.Position{UserID: owner.ID, BookID: first.ID, Page: 3, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	later = later.Add(time.Hour)
	if _, err := writes.MarkBooks(ctx, reading.MarkBooksInput{UserID: owner.ID, BookIDs: []int64{unknown.ID}, IsRead: true, OccurredAt: &later}); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		user       *database.User
		page       int64
		recent     int
		recentBook int64
	}{{"owner", &owner, 3, 1, unknown.ID}, {"other", &other, 0, 0, 0}, {"anonymous", nil, 7, 1, first.ID}} {
		t.Run(test.name, func(t *testing.T) {
			request := func(target, param string, id int64) *http.Request {
				req := requestWithRouteParam(http.MethodGet, target, nil, param, strconv.FormatInt(id, 10))
				if test.user != nil {
					req = withUserContext(req, *test.user)
				}
				return req
			}
			booksRec := httptest.NewRecorder()
			c.reader.mihonSeriesBooks(booksRec, request("/", "seriesId", series.ID))
			var books []MihonBookResponse
			if err := json.Unmarshal(booksRec.Body.Bytes(), &books); err != nil {
				t.Fatal(err)
			}
			if booksRec.Code != 200 || len(books) != 2 || books[0].ID != first.ID || books[0].LastReadPage != test.page || books[1].ID != unknown.ID || books[1].PageCount != 0 || books[1].LastReadPage != 0 {
				t.Fatalf("books=%+v status=%d", books, booksRec.Code)
			}
			for page := 1; page <= 2; page++ {
				rec := httptest.NewRecorder()
				c.reader.opdsSeriesBooks(rec, request("/?limit=1&page="+strconv.Itoa(page), "seriesId", series.ID))
				var feed OPDSFeed
				if err := xml.Unmarshal(rec.Body.Bytes(), &feed); err != nil {
					t.Fatal(err)
				}
				if rec.Code != 200 || len(feed.Entries) != 1 {
					t.Fatalf("page=%d status=%d feed=%+v", page, rec.Code, feed)
				}
				stream := findOPDSLink(feed.Entries[0].Links, opdsPSEStreamRel)
				if page == 1 {
					if feed.Entries[0].ID != "urn:manga-manager:opds:book:"+strconv.FormatInt(first.ID, 10) || stream == nil || findOPDSLink(feed.Links, "next") == nil {
						t.Fatalf("known page=%+v", feed)
					}
					if test.page > 0 && !strings.Contains(rec.Body.String(), "pse:lastRead="+strconv.Quote(strconv.FormatInt(test.page, 10))) {
						t.Fatalf("reader position missing: %s", rec.Body.String())
					}
					if test.page == 0 && strings.Contains(rec.Body.String(), "pse:lastRead=") {
						t.Fatalf("reader inherited position: %s", rec.Body.String())
					}
				} else if stream != nil || findOPDSLink(feed.Links, "previous") == nil {
					t.Fatalf("unknown page=%+v", feed)
				}
			}
			recentRec := httptest.NewRecorder()
			c.reader.mihonContinueReading(recentRec, request("/?limit=1", "", 0))
			var recent []MihonContinueItemResponse
			if err := json.Unmarshal(recentRec.Body.Bytes(), &recent); err != nil {
				t.Fatal(err)
			}
			if recentRec.Code != 200 || len(recent) != test.recent {
				t.Fatalf("recent=%+v status=%d", recent, recentRec.Code)
			}
			if len(recent) > 0 && (recent[0].BookID != test.recentBook || test.name == "owner" && recent[0].LastReadPage != 0) {
				t.Fatalf("recent wrong reader or sentinel=%+v", recent)
			}
		})
	}
}
