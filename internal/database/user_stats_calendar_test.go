// 本文件守「读取侧与写入侧同一套日历日」：连续天数的今天、回顾期的默认年月，都必须取本地日历日。
// 破了的话，本地日与 UTC 日错开的那几个小时里，用户的「当前连续」会掉成 0、跨年时回顾会退到去年。
// 用例经 pinCalendar 拨钟并写死期望日期串，不从 time.Now() 推期望值——那正是旧用例与 bug 互相掩盖的原因。

package database

import (
	"testing"
	"time"
)

// pinCalendar 把日历口径的「现在」钉在 at（含其时区），用例因此与运行机器的时区和真实时刻无关。
// calendarNow 是包级变量，故依赖它的用例不得并行。
func pinCalendar(t *testing.T, at time.Time) {
	t.Helper()
	prev := calendarNow
	calendarNow = func() time.Time { return at }
	t.Cleanup(func() { calendarNow = prev })
}

func TestUserReadingStreakUsesLocalCalendarDay(t *testing.T) {
	cases := []struct {
		name string
		at   time.Time
		// 本地日历日的今天/昨天/前天，写死字面量。
		today, yesterday, dayBefore string
	}{
		{
			name:      "UTC+8 本地已跨月到 9-1、UTC 还停在 8-31",
			at:        time.Date(2026, 9, 1, 3, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
			today:     "2026-09-01",
			yesterday: "2026-08-31",
			dayBefore: "2026-08-30",
		},
		{
			name:      "UTC-5 本地还是 8-31、UTC 已跨到 9-1",
			at:        time.Date(2026, 8, 31, 21, 0, 0, 0, time.FixedZone("UTC-5", -5*3600)),
			today:     "2026-08-31",
			yesterday: "2026-08-30",
			dayBefore: "2026-08-29",
		},
		{
			name:      "UTC 本身，本地日与 UTC 日相同",
			at:        time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
			today:     "2026-09-01",
			yesterday: "2026-08-31",
			dayBefore: "2026-08-30",
		},
		{
			name:      "UTC+8 跨年：本地已是 2027 元旦、UTC 还停在 2026-12-31",
			at:        time.Date(2027, 1, 1, 5, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
			today:     "2027-01-01",
			yesterday: "2026-12-31",
			dayBefore: "2026-12-30",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinCalendar(t, tc.at)
			if got := TodayDayKey(); got != tc.today {
				t.Fatalf("用例前提不成立：TodayDayKey = %q, want %q", got, tc.today)
			}

			store := newStoreForTest(t)
			ctx, _, _, book1, _ := seedUserProgressFixture(t, store)

			assertStreak := func(label string, userID int64, wantCur, wantLong int) {
				t.Helper()
				cur, long, err := store.GetUserReadingStreak(ctx, userID)
				if err != nil {
					t.Fatalf("%s streak: %v", label, err)
				}
				if cur != wantCur || long != wantLong {
					t.Fatalf("%s streak = (cur=%d long=%d) want (%d %d)", label, cur, long, wantCur, wantLong)
				}
			}

			// 读到今天：三天连续，当前连续必须是 3。
			// 本地日领先 UTC 日时，按 UTC 取「今天」会算出 gap=-24h，把这里判成断档。
			uToday := mkUser(t, ctx, store, "today", RoleRegular)
			for _, d := range []string{tc.dayBefore, tc.yesterday, tc.today} {
				insertActivity(t, ctx, store, uToday, book1, d, 5)
			}
			assertStreak("读到今天", uToday, 3, 3)

			// 读到昨天、今天还没读：连续未中断，当前连续必须是 2。
			// 本地日落后 UTC 日时，按 UTC 取「今天」会算出 gap=48h，把这里判成断档。
			uYesterday := mkUser(t, ctx, store, "yesterday", RoleRegular)
			for _, d := range []string{tc.dayBefore, tc.yesterday} {
				insertActivity(t, ctx, store, uYesterday, book1, d, 5)
			}
			assertStreak("读到昨天", uYesterday, 2, 2)

			// 末次活动是前天：真断档，当前连续必须归 0（防止修正过头，把所有人都判成连续）。
			uStale := mkUser(t, ctx, store, "stale", RoleRegular)
			insertActivity(t, ctx, store, uStale, book1, tc.dayBefore, 5)
			assertStreak("断档", uStale, 0, 1)
		})
	}
}

// TestCurrentPeriodFollowsLocalCalendar 守回顾接口的默认期：调用方不指定年份时取的必须是本地的年月。
func TestCurrentPeriodFollowsLocalCalendar(t *testing.T) {
	cases := []struct {
		name        string
		at          time.Time
		year, month int
	}{
		{
			name:  "UTC+8 元旦凌晨：本地已是 2027 年，取 UTC 会退回 2026 年度回顾",
			at:    time.Date(2027, 1, 1, 5, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
			year:  2027,
			month: 1,
		},
		{
			name:  "UTC-5 除夕深夜：本地还在 2026 年，取 UTC 会提前跳到 2027 年度回顾",
			at:    time.Date(2026, 12, 31, 21, 0, 0, 0, time.FixedZone("UTC-5", -5*3600)),
			year:  2026,
			month: 12,
		},
		{
			name:  "UTC+8 月初凌晨：本地已是 9 月，取 UTC 会停在 8 月",
			at:    time.Date(2026, 9, 1, 3, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)),
			year:  2026,
			month: 9,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pinCalendar(t, tc.at)
			year, month := CurrentPeriod()
			if year != tc.year || month != tc.month {
				t.Fatalf("CurrentPeriod = (%d, %d) want (%d, %d)", year, month, tc.year, tc.month)
			}
		})
	}
}

