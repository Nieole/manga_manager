package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"manga-manager/internal/database"
)

func TestBookmarkHTTPUsesAuthenticatedReaderDespitePayloadIdentity(t *testing.T) {
	c, store, _, dir := newTestController(t)
	_, _, book := seedBookFixture(t, store, dir, "Lib", "Series", "book.cbz", 12)
	_, _, other := seedBookFixture(t, store, dir, "OtherLib", "OtherSeries", "other.cbz", 12)
	alice := mkTestUser(t, store, "alice", database.RoleRegular)
	bob := mkTestUser(t, store, "bob", database.RoleRegular)
	save := func(user database.User, body string) database.ReadingBookmark {
		t.Helper()
		rec := httptest.NewRecorder()
		c.upsertReadingBookmark(rec, withUserContext(requestWithRouteParam(http.MethodPost, "/x", []byte(body), "bookId", strconv.FormatInt(book.ID, 10)), user))
		var got database.ReadingBookmark
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil {
			t.Fatalf("save=%d %s", rec.Code, rec.Body.String())
		}
		return got
	}
	bobs := save(bob, `{"page":5,"note":"private"}`)
	alices := save(alice, fmt.Sprintf(`{"page":5,"note":"mine","user_id":%d,"book_id":%d,"id":%d}`, bob.ID, other.ID, bobs.ID))
	if alices.ID == bobs.ID || alices.UserID != alice.ID || alices.BookID != book.ID {
		t.Fatalf("payload overrode subject: %+v", alices)
	}
	for _, ids := range []struct {
		user   database.User
		bookID int64
	}{{alice, book.ID}, {bob, other.ID}} {
		rec := httptest.NewRecorder()
		c.deleteReadingBookmark(rec, withUserContext(requestWithRouteParams(http.MethodDelete, "/x", nil, map[string]string{
			"bookId": strconv.FormatInt(ids.bookID, 10), "bookmarkId": strconv.FormatInt(bobs.ID, 10),
		}), ids.user))
		if rec.Code != 404 {
			t.Fatalf("cross-subject delete=%d %s", rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	c.listReadingBookmarks(rec, withUserContext(requestWithRouteParam(http.MethodGet, "/x", nil, "bookId", strconv.FormatInt(book.ID, 10)), bob))
	var items []database.ReadingBookmark
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &items) != nil || len(items) != 1 || items[0].ID != bobs.ID || items[0].Note != "private" {
		t.Fatalf("private bookmark changed: %d %s", rec.Code, rec.Body.String())
	}
}

func TestBookmarkHTTPBoundsIdentityAndMissingObjects(t *testing.T) {
	c, store, _, dir := newTestController(t)
	_, _, book := seedBookFixture(t, store, dir, "Lib", "Series", "book.cbz", 12)
	bid := strconv.FormatInt(book.ID, 10)
	request := func(method, bookID, bookmarkID, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := requestWithRouteParams(method, "/x", []byte(body), map[string]string{"bookId": bookID, "bookmarkId": bookmarkID})
		rec := httptest.NewRecorder()
		switch method {
		case http.MethodGet:
			c.listReadingBookmarks(rec, req)
		case http.MethodPost:
			c.upsertReadingBookmark(rec, req)
		case http.MethodDelete:
			c.deleteReadingBookmark(rec, req)
		}
		return rec
	}
	if rec := request(http.MethodGet, bid, "", ""); rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("empty list=%d %s", rec.Code, rec.Body.String())
	}
	var first database.ReadingBookmark
	for _, body := range []string{`{"page":999,"note":"  私人备注  "}`, `{"page":12,"note":""}`} {
		rec := request(http.MethodPost, bid, "", body)
		var got database.ReadingBookmark
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got.Page != 12 {
			t.Fatalf("save=%d %s", rec.Code, rec.Body.String())
		}
		if first.ID == 0 {
			first = got
			if first.Note != "私人备注" {
				t.Fatalf("note=%q", first.Note)
			}
		} else if got.ID != first.ID || got.Note != "" {
			t.Fatalf("update=%+v first=%+v", got, first)
		}
	}
	for _, tc := range []struct {
		method, bid, id, body, message string
		code                           int
	}{
		{http.MethodGet, "bad", "", "", "Invalid book ID", 400},
		{http.MethodPost, bid, "", "{", "Invalid request payload", 400},
		{http.MethodDelete, bid, "bad", "", "Invalid bookmark ID", 400},
		{http.MethodGet, "999999", "", "", "Book not found", 404},
		{http.MethodPost, "999999", "", `{"page":1}`, "Book not found", 404},
		{http.MethodDelete, "999999", strconv.FormatInt(first.ID, 10), "", "Bookmark not found", 404},
		{http.MethodDelete, bid, "999999", "", "Bookmark not found", 404},
	} {
		rec := request(tc.method, tc.bid, tc.id, tc.body)
		if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.message) {
			t.Fatalf("%+v got=%d %s", tc, rec.Code, rec.Body.String())
		}
	}
}

func TestBookmarkHTTPStorageFailureResults(t *testing.T) {
	for _, op := range []string{"list", "save", "delete", "book lookup"} {
		t.Run(op, func(t *testing.T) {
			c, store, _, dir := newTestController(t)
			_, _, book := seedBookFixture(t, store, dir, "Lib", "Series", "book.cbz", 12)
			bid := strconv.FormatInt(book.ID, 10)
			rec := httptest.NewRecorder()
			want, message := 500, ""
			if op == "book lookup" {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				want, message = 404, "Book not found"
				c.listReadingBookmarks(rec, requestWithRouteParam(http.MethodGet, "/x", nil, "bookId", bid))
			} else {
				if _, err := store.DB().Exec(`ALTER TABLE reading_bookmarks RENAME TO unavailable_bookmarks`); err != nil {
					t.Fatal(err)
				}
				switch op {
				case "list":
					message = "Failed to load reading bookmarks"
					c.listReadingBookmarks(rec, requestWithRouteParam(http.MethodGet, "/x", nil, "bookId", bid))
				case "save":
					message = "Failed to save reading bookmark"
					c.upsertReadingBookmark(rec, requestWithRouteParam(http.MethodPost, "/x", []byte(`{"page":1}`), "bookId", bid))
				case "delete":
					message = "Failed to delete reading bookmark"
					c.deleteReadingBookmark(rec, requestWithRouteParams(http.MethodDelete, "/x", nil, map[string]string{"bookId": bid, "bookmarkId": "1"}))
				}
			}
			if rec.Code != want || !strings.Contains(rec.Body.String(), message) {
				t.Fatalf("want=%d %s got=%d %s", want, message, rec.Code, rec.Body.String())
			}
		})
	}
}
