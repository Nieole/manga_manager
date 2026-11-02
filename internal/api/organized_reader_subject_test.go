package api

import (
	"context"
	"manga-manager/internal/database"
	"manga-manager/internal/organize"
	"manga-manager/internal/reading"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func testReaderSubject(t *testing.T, uid int64) reading.Subject {
	t.Helper()
	if uid == 0 {
		return reading.AnonymousSubject()
	}
	subject, err := reading.UserSubject(uid)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func TestOrganizedReaderEndpointsRejectInvalidSubject(t *testing.T) {
	c, store, _, _ := newTestController(t)
	lib, _, _ := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 0)
	filter, err := organize.NewSmartCollections(store).Save(context.Background(), lib.ID, organize.SaveSmartFilter{Name: "Shelf"})
	if err != nil {
		t.Fatal(err)
	}
	list, err := c.organizationHTTP.readingLists().Create(context.Background(), "List", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		param   string
		id      int64
	}{
		{"views", c.organizationHTTP.listCollectionViews, "", 0}, {"smart", c.organizationHTTP.getSmartCollectionSeries, "filterId", filter.ID},
		{"items", c.organizationHTTP.listReadingListItems, "listId", list.ID}, {"opds views", c.reader.opdsCollections, "", 0},
		{"mihon views", c.reader.mihonCollections, "", 0}, {"opds list", c.reader.opdsReadingListSeries, "listId", list.ID}, {"mihon list", c.reader.mihonReadingListSeries, "listId", list.ID},
	} {
		t.Run(test.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := withUserContext(requestWithRouteParam(http.MethodGet, "/", nil, test.param, strconv.FormatInt(test.id, 10)), database.User{ID: -1})
			test.handler(rec, req)
			if rec.Code != 400 {
				t.Fatalf("invalid subject=%d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
