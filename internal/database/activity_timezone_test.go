// 本文件守卫「阅读活动的日期与 last_read_at 用同一套日历」。
//
// 活动日期与 last_read_at 必须用同一套日历：last_read_at 落库是本地墙钟串，活动日期不得用
// SQLite 的 DATE('now')（那产出的是 UTC 日历日）。两套日历在跨零点的时段会分裂：UTC+8 时区、
// 本地 2026-01-01 07:00 读完一本书，活动记成 2025-12-31、last_read_at 前缀是 2026-01-01——
// 年度回顾里「读完的书」算进 2026、「翻过的页」算进 2025，同一次阅读被劈成两年。

package database

import (
	"context"
	"testing"
	"time"
)

// TestActivityDayKeyUsesLocalCalendar 是最直接的判据：
// 活动日期必须与本地日历日一致，而不是 UTC 日历日。
//
// 用例只在两者确实不同的时刻才有区分力（同一瞬间的 UTC 日与本地日在一天里大部分时候相同），
// 所以另用一个构造出来的时刻兜住恒等的情况。
func TestActivityDayKeyUsesLocalCalendar(t *testing.T) {
	// 本地时间 2026-01-01 07:00，在 UTC+8 下对应 UTC 的 2025-12-31 23:00。
	local := time.Date(2026, 1, 1, 7, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if got := ActivityDayKey(local); got != "2026-01-01" {
		t.Fatalf("ActivityDayKey = %q, want 2026-01-01（本地日历日）", got)
	}
	if utcDay := local.UTC().Format("2006-01-02"); utcDay == "2026-01-01" {
		t.Fatal("用例前提不成立：该时刻的 UTC 日与本地日相同，区分不出两套日历")
	}
}

// TestReadingActivityDateMatchesLastReadAt 走真实写入路径，
// 断言活动日期与 last_read_at 的日期前缀落在同一天。
//
// 用例刻意把进程时区改成一个「本地日必然 ≠ UTC 日」的偏移，否则一天里大部分时刻两者相同，
// 断言恒真、退回 DATE('now') 也不会报红。time.Local 是进程级变量，本包测试默认串行执行，
// 用 t.Cleanup 还原即可。
func TestReadingActivityDateMatchesLastReadAt(t *testing.T) {
	original := time.Local
	t.Cleanup(func() { time.Local = original })
	// 按当前 UTC 小时挑一个必然跨日的偏移：UTC 上午往前推 13 小时落到昨天，下午往后推落到明天。
	offset := 13 * 3600
	if time.Now().UTC().Hour() < 12 {
		offset = -13 * 3600
	}
	time.Local = time.FixedZone("TEST", offset)
	if time.Now().Format("2006-01-02") == time.Now().UTC().Format("2006-01-02") {
		t.Fatal("构造的时区没能让本地日与 UTC 日分开，用例失去区分力")
	}

	store := newExecTxTestStore(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "tz", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{
		LibraryID: lib.ID, Name: "S", Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("UpsertSeriesByPath: %v", err)
	}
	res, err := store.DB().ExecContext(ctx,
		`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
		 VALUES (?, ?, 'v1', '/lib/v1.cbz', 0, CURRENT_TIMESTAMP, 10)`, series.ID, lib.ID)
	if err != nil {
		t.Fatalf("insert book: %v", err)
	}
	bookID, _ := res.LastInsertId()

	user, err := store.CreateUser(ctx, CreateUserParams{
		Username: "reader", PasswordHash: "x", Role: "regular", DisplayName: "R",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	now := time.Now()
	if err := store.SetUserBookProgress(ctx, user.ID, bookID, 5, now); err != nil {
		t.Fatalf("SetUserBookProgress: %v", err)
	}
	if err := store.LogUserReadingActivity(ctx, user.ID, bookID, 5); err != nil {
		t.Fatalf("LogUserReadingActivity: %v", err)
	}

	var activityDate string
	if err := store.DB().QueryRowContext(ctx,
		`SELECT date FROM user_reading_activity WHERE user_id = ? AND book_id = ?`,
		user.ID, bookID).Scan(&activityDate); err != nil {
		t.Fatalf("query activity: %v", err)
	}

	wantDay := ActivityDayKey(now)
	if activityDate != wantDay {
		t.Fatalf("活动日期 = %q, last_read_at 所在的本地日 = %q —— 两套日历又分裂了", activityDate, wantDay)
	}
}

// TestHeatmapSinceDateParsesOffsetClause 覆盖热力图窗口下界的换算。
func TestHeatmapSinceDateParsesOffsetClause(t *testing.T) {
	now := time.Now()
	cases := map[string]string{
		"-7 days":   ActivityDayKeyBefore(now, 7),
		"-112 days": ActivityDayKeyBefore(now, 112),
		"":          ActivityDayKeyBefore(now, 112), // 解析失败时退回默认窗口
		"garbage":   ActivityDayKeyBefore(now, 112),
	}
	for clause, want := range cases {
		if got := HeatmapSinceDate(clause); got != want {
			t.Errorf("HeatmapSinceDate(%q) = %q, want %q", clause, got, want)
		}
	}
}
