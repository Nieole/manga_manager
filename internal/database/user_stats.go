// 业务说明：本文件是「深度统计」（第 6 项）的数据访问层：每用户每日活动、连续阅读天数、每本阅读时长、
// 年度/月度回顾聚合，以及每用户系列短评的 CRUD。全部按用户维度（多用户），沿用手写 SqlStore 方法 + Store 接口范式。
// 维护要点：活动写入与旧全局 reading_activity 双写（旧数据在首个管理员创建时迁入其名下）；连续天数在 Go 侧计算。

package database

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

// BookReadingTimeRow 是「每本阅读时长」排行的一行。
type BookReadingTimeRow struct {
	BookID       int64  `json:"book_id"`
	BookName     string `json:"book_name"`
	BookTitle    string `json:"book_title"`
	SeriesID     int64  `json:"series_id"`
	SeriesName   string `json:"series_name"`
	TotalSeconds int64  `json:"total_seconds"`
}

// PeriodTopSeries 是回顾期内阅读页数最多的系列。
type PeriodTopSeries struct {
	SeriesID   int64  `json:"series_id"`
	SeriesName string `json:"series_name"`
	Pages      int64  `json:"pages"`
}

// UserPeriodStats 是某用户在某年（month=0）或某月的阅读回顾聚合。
type UserPeriodStats struct {
	Pages          int64             `json:"pages"`
	ReadSeconds    int64             `json:"read_seconds"`
	ActiveDays     int64             `json:"active_days"`
	BooksTouched   int64             `json:"books_touched"`
	BooksCompleted int64             `json:"books_completed"`
	TopSeries      []PeriodTopSeries `json:"top_series"`
}

// UserSeriesReview 是某用户对某系列的个人评分 + 短评。
type UserSeriesReview struct {
	SeriesID  int64           `json:"series_id"`
	Rating    sql.NullFloat64 `json:"rating"`
	Review    string          `json:"review"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// ---- 每用户每日活动 ----

// ActivityDayKey 返回 t 所在的**本地**日历日（YYYY-MM-DD）。
//
// 活动日期此前用 SQLite 的 DATE('now')，那是 UTC 日历日；而 last_read_at 落库时是本地墙钟串。
// 于是同一次阅读在两套日历下分属不同的一天——UTC+8 时区、本地 2026-01-01 07:00 读完一本书，
// 活动记成 2025-12-31 而 last_read_at 前缀是 2026-01-01，年度回顾里「读完的书」算进 2026、
// 「翻过的页」算进 2025，同一次阅读被劈成两年。
//
// 统一到服务器本地时区：用户看到的「今天读了多少」本来就该按他所在的那一天算。
func ActivityDayKey(t time.Time) string {
	return t.Format("2006-01-02")
}

// ActivityDayKeyBefore 返回距 t 若干天之前的本地日历日，供「近 N 天」这类范围查询做下界。
func ActivityDayKeyBefore(t time.Time, days int) string {
	return ActivityDayKey(t.AddDate(0, 0, -days))
}

// LogUserReadingActivity 记录某用户当天在某书翻读的页数（pages_read 取 MAX，语义同旧全局表）。
func (s *SqlStore) LogUserReadingActivity(ctx context.Context, userID, bookID, pages int64) error {
	// 日期在 Go 侧按本地时区算好再传参，不用 DATE('now')（那是 UTC，见 ActivityDayKey 的注释）。
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_reading_activity (user_id, book_id, date, pages_read)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(user_id, book_id, date) DO UPDATE SET
		   pages_read = MAX(user_reading_activity.pages_read, excluded.pages_read)`,
		userID, bookID, ActivityDayKey(time.Now()), pages)
	return err
}