// TestPeriodDayKeysBounds 守回顾期的左闭右开界，含跨年进位与闰年 2 月。
func TestPeriodDayKeysBounds(t *testing.T) {
	cases := []struct {
		name         string
		year, month  int
		lower, upper string
	}{
		{"整年（month=0）", 2026, 0, "2026-01-01", "2027-01-01"},
		{"跨年进位：12 月的上界是次年 1 月 1 日", 2026, 12, "2026-12-01", "2027-01-01"},
		{"闰年 2 月的上界是 3 月 1 日", 2028, 2, "2028-02-01", "2028-03-01"},
		{"月份非法（负数）按整年处理", 2026, -3, "2026-01-01", "2027-01-01"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lower, upper := PeriodDayKeys(tc.year, tc.month)
			if lower != tc.lower || upper != tc.upper {
				t.Fatalf("PeriodDayKeys(%d, %d) = (%q, %q) want (%q, %q)", tc.year, tc.month, lower, upper, tc.lower, tc.upper)
			}
		})
	}
}

// TestUserReadingActivityWriteReadUseSameDay 走真实写入路径，断言写进去的活动
// 在同一时刻的「今天」口径下读得回来——写入按本地日、读取按 UTC 日时这里会一起塌成 0。
func TestUserReadingActivityWriteReadUseSameDay(t *testing.T) {
	// 本地 2026-09-01 凌晨，UTC 还是 2026-08-31：月份都跨了，是口径分裂最刺眼的时刻。
	pinCalendar(t, time.Date(2026, 9, 1, 3, 0, 0, 0, time.FixedZone("UTC+8", 8*3600)))

	store := newStoreForTest(t)
	ctx, _, _, book1, _ := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)

	if err := store.LogUserReadingActivity(ctx, u, book1, 7); err != nil {
		t.Fatalf("LogUserReadingActivity: %v", err)
	}
	if err := store.AddUserBookReadingTime(ctx, u, book1, 350); err != nil {
		t.Fatalf("AddUserBookReadingTime: %v", err)
	}

	// 当前连续：今天刚读过 → 1 天。
	cur, long, err := store.GetUserReadingStreak(ctx, u)
	if err != nil {
		t.Fatalf("streak: %v", err)
	}
	if cur != 1 || long != 1 {
		t.Fatalf("streak = (cur=%d long=%d) want (1 1)", cur, long)
	}

	// 本月回顾（默认期）：页数与时长都必须落在 2026-09。
	year, month := CurrentPeriod()
	stats, err := store.GetUserPeriodStats(ctx, u, year, month)
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if stats.Pages != 7 || stats.ReadSeconds != 350 || stats.ActiveDays != 1 {
		t.Fatalf("2026-09 回顾 = pages=%d seconds=%d days=%d want 7/350/1", stats.Pages, stats.ReadSeconds, stats.ActiveDays)
	}

	// 上个月（UTC 那一侧的 8 月）必须是空的：活动属于本地的 9 月。
	prev, err := store.GetUserPeriodStats(ctx, u, 2026, 8)
	if err != nil {
		t.Fatalf("prev period: %v", err)
	}
	if prev.Pages != 0 || prev.ReadSeconds != 0 {
		t.Fatalf("2026-08 应为空，got pages=%d seconds=%d", prev.Pages, prev.ReadSeconds)
	}

	// 热力图窗口下界也必须按本地日算："-7 days" 即本地今天减 7 天。
	if got := HeatmapSinceDate("-7 days"); got != "2026-08-25" {
		t.Fatalf("HeatmapSinceDate(-7 days) = %q want 2026-08-25", got)
	}
}
