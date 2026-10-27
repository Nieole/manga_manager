package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestSeriesReaderQueriesRejectInvalidSubject(t *testing.T) {
	c, store, _, _ := newTestController(t)
	lib, series, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 0)
	for _, test := range []struct {
		name       string
		handler    http.HandlerFunc
		url, param string
		id         int64
	}{
		{"search", c.searchSeriesPaged, "/?libraryId=" + strconv.FormatInt(lib.ID, 10), "", 0},
		{"recent", c.getRecentReadSeries, "/?libraryId=" + strconv.FormatInt(lib.ID, 10), "", 0},
		{"context", c.getSeriesContext, "/", "seriesId", series.ID},
		{"continue", c.getSeriesContinueEndpoint, "/", "seriesId", series.ID},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := withUserContext(requestWithRouteParam(http.MethodGet, test.url, nil, test.param, strconv.FormatInt(test.id, 10)), database.User{ID: -1})
			rec := httptest.NewRecorder()
			test.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid subject=%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestSeriesSearchPagesOnlyTheCurrentReadersUnknownPageCompletions(t *testing.T) {
	c, store, _, _ := newTestController(t)
	lib, _, first := seedBookFixture(t, store, t.TempDir(), "Lib", "Unselected", "01.cbz", 0)
	owner := mkTestUser(t, store, "owner", "regular")
	other := mkTestUser(t, store, "other", "regular")
	ctx := context.Background()
	ids := make([]int64, 3)
	for i, name := range []string{"reader A", "reader B", "reader C"} {
		series, err := store.CreateSeries(ctx, database.CreateSeriesParams{Name: name, Path: first.Path + name, LibraryID: lib.ID})
		if err != nil {
			t.Fatal(err)
		}
		book, err := store.CreateBook(ctx, database.CreateBookParams{Name: "book", Path: first.Path + name + "/book", LibraryID: lib.ID, SeriesID: series.ID, FileModifiedAt: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		uid := owner.ID
		if i == 2 {
			uid = other.ID
		}
		if _, err := reading.New(store, nil).MarkBooks(ctx, reading.MarkBooksInput{UserID: uid, BookIDs: []int64{book.ID}, IsRead: true}); err != nil {
			t.Fatal(err)
		}
		if err := store.RefreshSeriesDerivedData(ctx, series.ID); err != nil {
			t.Fatal(err)
		}
		ids[i] = series.ID
	}
	for _, test := range []struct {
		user      database.User
		page      string
		wantID    int64
		wantTotal int
	}{
		{owner, "1", ids[0], 2}, {owner, "2", ids[1], 2}, {other, "1", ids[2], 1},
	} {
		target := "/?libraryId=" + strconv.FormatInt(lib.ID, 10) + "&q=reader&readState=completed&limit=1&sortBy=name_asc&page=" + test.page + "&cursor=invalid"
		rec := httptest.NewRecorder()
		c.searchSeriesPaged(rec, withUserContext(httptest.NewRequest(http.MethodGet, target, nil), test.user))
		var result struct {
			Items []database.SearchSeriesPagedRow `json:"items"`
			Total int                             `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 200 || result.Total != test.wantTotal || len(result.Items) != 1 || result.Items[0].ID != test.wantID || result.Items[0].ReadCount != 0 || result.Items[0].LastReadPage.Valid {
			t.Fatalf("reader=%d page=%s status=%d result=%+v", test.user.ID, test.page, rec.Code, result)
		}
	}
}