// GetUserActivityHeatmap 返回某用户近期每日阅读页数（offsetClause 形如 "-112 days"）。
func (s *SqlStore) GetUserActivityHeatmap(ctx context.Context, userID int64, offsetClause string) ([]ActivityDay, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT date, SUM(pages_read) AS page_count
		 FROM user_reading_activity
		 WHERE user_id = ? AND date >= ?
		 GROUP BY date ORDER BY date ASC`, userID, HeatmapSinceDate(offsetClause))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActivityDay
	for rows.Next() {
		var d ActivityDay
		if err := rows.Scan(&d.Date, &d.PageCount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetUserReadingStreak 返回某用户的当前连续阅读天数与历史最长连续天数（基于有活动的日期）。
// 当前连续天数：从今天或昨天起向前连续的天数（昨天有读、今天还没读，连续未中断）。
func (s *SqlStore) GetUserReadingStreak(ctx context.Context, userID int64) (current, longest int, err error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT date FROM user_reading_activity WHERE user_id = ? AND date != '' ORDER BY date ASC`, userID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	var days []time.Time
	for rows.Next() {
		var ds string
		if err := rows.Scan(&ds); err != nil {
			return 0, 0, err
		}
		if t, e := time.Parse("2006-01-02", ds); e == nil {
			days = append(days, t)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(days) == 0 {
		return 0, 0, nil
	}

	// 最长连续：相邻日期差 1 天则延续。
	longest = 1
	run := 1
	for i := 1; i < len(days); i++ {
		if days[i].Sub(days[i-1]) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		if run > longest {
			longest = run
		}
	}

	// 当前连续：末次活动须是今天或昨天，否则连续为 0。
	today := time.Now().UTC().Truncate(24 * time.Hour)
	last := days[len(days)-1]
	gap := today.Sub(last)
	if gap != 0 && gap != 24*time.Hour {
		return 0, longest, nil
	}
	current = 1
	for i := len(days) - 1; i > 0; i-- {
		if days[i].Sub(days[i-1]) == 24*time.Hour {
			current++
		} else {
			break
		}
	}
	return current, longest, nil
}

// ---- 每本阅读时长 ----

// AddUserBookReadingTime 累加某用户某书的活跃阅读秒数，并同步累加到当天的 user_reading_activity（供回顾按期统计时长）。
func (s *SqlStore) AddUserBookReadingTime(ctx context.Context, userID, bookID, seconds int64) error {
	if seconds <= 0 {
		return nil
	}
	return s.ExecTx(ctx, func(q *Queries) error {
		if _, err := q.db.ExecContext(ctx,
			`INSERT INTO user_book_reading_time (user_id, book_id, total_seconds, updated_at)
			 VALUES (?, ?, ?, CURRENT_TIMESTAMP)
			 ON CONFLICT(user_id, book_id) DO UPDATE SET
			   total_seconds = total_seconds + excluded.total_seconds, updated_at = CURRENT_TIMESTAMP`,
			userID, bookID, seconds); err != nil {
			return err
		}
		_, err := q.db.ExecContext(ctx,
			`INSERT INTO user_reading_activity (user_id, book_id, date, pages_read, read_seconds)
			 VALUES (?, ?, ?, 0, ?)
			 ON CONFLICT(user_id, book_id, date) DO UPDATE SET
			   read_seconds = read_seconds + excluded.read_seconds`,
			userID, bookID, ActivityDayKey(time.Now()), seconds)
		return err
	})
}

// GetUserTotalReadingTime 返回某用户累计的活跃阅读秒数。
func (s *SqlStore) GetUserTotalReadingTime(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(total_seconds), 0) FROM user_book_reading_time WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// GetUserBookReadingTimeTop 返回某用户阅读时长最多的若干本书。
func (s *SqlStore) GetUserBookReadingTimeTop(ctx context.Context, userID int64, limit int) ([]BookReadingTimeRow, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT rt.book_id, b.name, COALESCE(b.title, ''), b.series_id,
		        COALESCE(NULLIF(s.title, ''), s.name), rt.total_seconds
		 FROM user_book_reading_time rt
		 JOIN books b ON b.id = rt.book_id
		 JOIN series s ON s.id = b.series_id
		 WHERE rt.user_id = ? AND rt.total_seconds > 0
		 ORDER BY rt.total_seconds DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BookReadingTimeRow
	for rows.Next() {
		var r BookReadingTimeRow
		if err := rows.Scan(&r.BookID, &r.BookName, &r.BookTitle, &r.SeriesID, &r.SeriesName, &r.TotalSeconds); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---- 年度 / 月度回顾 ----

// GetUserPeriodStats 聚合某用户在指定年（month=0）或年月的阅读回顾。
func (s *SqlStore) GetUserPeriodStats(ctx context.Context, userID int64, year, month int) (UserPeriodStats, error) {
	var stats UserPeriodStats
	// 期间匹配改用左闭右开的日期区间 [lowerBound, upperBound)，取代 strftime()/substr() 包裹列。
	// date 列是零填充的 'YYYY-MM-DD'（字典序即时间序）；last_read_at 由 Go time.Time 经 modernc 驱动以
	// t.String() 落库（"2026-07-05 22:31:16... +0800 CST"，strftime/date 无法解析），但其前缀恒为
	// "YYYY-MM-DD"，故对二者做字符串区间比较等价于原先的前缀相等，且能让 (user_id, date) /
	// (user_id, last_read_at) 复合索引做 range-scan，而非 seek 到 user_id 后全历史逐行求值函数。
	lower := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	upper := lower.AddDate(1, 0, 0)
	if month > 0 {
		lower = time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
		upper = lower.AddDate(0, 1, 0)
	}
	lowerBound := lower.Format("2006-01-02")
	upperBound := upper.Format("2006-01-02")

	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(pages_read),0), COALESCE(SUM(read_seconds),0),
		        COUNT(DISTINCT date), COUNT(DISTINCT book_id)
		 FROM user_reading_activity
		 WHERE user_id = ? AND date >= ? AND date < ?`,
		userID, lowerBound, upperBound).Scan(&stats.Pages, &stats.ReadSeconds, &stats.ActiveDays, &stats.BooksTouched)
	if err != nil {
		return UserPeriodStats{}, err
	}

	// 期间读完的书：以 last_read_at 落在该期、且进度到达页数为准。区间比较利用其 'YYYY-MM-DD' 前缀，
	// 与上面 date 同口径（本地墙钟日期），可走 idx_user_book_progress_user_time(user_id, last_read_at)。
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM user_book_progress ubp JOIN books b ON b.id = ubp.book_id
		 WHERE ubp.user_id = ? AND b.page_count > 0 AND ubp.last_read_page >= b.page_count
		   AND ubp.last_read_at IS NOT NULL AND ubp.last_read_at >= ? AND ubp.last_read_at < ?`,
		userID, lowerBound, upperBound).Scan(&stats.BooksCompleted); err != nil {
		return UserPeriodStats{}, err
	}

	// 期间阅读页数最多的系列 Top 5。
	rows, err := s.db.QueryContext(ctx,
		`SELECT b.series_id, COALESCE(NULLIF(s.title, ''), s.name) AS series_name, SUM(ura.pages_read) AS pages
		 FROM user_reading_activity ura
		 JOIN books b ON b.id = ura.book_id
		 JOIN series s ON s.id = b.series_id
		 WHERE ura.user_id = ? AND ura.date >= ? AND ura.date < ?
		 GROUP BY b.series_id ORDER BY pages DESC LIMIT 5`,
		userID, lowerBound, upperBound)
	if err != nil {
		return UserPeriodStats{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var t PeriodTopSeries
		if err := rows.Scan(&t.SeriesID, &t.SeriesName, &t.Pages); err != nil {
			return UserPeriodStats{}, err
		}
		stats.TopSeries = append(stats.TopSeries, t)
	}
	return stats, rows.Err()
}

// ---- 每用户系列短评 ----

// UpsertUserSeriesReview 写入/更新某用户对某系列的评分与短评。rating 为 nil 表示不评分。
func (s *SqlStore) UpsertUserSeriesReview(ctx context.Context, userID, seriesID int64, rating *float64, review string) error {
	var ratingVal sql.NullFloat64
	if rating != nil {
		ratingVal = sql.NullFloat64{Float64: *rating, Valid: true}
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_series_review (user_id, series_id, rating, review, updated_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(user_id, series_id) DO UPDATE SET
		   rating = excluded.rating, review = excluded.review, updated_at = CURRENT_TIMESTAMP`,
		userID, seriesID, ratingVal, review)
	return err
}

// GetUserSeriesReview 返回某用户对某系列的短评；无则 found=false。
func (s *SqlStore) GetUserSeriesReview(ctx context.Context, userID, seriesID int64) (UserSeriesReview, bool, error) {
	var rv UserSeriesReview
	err := s.db.QueryRowContext(ctx,
		`SELECT series_id, rating, review, updated_at FROM user_series_review WHERE user_id = ? AND series_id = ?`,
		userID, seriesID).Scan(&rv.SeriesID, &rv.Rating, &rv.Review, &rv.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return UserSeriesReview{}, false, nil
	}
	if err != nil {
		return UserSeriesReview{}, false, err
	}
	return rv, true, nil
}

// DeleteUserSeriesReview 删除某用户对某系列的短评。
func (s *SqlStore) DeleteUserSeriesReview(ctx context.Context, userID, seriesID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM user_series_review WHERE user_id = ? AND series_id = ?`, userID, seriesID)
	return err
}

// ---- 迁移 ----

// MigrateGlobalActivityToUser 把旧的全局 reading_activity 一次性迁入某用户名下（幂等，首个管理员创建时调用）。
func (s *SqlStore) MigrateGlobalActivityToUser(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO user_reading_activity (user_id, book_id, date, pages_read)
		 SELECT ?, book_id, date, pages_read FROM reading_activity`, userID)
	return err
}

// GetUserTopReadingTags 是 GetTopReadingTags 的每用户版本。
//
// sqlc 版基于全局 reading_activity 聚合，多用户下所有人拿到同一份「常看标签」，
// AI 推荐因此完全没有个人化，还会把彼此的阅读偏好互相泄露。
// user_reading_activity 位于 schema_handwritten.sql（避开 sqlc），故手写于此。
func (s *SqlStore) GetUserTopReadingTags(ctx context.Context, userID, limit int64) ([]GetTopReadingTagsRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT t.name, COUNT(*) AS tag_count
		FROM tags t
		JOIN series_tags st ON t.id = st.tag_id
		JOIN books b ON st.series_id = b.series_id
		JOIN user_reading_activity ura ON b.id = ura.book_id
		WHERE ura.user_id = ?
		GROUP BY t.id
		ORDER BY tag_count DESC
		LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []GetTopReadingTagsRow
	for rows.Next() {
		var i GetTopReadingTagsRow
		if err := rows.Scan(&i.Name, &i.TagCount); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

// HeatmapSinceDate 把既有的 offsetClause（形如 "-112 days"）换算成本地日历日下界。
//
// 保留字符串入参是为了不改调用方签名；解析失败时退回 112 天，与前端热力图的默认窗口一致。
// 之所以不继续把它交给 SQLite 的 DATE('now', ?)，是因为那样下界按 UTC 算、
// 而表里的 date 现在是本地日历日，跨时区部署下窗口会整体错开一天。
func HeatmapSinceDate(offsetClause string) string {
	days := 112
	fields := strings.Fields(strings.TrimSpace(offsetClause))
	if len(fields) > 0 {
		if n, err := strconv.Atoi(strings.TrimPrefix(fields[0], "-")); err == nil && n > 0 {
			days = n
		}
	}
	return ActivityDayKeyBefore(time.Now(), days)
}
