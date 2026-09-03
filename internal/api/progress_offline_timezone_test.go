// 守「离线补传的进度与在线写入落进同一套时间文本口径」。
//
// last_read_at 是 Go time.Time 经驱动以 t.String() 落库的本地墙钟串，所有排序与期间筛选都对它做
// SQL 文本比较（见 database.ActivityDayKey 与 GetUserPeriodStats）。补传体里的 updated_at 是客户端
// 的 ISO 8601（恒带 Z），若原样落库就会在同一列里混进 UTC 墙钟，服务器不在 UTC 时整列比错。

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

// pinLocalZone 把进程时区钉在给定偏移上，用例结束还原。
//
// 用 time.FixedZone 而不是 TZ 环境变量或 time.LoadLocation：后两者要系统 tzdata，Windows 上未必有，
// 且 time.Local 只在首次使用时读一次 TZ，用例中途改 TZ 不生效。本包用例不并行，改全局变量是安全的。
func pinLocalZone(t *testing.T, name string, offsetSeconds int) {
	t.Helper()
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	time.Local = time.FixedZone(name, offsetSeconds)
}

// bulkSyncOfflineItem 按离线队列的真实报文发一条补传：updated_at 是 toISOString() 的 UTC 文本。
func bulkSyncOfflineItem(t *testing.T, controller *Controller, user database.User, bookID, page int64, updatedAtUTC string) {
	t.Helper()
	body := []byte(`{"items":[{"book_id":` + strconv.FormatInt(bookID, 10) +
		`,"page":` + strconv.FormatInt(page, 10) + `,"updated_at":"` + updatedAtUTC + `"}]}`)
	rec := httptest.NewRecorder()
	req := withUserContext(httptest.NewRequest(http.MethodPost, "/api/books/bulk-progress/sync", bytes.NewReader(body)), user)
	controller.bulkSyncBookProgress(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk sync: expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Updated int                          `json:"updated"`
		Results []BulkSyncProgressResultItem `json:"results"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode bulk sync response: %v", err)
	}
	if resp.Updated != 1 {
		t.Fatalf("expected 1 updated, got %d (results=%+v)", resp.Updated, resp.Results)
	}
}

// TestBulkSyncProgressUTCTimestampWinsContinueReading：离线读完的那卷比在线读过的更晚，
// 「继续阅读」必须指向它。UTC+8 下补传体的 2030-09-30T23:00Z 就是本地 2030-10-01 07:00，
// 比在线那条晚一小时；把它按 UTC 墙钟落库，文本上反而落到在线那条前面。
func TestBulkSyncProgressUTCTimestampWinsContinueReading(t *testing.T) {
	pinLocalZone(t, "CST", 8*3600)
	controller, store, _, rootDir := newTestController(t)
	ctx := context.Background()

	lib, series, onlineBook := seedBookFixture(t, store, rootDir, "Library A", "Series", "03.cbz", 10)
	offlineBook := seedBookInSeries(t, store, series, lib.ID, "05.cbz", 10)
	user := mkTestUser(t, store, "reader", "regular")

	// 在线路径写的是 time.Now()，其 Location 恒为 time.Local；换成同时区的定点时刻以去掉不确定性。
	onlineAt := time.Date(2030, 10, 1, 6, 0, 0, 0, time.Local)
	if err := store.SetUserBookProgress(ctx, user.ID, onlineBook.ID, 10, onlineAt); err != nil {
		t.Fatalf("SetUserBookProgress: %v", err)
	}

	bulkSyncOfflineItem(t, controller, user, offlineBook.ID, 10, "2030-09-30T23:00:00Z")

	items, err := store.GetUserRecentReadAll(ctx, user.ID, 10)
	if err != nil {
		t.Fatalf("GetUserRecentReadAll: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("看板「继续阅读」为空")
	}
	if items[0].BookID != offlineBook.ID {
		t.Fatalf("「继续阅读」指向 book_id=%d，应为离线补传的 %d", items[0].BookID, offlineBook.ID)
	}

	recent, err := store.GetUserRecentReadSeries(ctx, user.ID, lib.ID, 10)
	if err != nil {
		t.Fatalf("GetUserRecentReadSeries: %v", err)
	}
	if len(recent) == 0 || recent[0].RecentBookID != offlineBook.ID {
		t.Fatalf("库内「最近阅读的系列」取到的书不是离线补传的 %d：%+v", offlineBook.ID, recent)
	}
}

// TestBulkSyncProgressUTCTimestampKeepsMonthlyReviewWhole：同一次阅读的「翻过的页」与「读完的书」
// 必须落在同一个月。UTC+8 下本地 2030-10-01 07:00 读完，活动日期是 10-01，
// 若 last_read_at 按 UTC 墙钟落库，前缀变成 2030-09-30，回顾就把这次阅读劈成两个月。
func TestBulkSyncProgressUTCTimestampKeepsMonthlyReviewWhole(t *testing.T) {
	pinLocalZone(t, "CST", 8*3600)
	controller, store, _, rootDir := newTestController(t)
	ctx := context.Background()

	_, _, book := seedBookFixture(t, store, rootDir, "Library A", "Series", "05.cbz", 10)
	user := mkTestUser(t, store, "reader", "regular")

	readAt := time.Date(2030, 10, 1, 7, 0, 0, 0, time.Local)
	// 活动日期按本地日历日落库（database.ActivityDayKey），这里直接补上那一天的活动行；
	// 真实写入点取的是「今天」，用例要的是一个固定的、与 last_read_at 同一瞬间的日期。
	if _, err := store.(*database.SqlStore).DB().ExecContext(ctx,
		`INSERT INTO user_reading_activity (user_id, book_id, date, pages_read) VALUES (?, ?, ?, ?)`,
		user.ID, book.ID, database.ActivityDayKey(readAt), 10); err != nil {
		t.Fatalf("insert user_reading_activity: %v", err)
	}

	bulkSyncOfflineItem(t, controller, user, book.ID, 10, "2030-09-30T23:00:00Z")

	october, err := store.GetUserPeriodStats(ctx, user.ID, 2030, 10)
	if err != nil {
		t.Fatalf("GetUserPeriodStats(10): %v", err)
	}
	if october.Pages != 10 || october.BooksCompleted != 1 {
		t.Fatalf("10 月回顾 pages=%d books_completed=%d，应为 10 / 1", october.Pages, october.BooksCompleted)
	}
	september, err := store.GetUserPeriodStats(ctx, user.ID, 2030, 9)
	if err != nil {
		t.Fatalf("GetUserPeriodStats(9): %v", err)
	}
	if september.BooksCompleted != 0 {
		t.Fatalf("9 月回顾 books_completed=%d，应为 0（这次阅读整个属于 10 月）", september.BooksCompleted)
	}
}
