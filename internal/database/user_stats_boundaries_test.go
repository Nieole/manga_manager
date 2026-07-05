// 业务说明：本文件是业务回归测试，覆盖深度统计(user_stats.go)的边界：连续阅读天数在单日/仅昨日/
// 重复日期/历史最长 run/空数据等边界下的 current 与 longest 计算，以及年度回顾 vs 月度回顾的时间分桶
// （strftime 年/月前缀），确保未来对 streak Go 侧算法或分桶 SQL 的改动不会悄悄改变统计口径。

package database

import (
	"context"
	"testing"
)

// insertActivity 直接写 user_reading_activity（date 为 'YYYY-MM-DD'）。
func insertActivity(t *testing.T, ctx context.Context, store Store, userID, bookID int64, date string, pages int) {
	t.Helper()
	if _, err := store.(*SqlStore).db.ExecContext(ctx,
		`INSERT INTO user_reading_activity (user_id, book_id, date, pages_read) VALUES (?, ?, ?, ?)`,
		userID, bookID, date, pages); err != nil {
		t.Fatalf("insert activity (%d,%s): %v", userID, date, err)
	}
}

func TestUserReadingStreakBoundaries(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, _, book1, book2 := seedUserProgressFixture(t, store)

	assertStreak := func(userID int64, wantCur, wantLong int) {
		t.Helper()
		cur, long, err := store.GetUserReadingStreak(ctx, userID)
		if err != nil {
			t.Fatalf("streak user %d: %v", userID, err)
		}
		if cur != wantCur || long != wantLong {
			t.Fatalf("user %d streak = (cur=%d long=%d) want (%d %d)", userID, cur, long, wantCur, wantLong)
		}
	}

	// 无任何活动：0/0。
	uEmpty := mkUser(t, ctx, store, "empty", RoleRegular)
	assertStreak(uEmpty, 0, 0)

	// 仅今天：1/1。
	uToday := mkUser(t, ctx, store, "today", RoleRegular)
	insertActivity(t, ctx, store, uToday, book1, dayStr(0), 5)
	assertStreak(uToday, 1, 1)

	// 仅昨天（末次活动=昨天，gap=24h 视为未中断）：current=1。
	uYesterday := mkUser(t, ctx, store, "yesterday", RoleRegular)
	insertActivity(t, ctx, store, uYesterday, book1, dayStr(-1), 5)
	assertStreak(uYesterday, 1, 1)

	// 重复日期（今天两本 + 昨天一本）：DISTINCT date 折叠，不虚增 → 2/2。
	uDup := mkUser(t, ctx, store, "dup", RoleRegular)
	insertActivity(t, ctx, store, uDup, book1, dayStr(0), 5)
	insertActivity(t, ctx, store, uDup, book2, dayStr(0), 3)
	insertActivity(t, ctx, store, uDup, book1, dayStr(-1), 4)
	assertStreak(uDup, 2, 2)

	// 历史最长 run(4 天，久远) + 当前 run(今天+昨天=2 天)：longest 取历史 4，current=2。
	uHist := mkUser(t, ctx, store, "hist", RoleRegular)
	for _, off := range []int{-20, -19, -18, -17} {
		insertActivity(t, ctx, store, uHist, book1, dayStr(off), 5)
	}
	insertActivity(t, ctx, store, uHist, book1, dayStr(-1), 5)
	insertActivity(t, ctx, store, uHist, book2, dayStr(0), 5)
	assertStreak(uHist, 2, 4)
}

// TestUserPeriodStatsYearVsMonthBucketing 验证回顾按年(month=0)与按月分桶互不串期：
// 页数/活跃天数/涉及书目仅统计目标期，跨月与跨年的活动被正确排除或并入。
func TestUserPeriodStatsYearVsMonthBucketing(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, _, book1, book2 := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)

	// 2025-01：10 页；2026-03：20+5 页(两本，两天)；2026-07：7 页。
	insertActivity(t, ctx, store, u, book1, "2025-01-15", 10)
	insertActivity(t, ctx, store, u, book1, "2026-03-10", 20)
	insertActivity(t, ctx, store, u, book2, "2026-03-20", 5)
	insertActivity(t, ctx, store, u, book1, "2026-07-02", 7)

	// 按月 2026-03：仅该月两天、两本、25 页。
	mar, err := store.GetUserPeriodStats(ctx, u, 2026, 3)
	if err != nil {
		t.Fatalf("march: %v", err)
	}
	if mar.Pages != 25 || mar.ActiveDays != 2 || mar.BooksTouched != 2 {
		t.Fatalf("2026-03 = pages=%d days=%d books=%d want 25/2/2", mar.Pages, mar.ActiveDays, mar.BooksTouched)
	}
	// 两本书同属一个系列 → TopSeries 一行、25 页。
	if len(mar.TopSeries) != 1 || mar.TopSeries[0].Pages != 25 {
		t.Fatalf("2026-03 top series = %+v want single 25", mar.TopSeries)
	}

	// 按年 2026：并入 3 月与 7 月 → 32 页、3 活跃天、2 本。
	y2026, err := store.GetUserPeriodStats(ctx, u, 2026, 0)
	if err != nil {
		t.Fatalf("2026: %v", err)
	}
	if y2026.Pages != 32 || y2026.ActiveDays != 3 || y2026.BooksTouched != 2 {
		t.Fatalf("2026 = pages=%d days=%d books=%d want 32/3/2", y2026.Pages, y2026.ActiveDays, y2026.BooksTouched)
	}

	// 按年 2025：仅 1 月 10 页、1 天。跨年隔离。
	y2025, err := store.GetUserPeriodStats(ctx, u, 2025, 0)
	if err != nil {
		t.Fatalf("2025: %v", err)
	}
	if y2025.Pages != 10 || y2025.ActiveDays != 1 {
		t.Fatalf("2025 = pages=%d days=%d want 10/1", y2025.Pages, y2025.ActiveDays)
	}

	// 空期(2026-12)：全 0。
	empty, err := store.GetUserPeriodStats(ctx, u, 2026, 12)
	if err != nil {
		t.Fatalf("empty period: %v", err)
	}
	if empty.Pages != 0 || empty.ActiveDays != 0 || empty.BooksTouched != 0 || len(empty.TopSeries) != 0 {
		t.Fatalf("2026-12 should be empty, got %+v", empty)
	}
}
