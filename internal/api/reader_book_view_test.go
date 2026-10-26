package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestBookReaderEndpointsRejectInvalidSubject(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, series, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 20)
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		param   string
		id      int64
	}{
		{"detail", c.getBookInfo, "bookId", book.ID},
		{"next", c.getNextBook, "bookId", book.ID},
		{"previous", c.getPrevBook, "bookId", book.ID},
		{"series", c.getBooksBySeries, "seriesId", series.ID},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := withUserContext(requestWithRouteParam(http.MethodGet, "/", nil, test.param, strconv.FormatInt(test.id, 10)), database.User{ID: -1})
			rec := httptest.NewRecorder()
			test.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid subject became reader: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestBookInfoPreservesFlatCompatibilityContract(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 20)
	expected, err := store.GetBook(context.Background(), book.ID)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c.getBookInfo(rec, requestWithRouteParam(http.MethodGet, "/", nil, "bookId", strconv.FormatInt(book.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("detail=%d %s", rec.Code, rec.Body.String())
	}
	var got, want map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	expected.Path = ""
	data, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("flat book contract changed: got=%v want=%v", got, want)
	}
}
