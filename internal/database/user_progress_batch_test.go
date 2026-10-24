package database

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// aggFor 读取某 (user, series) 的派生聚合三元组。
func aggFor(t *testing.T, ctx context.Context, store Store, userID, seriesID int64) (read, completed, pages int64) {
	t.Helper()
	err := store.(*SqlStore).db.QueryRowContext(ctx,
		`SELECT read_book_count, completed_book_count, read_pages FROM user_series_progress WHERE user_id=? AND series_id=?`,
		userID, seriesID).Scan(&read, &completed, &pages)
	if err == sql.ErrNoRows {
		return 0, 0, 0
	}
	if err != nil {
		t.Fatalf("agg (%d,%d): %v", userID, seriesID, err)
	}
	return read, completed, pages
}

func TestGetUserBookProgressMap(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, _, book1, book2 := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)
	other := mkUser(t, ctx, store, "bob", RoleRegular)

	if err := store.SetUserBookProgress(ctx, u, book1, 7, time.Now()); err != nil {
		t.Fatalf("set: %v", err)
	}

	// 空输入 → 空 map。
	m, err := store.GetUserBookProgressMap(ctx, u, nil)
	if err != nil {
		t.Fatalf("empty map: %v", err)
	}
	if len(m) != 0 {
		t.Fatalf("empty input want empty map got %v", m)
	}

	// 只返回有进度记录的书：book1 有、book2 无。
	m, err = store.GetUserBookProgressMap(ctx, u, []int64{book1, book2})
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("map want 1 entry got %d (%v)", len(m), m)
	}
	if p, ok := m[book1]; !ok || p.LastReadPage.Int64 != 7 {
		t.Fatalf("book1 entry want page=7 got %+v ok=%v", p.LastReadPage, ok)
	}
	if _, ok := m[book2]; ok {
		t.Fatal("book2 should be absent from map")
	}

	// 隔离：另一个用户对同批书取到空。
	m2, err := store.GetUserBookProgressMap(ctx, other, []int64{book1, book2})
	if err != nil {
		t.Fatalf("other map: %v", err)
	}
	if len(m2) != 0 {
		t.Fatalf("other user want empty map got %v", m2)
	}
}

// TestMigrateGlobalProgressToUserIdempotentAndPreservesExisting 验证迁移幂等，且 INSERT OR IGNORE
// 不会覆盖用户已有的进度（已迁移用户重复迁移不改变、已有本地进度被保留）。
func TestMigrateGlobalProgressToUserIdempotentAndPreservesExisting(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, seriesID, book1, book2 := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "root", RoleAdmin)

	// 用户已有 book1 本地进度=3（迁移不应覆盖它）。
	if err := store.SetUserBookProgress(ctx, u, book1, 3, time.Now()); err != nil {
		t.Fatalf("seed local: %v", err)
	}
	// 全局世界：book1=12（会被 IGNORE）、book2=20（会被迁入）。
	for _, pr := range []struct {
		id   int64
		page int64
	}{{book1, 12}, {book2, 20}} {
		if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: pr.page, Valid: true},
			LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
			ID:           pr.id,
		}); err != nil {
			t.Fatalf("global progress %d: %v", pr.id, err)
		}
	}

	if err := store.MigrateGlobalProgressToUser(ctx, u); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// book1 保留本地 3（未被全局 12 覆盖）。
	if p, ok, _ := store.GetUserBookProgress(ctx, u, book1); !ok || p.LastReadPage.Int64 != 3 {
		t.Fatalf("book1 should keep local 3, got %+v ok=%v", p.LastReadPage, ok)
	}
	// book2 迁入为 20。
	if p, ok, _ := store.GetUserBookProgress(ctx, u, book2); !ok || p.LastReadPage.Int64 != 20 {
		t.Fatalf("book2 should migrate to 20, got %+v ok=%v", p.LastReadPage, ok)
	}

	// 再次迁移：幂等，读数不变（两本都有进度 → read_book_count=2）。
	if err := store.MigrateGlobalProgressToUser(ctx, u); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if r, _, _ := aggFor(t, ctx, store, u, seriesID); r != 2 {
		t.Fatalf("read_book_count after idempotent migrate want 2 got %d", r)
	}
}
