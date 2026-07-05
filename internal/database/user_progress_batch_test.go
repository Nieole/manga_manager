// 业务说明：本文件是业务回归测试，覆盖每用户阅读进度(user_progress.go)的批量与迁移路径：
// SetUserBooksReadState(批量标记已读/未读，含 0 页书哨兵值)、GetUserBookProgressMap(批量取进度)、
// ClearUserBookProgress(清除后聚合重算)、MigrateGlobalProgressToUser(幂等 + INSERT OR IGNORE 不覆盖已有)。
// 每次写入后都要求派生 user_series_progress 聚合被正确刷新。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
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

func TestSetUserBooksReadStateBatch(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, seriesID, book1, book2 := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)
	now := time.Now()

	// 空列表：安全 no-op。
	if err := store.SetUserBooksReadState(ctx, u, nil, true, now); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if r, _, _ := aggFor(t, ctx, store, u, seriesID); r != 0 {
		t.Fatalf("empty batch should not create progress, read=%d", r)
	}

	// 批量标记两本已读（各 20 页）→ 全部完成。
	if err := store.SetUserBooksReadState(ctx, u, []int64{book1, book2}, true, now); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	if p, ok, _ := store.GetUserBookProgress(ctx, u, book1); !ok || p.LastReadPage.Int64 != 20 {
		t.Fatalf("book1 read want page=20 got %+v ok=%v", p.LastReadPage, ok)
	}
	if r, c, pg := aggFor(t, ctx, store, u, seriesID); r != 2 || c != 2 || pg != 40 {
		t.Fatalf("after mark-read agg = (read=%d completed=%d pages=%d) want (2 2 40)", r, c, pg)
	}

	// 批量标记 book1 未读 → 清除该本，聚合降为 1/1/20。
	if err := store.SetUserBooksReadState(ctx, u, []int64{book1}, false, now); err != nil {
		t.Fatalf("mark unread: %v", err)
	}
	if _, ok, _ := store.GetUserBookProgress(ctx, u, book1); ok {
		t.Fatal("book1 should be cleared")
	}
	if r, c, pg := aggFor(t, ctx, store, u, seriesID); r != 1 || c != 1 || pg != 20 {
		t.Fatalf("after mark-unread agg = (read=%d completed=%d pages=%d) want (1 1 20)", r, c, pg)
	}
}

// TestSetUserBooksReadStateZeroPageSentinel 验证 page_count=0 的书标记已读时用哨兵值 99999：
// 计入 read_book_count，但因 page_count 非正不计入 completed_book_count。
func TestSetUserBooksReadStateZeroPageSentinel(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Z", Path: filepath.Join(t.TempDir(), "z"), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("lib: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{LibraryID: lib.ID, Name: "ZS", Path: filepath.Join(lib.Path, "ZS"), NameInitial: "Z"})
	if err != nil {
		t.Fatalf("series: %v", err)
	}
	zeroBook, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID: series.ID, LibraryID: lib.ID, Name: "z0.cbz", Path: filepath.Join(lib.Path, "z0.cbz"),
		Size: 1, FileModifiedAt: time.Now(), PageCount: 0,
	})
	if err != nil {
		t.Fatalf("zero book: %v", err)
	}
	u := mkUser(t, ctx, store, "alice", RoleAdmin)

	if err := store.SetUserBooksReadState(ctx, u, []int64{zeroBook.ID}, true, time.Now()); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	p, ok, _ := store.GetUserBookProgress(ctx, u, zeroBook.ID)
	if !ok || p.LastReadPage.Int64 != 99999 {
		t.Fatalf("zero-page book want sentinel 99999 got %+v ok=%v", p.LastReadPage, ok)
	}
	if r, c, _ := aggFor(t, ctx, store, u, series.ID); r != 1 || c != 0 {
		t.Fatalf("zero-page agg want read=1 completed=0 got read=%d completed=%d", r, c)
	}
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

func TestClearUserBookProgressNoopWhenAbsent(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, seriesID, book1, _ := seedUserProgressFixture(t, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)

	// 从未记过进度就清除 → 安全 no-op，聚合保持 0。
	if err := store.ClearUserBookProgress(ctx, u, book1); err != nil {
		t.Fatalf("clear absent: %v", err)
	}
	if r, c, pg := aggFor(t, ctx, store, u, seriesID); r != 0 || c != 0 || pg != 0 {
		t.Fatalf("agg after noop clear want zeros got (%d %d %d)", r, c, pg)
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
