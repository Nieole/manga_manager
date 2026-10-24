package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"manga-manager/internal/database"
)

func TestUnknownReadMarksRemainCompletedWithoutPagesInSeriesResponses(t *testing.T) {
	for _, perUser := range []bool{false, true} {
		t.Run(strconv.FormatBool(perUser), func(t *testing.T) {
			controller, store, _, _ := newTestController(t)
			_, series, book := seedBookFixture(t, store, t.TempDir(), "Lib", "Series", "book.cbz", 0)
			user := database.User{}
			if perUser {
				user = mkTestUser(t, store, "reader", "regular")
			}
			body := []byte(`{"book_ids":[` + strconv.FormatInt(book.ID, 10) + `],"is_read":true,"occurred_at":"2026-10-20T10:00:00Z","operation_id":"unknown"}`)
			marked := httptest.NewRecorder()
			controller.bulkUpdateBookProgress(marked, withUserContext(httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)), user))
			if marked.Code != 200 {
				t.Fatalf("%d %s", marked.Code, marked.Body.String())
			}
			readContext := func() SeriesContextResponse {
				t.Helper()
				rec := httptest.NewRecorder()
				controller.getSeriesContext(rec, withUserContext(requestWithRouteParam(http.MethodGet, "/", nil, "seriesId", strconv.FormatInt(series.ID, 10)), user))
				if rec.Code != 200 {
					t.Fatalf("%d %s", rec.Code, rec.Body.String())
				}
				var response SeriesContextResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				return response
			}
			for _, pageCount := range []int64{0, 20} {
				if _, err := store.DB().Exec(`UPDATE books SET page_count=? WHERE id=?`, pageCount, book.ID); err != nil {
					t.Fatal(err)
				}
				if err := store.RefreshSeriesDerivedData(context.Background(), series.ID); err != nil {
					t.Fatal(err)
				}
				response := readContext()
				if len(response.Books) != 1 || !response.Books[0].IsRead || response.Books[0].LastReadPage.Valid || response.Continue.ReadPages != 0 || response.Continue.ReadBooks != 1 {
					t.Fatalf("pageCount=%d response=%+v", pageCount, response)
				}
			}
		})
	}
}
