package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"manga-manager/internal/database"
)

func TestOfflineTransportsShareOccurrenceAndActivityMatrix(t *testing.T) {
	pinLocalZone(t, "CST", 8*3600)
	for _, transport := range []string{"bulk", "fallback"} {
		for _, authenticated := range []bool{false, true} {
			t.Run(transport+"/user="+strconv.FormatBool(authenticated), func(t *testing.T) {
				controller, store, _, rootDir := newTestController(t)
				_, _, book := seedBookFixture(t, store, rootDir, "Library", "Series", "book.cbz", 20)
				user := database.User{}
				if authenticated {
					user = mkTestUser(t, store, "reader", "regular")
				}
				reports := []struct {
					page    int64
					at, id  string
					applied bool
				}{
					{10, "2026-10-19T23:30:00Z", "A", true},
					{4, "2026-10-20T23:30:00Z", "B", true},
					{4, "2026-10-21T23:30:00Z", "C", true},
					{4, "2026-10-23T23:30:00Z", "C", false},
					{18, "2026-10-18T23:30:00Z", "stale", false},
					{4, "2026-10-21T23:30:00Z", "", false},
				}
				for _, report := range reports {
					rec := httptest.NewRecorder()
					item := map[string]any{"page": report.page, "updated_at": report.at, "fact_id": report.id}
					if transport == "bulk" {
						item["book_id"] = book.ID
						body, _ := json.Marshal(map[string]any{"items": []any{item}})
						controller.bulkSyncBookProgress(rec, withUserContext(httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)), user))
						var response struct {
							Updated int `json:"updated"`
						}
						if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
							t.Fatal(err)
						}
						if (response.Updated == 1) != report.applied {
							t.Fatalf("report=%+v body=%s", report, rec.Body.String())
						}
					} else {
						body, _ := json.Marshal(item)
						req := requestWithRouteParam(http.MethodPost, "/", body, "bookId", strconv.FormatInt(book.ID, 10))
						controller.updateBookProgress(rec, withUserContext(req, user))
						if (statusOf(rec) == "Progress updated") != report.applied {
							t.Fatalf("report=%+v body=%s", report, rec.Body.String())
						}
					}
					if rec.Code != 200 {
						t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
					}
				}
				at := time.Date(2026, 10, 21, 23, 30, 0, 0, time.UTC)
				got, err := store.GetBook(context.Background(), book.ID)
				if authenticated {
					p, _, e := store.GetUserBookProgress(context.Background(), user.ID, book.ID)
					got.LastReadPage, got.LastReadAt, err = p.LastReadPage, p.LastReadAt, e
				}
				if err != nil || got.LastReadPage.Int64 != 4 || !got.LastReadAt.Time.Equal(at) {
					t.Fatalf("progress=%+v at=%v err=%v", got.LastReadPage, got.LastReadAt, err)
				}
				days, err := store.GetActivityHeatmap(context.Background(), "2026-10-01")
				if err != nil || len(days) != 3 {
					t.Fatalf("activity days=%+v err=%v", days, err)
				}
				wantDays := []string{"2026-10-20", "2026-10-21", "2026-10-22"}
				for i, day := range days {
					if day.Date != wantDays[i] {
						t.Fatalf("days=%+v", days)
					}
				}
				if authenticated {
					stats, err := store.GetUserPeriodStats(context.Background(), user.ID, 2026, 10)
					if err != nil || stats.Pages != 18 || stats.ActiveDays != 3 {
						t.Fatalf("stats=%+v err=%v", stats, err)
					}
				}
			})
		}
	}
}
