package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestBulkMarkUnreadRejectsDelayedOnlineReplay(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 20)
	id := strconv.FormatInt(book.ID, 10)
	mark := httptest.NewRecorder()
	controller.bulkUpdateBookProgress(mark, httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress", strings.NewReader(`{"book_ids":[`+id+`],"is_read":false,"occurred_at":"2026-10-20T12:00:00Z","operation_id":"clear-reader"}`)))
	if mark.Code != http.StatusOK {
		t.Fatalf("mark=%d %s", mark.Code, mark.Body.String())
	}
	replay := httptest.NewRecorder()
	controller.updateBookProgress(replay, requestWithRouteParam(http.MethodPost, "/", []byte(`{"page":15,"updated_at":"2026-10-20T11:00:00Z"}`), "bookId", id))
	if replay.Code != http.StatusOK || statusOf(replay) != "Progress unchanged" {
		t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
	}
	got, err := store.GetBook(context.Background(), book.ID)
	if err != nil || got.LastReadPage.Valid || got.LastReadAt.Valid {
		t.Fatalf("cleared book=%+v err=%v", got, err)
	}
}
