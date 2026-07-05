package database

import (
	"testing"
	"time"
)

func dayStr(offset int) string {
	return time.Now().UTC().AddDate(0, 0, offset).Format("2006-01-02")
}

func TestUserReadingStreak(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, _, book1, _ := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)
	db := store.(*SqlStore).db

	insert := func(date string, pages int) {
		t.Helper()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO user_reading_activity (user_id, book_id, date, pages_read) VALUES (?, ?, ?, ?)`,
			u, book1, date, pages); err != nil {
			t.Fatalf("insert activity %s: %v", date, err)
		}
	}
	// 连续 3 天到今天；另有更早的孤立 2 天。
	insert(dayStr(0), 5)
	insert(dayStr(-1), 5)
	insert(dayStr(-2), 5)
	insert(dayStr(-10), 5)
	insert(dayStr(-11), 5)

	current, longest, err := store.GetUserReadingStreak(ctx, u)
	if err != nil {
		t.Fatalf("streak: %v", err)
	}
	if current != 3 {
		t.Fatalf("current streak want 3 got %d", current)
	}
	if longest != 3 {
		t.Fatalf("longest streak want 3 got %d", longest)
	}

	// 断档用户：末次活动是前天 → 当前连续为 0，最长仍为其历史。
	u2 := mkUser(t, ctx, store, "bob", RoleRegular)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO user_reading_activity (user_id, book_id, date, pages_read) VALUES (?,?,?,5),(?,?,?,5)`,
		u2, book1, dayStr(-2), u2, book1, dayStr(-3)); err != nil {
		t.Fatalf("insert u2 activity: %v", err)
	}
	cur2, long2, _ := store.GetUserReadingStreak(ctx, u2)
	if cur2 != 0 {
		t.Fatalf("u2 current streak want 0 got %d", cur2)
	}
	if long2 != 2 {
		t.Fatalf("u2 longest streak want 2 got %d", long2)
	}
}

func TestUserBookReadingTime(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, _, book1, book2 := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)

	if err := store.AddUserBookReadingTime(ctx, u, book1, 100); err != nil {
		t.Fatalf("add time1: %v", err)
	}
	if err := store.AddUserBookReadingTime(ctx, u, book1, 50); err != nil {
		t.Fatalf("add time2: %v", err)
	}
	if err := store.AddUserBookReadingTime(ctx, u, book2, 200); err != nil {
		t.Fatalf("add time3: %v", err)
	}

	total, err := store.GetUserTotalReadingTime(ctx, u)
	if err != nil {
		t.Fatalf("total: %v", err)
	}
	if total != 350 {
		t.Fatalf("total want 350 got %d", total)
	}

	top, err := store.GetUserBookReadingTimeTop(ctx, u, 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	if len(top) != 2 || top[0].BookID != book2 || top[0].TotalSeconds != 200 {
		t.Fatalf("top ranking wrong: %+v", top)
	}

	// 阅读时长也累加到当天活动（供回顾按期统计）。
	stats, err := store.GetUserPeriodStats(ctx, u, time.Now().UTC().Year(), int(time.Now().UTC().Month()))
	if err != nil {
		t.Fatalf("period: %v", err)
	}
	if stats.ReadSeconds != 350 {
		t.Fatalf("period read_seconds want 350 got %d", stats.ReadSeconds)
	}

	// 读完一本（page_count=20）后，回顾的「读完本数」应为 1（验证 books_completed 用 substr 而非无法解析的 strftime）。
	if err := store.SetUserBookProgress(ctx, u, book1, 20, time.Now()); err != nil {
		t.Fatalf("complete book1: %v", err)
	}
	now := time.Now()
	monthStats, err := store.GetUserPeriodStats(ctx, u, now.Year(), int(now.Month()))
	if err != nil {
		t.Fatalf("month period: %v", err)
	}
	if monthStats.BooksCompleted != 1 {
		t.Fatalf("month books_completed want 1 got %d", monthStats.BooksCompleted)
	}
	yearStats, err := store.GetUserPeriodStats(ctx, u, now.Year(), 0)
	if err != nil {
		t.Fatalf("year period: %v", err)
	}
	if yearStats.BooksCompleted != 1 {
		t.Fatalf("year books_completed want 1 got %d", yearStats.BooksCompleted)
	}
}

func TestUserSeriesReviewIsolation(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, seriesID, _, _ := seedUserProgressFixture(t, store)
	u1 := mkUser(t, ctx, store, "alice", RoleAdmin)
	u2 := mkUser(t, ctx, store, "bob", RoleRegular)

	r4 := 4.0
	if err := store.UpsertUserSeriesReview(ctx, u1, seriesID, &r4, "great"); err != nil {
		t.Fatalf("upsert u1: %v", err)
	}
	r2 := 2.0
	if err := store.UpsertUserSeriesReview(ctx, u2, seriesID, &r2, "meh"); err != nil {
		t.Fatalf("upsert u2: %v", err)
	}

	rv, ok, _ := store.GetUserSeriesReview(ctx, u1, seriesID)
	if !ok || !rv.Rating.Valid || rv.Rating.Float64 != 4 || rv.Review != "great" {
		t.Fatalf("u1 review wrong: %+v ok=%v", rv, ok)
	}
	rv2, ok2, _ := store.GetUserSeriesReview(ctx, u2, seriesID)
	if !ok2 || rv2.Rating.Float64 != 2 || rv2.Review != "meh" {
		t.Fatalf("u2 review wrong: %+v", rv2)
	}

	// 更新覆盖，删除只影响本人。
	if err := store.UpsertUserSeriesReview(ctx, u1, seriesID, nil, "updated"); err != nil {
		t.Fatalf("update u1: %v", err)
	}
	rv, _, _ = store.GetUserSeriesReview(ctx, u1, seriesID)
	if rv.Rating.Valid || rv.Review != "updated" {
		t.Fatalf("u1 after update wrong: %+v", rv)
	}
	if err := store.DeleteUserSeriesReview(ctx, u1, seriesID); err != nil {
		t.Fatalf("delete u1: %v", err)
	}
	if _, ok, _ := store.GetUserSeriesReview(ctx, u1, seriesID); ok {
		t.Fatal("u1 review should be gone")
	}
	if _, ok, _ := store.GetUserSeriesReview(ctx, u2, seriesID); !ok {
		t.Fatal("u2 review should remain")
	}
}
