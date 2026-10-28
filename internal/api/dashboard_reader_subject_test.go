package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-manager/internal/database"
	"manga-manager/internal/reading"
)

func TestDashboardRecentReadRejectsInvalidSubjectInsteadOfAnonymousFallback(t *testing.T) {
	c, store, _, _ := newTestController(t)
	_, _, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "01.cbz", 20)
	at := time.Now()
	if _, err := reading.New(store, nil).MarkBooks(context.Background(), reading.MarkBooksInput{BookIDs: []int64{book.ID}, IsRead: true, OccurredAt: &at}); err != nil {
		t.Fatal(err)
	}
	req := withUserContext(httptest.NewRequest(http.MethodGet, "/?limit=1", nil), database.User{ID: -1})
	rec := httptest.NewRecorder()
	c.getRecentReadAll(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid reader inherited anonymous results: %d %s", rec.Code, rec.Body.String())
	}
}
