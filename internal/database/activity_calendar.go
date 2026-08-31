// 阅读活动的日历口径：本系统的「一天」一律是**服务器 time.Local 的日历日**。
// 写入（活动日期）与读取（连续天数的今天、回顾期的年月界、近 N 天窗口的下界）都必须经本文件取日期；
// 任何一处自行取 UTC 日历日，跨零点的那几个小时里两侧就会错开一天。

package database

import (
	"strconv"
	"strings"
	"time"
)

// dayKeyLayout 是活动日期在库内的存储格式。零填充，故字典序即时间序，可直接做区间比较。
const dayKeyLayout = "2006-01-02"

// calendarNow 是「现在」进入日历口径的唯一入口。
//
// 抽成变量是为了让测试把时钟拨到任意时区与任意时刻（日界两侧、跨月、跨年），
// 从而不必用 time.Now() 造期望值——测试与被测代码共用一套时间来源时，口径错了也是一起错，互相掩盖。
var calendarNow = time.Now

// ActivityDayKey 返回 t 所在的**本地**日历日（YYYY-MM-DD）。
//
// 活动日期须取本地日历日，不用 SQLite 的 DATE('now')（那是 UTC 日历日）：这里的 date 列要与
// last_read_at（本地墙钟串）同口径，否则跨时区场景下年度回顾里「读完的书」与「翻过的页」
// 会被计入不同年份，同一次阅读被劈成两截。用户看到的「今天读了多少」本来就该按他所在的那一天算。
//
// 「本地」指服务器进程的 time.Local。date 列只存一个不带时区的日历日，账号上也没有时区设置，
// 因此用户与服务器不在同一时区时，跨零点的那几小时按服务器的那一天记账——这是当前设计的取舍。
// 要按用户所在时区记账，得先给账号加时区并让写入侧带上它，那是另一件事。
func ActivityDayKey(t time.Time) string {
	return t.Format(dayKeyLayout)
}

// ActivityDayKeyBefore 返回距 t 若干天之前的本地日历日，供「近 N 天」这类范围查询做下界。
func ActivityDayKeyBefore(t time.Time, days int) string {
	return ActivityDayKey(t.AddDate(0, 0, -days))
}

// TodayDayKey 返回服务器本地的今天，是所有读取点判断「今天」的唯一来源。
func TodayDayKey() string {
	return ActivityDayKey(calendarNow())
}

// DayKeyDaysAgo 返回距今 days 天前的本地日历日，供「近 N 天」窗口取下界。
func DayKeyDaysAgo(days int) string {
	return ActivityDayKeyBefore(calendarNow(), days)
}

// ParseDayKey 把 'YYYY-MM-DD' 解析成可做日期算术的日历日值；解析失败时 ok=false。
//
// 值一律挂在 UTC 上：这里只当纯日历坐标用，相邻两日恒差 24h。若挂本地时区，
// 夏令时切换那两天会差 23h/25h，连续天数会在那里凭空断掉。
func ParseDayKey(key string) (time.Time, bool) {
	t, err := time.Parse(dayKeyLayout, key)
	return t, err == nil
}

// CurrentPeriod 返回服务器本地的当前年月，供回顾接口在调用方没指定时取默认期。
func CurrentPeriod() (year, month int) {
	now := calendarNow()
	return now.Year(), int(now.Month())
}

// PeriodDayKeys 返回回顾期的左闭右开日期界 [lower, upper)；month<=0 表示整年。
//
// 界是纯日历日字面量（time.UTC 只是做日期加法的坐标原点，不代表口径），
// 与 date 列、last_read_at 前缀同为本地日历日，故可直接字符串比较并走索引区间扫描。
func PeriodDayKeys(year, month int) (lower, upper string) {
	from := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(1, 0, 0)
	if month > 0 {
		from = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		to = from.AddDate(0, 1, 0)
	}
	return from.Format(dayKeyLayout), to.Format(dayKeyLayout)
}

// HeatmapSinceDate 把既有的 offsetClause（形如 "-112 days"）换算成本地日历日下界。
//
// 保留字符串入参是为了不改调用方签名；解析失败时退回 112 天，与前端热力图的默认窗口一致。
// 之所以不继续把它交给 SQLite 的 DATE('now', ?)，是因为那样下界按 UTC 算、
// 而表里的 date 是本地日历日，跨时区部署下窗口会整体错开一天。
func HeatmapSinceDate(offsetClause string) string {
	days := 112
	fields := strings.Fields(strings.TrimSpace(offsetClause))
	if len(fields) > 0 {
		if n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "-")); err == nil && n > 0 {
			days = n
		}
	}
	return DayKeyDaysAgo(days)
}
