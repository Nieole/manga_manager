package api

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestBookmarkAndTimeEndpointsRejectInvalidReader(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 20)
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		body    string
	}{
		{"list", c.listReadingBookmarks, ""}, {"save", c.upsertReadingBookmark, `{"page":4,"note":"private"}`},
		{"delete", c.deleteReadingBookmark, ""}, {"time", c.addBookReadingTime, `{"seconds":30}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := requestWithRouteParams(http.MethodPost, "/", []byte(tc.body), map[string]string{"bookId": strconv.FormatInt(book.ID, 10), "bookmarkId": "1"})
			req = withUserContext(req, database.User{ID: -1})
			rec := httptest.NewRecorder()
			tc.handler(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("invalid subject status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
