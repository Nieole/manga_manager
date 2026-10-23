package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestOnlinePositionPreservesOccurrenceAndRefreshesSamePage(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 20)
	for _, body := range []string{`{"page":5,"updated_at":"2026-10-20T10:00:00Z","fact_id":"first"}`, `{"page":5,"updated_at":"2026-10-20T11:00:00Z","fact_id":"second"}`} {
		req := requestWithRouteParam(http.MethodPost, "/", []byte(body), "bookId", strconv.FormatInt(book.ID, 10))
		rec := httptest.NewRecorder()
		controller.updateBookProgress(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
	}
	got, err := store.GetBook(context.Background(), book.ID)
	want := time.Date(2026, 10, 20, 11, 0, 0, 0, time.UTC)
	if err != nil || !got.LastReadAt.Time.Equal(want) {
		t.Fatalf("last_read_at=%v err=%v", got.LastReadAt, err)
	}
}
