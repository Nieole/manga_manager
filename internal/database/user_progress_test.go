package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// seedUserProgressFixture 建一个库 + 一个系列 + 两本书（各 20 页），并把 series 的冗余统计列
// （扫描器维护、测试里默认 0）手动设为 book_count=2 / total_pages=40，供阅读状态/进度筛选生效。
func seedUserProgressFixture(t *testing.T, store Store) (ctx context.Context, libID, seriesID, book1, book2 int64) {
	t.Helper()
	ctx = context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Main", Path: filepath.Join(t.TempDir(), "library"), ScanMode: "none",
		KoreaderSyncEnabled: true, ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("create library: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID: lib.ID, Name: "Series A", Path: filepath.Join(lib.Path, "Series A"), NameInitial: "S",
	})
	if err != nil {
		t.Fatalf("create series: %v", err)
	}
	b1, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID: series.ID, LibraryID: lib.ID, Name: "Vol.01.cbz",
		Path: filepath.Join(series.Path, "Vol.01.cbz"), Size: 1024, FileModifiedAt: time.Now(), PageCount: 20,
	})
	if err != nil {
		t.Fatalf("create book1: %v", err)
	}
	b2, err := store.CreateBook(ctx, CreateBookParams{
		SeriesID: series.ID, LibraryID: lib.ID, Name: "Vol.02.cbz",
		Path: filepath.Join(series.Path, "Vol.02.cbz"), Size: 1024, FileModifiedAt: time.Now(), PageCount: 20,
	})
	if err != nil {
		t.Fatalf("create book2: %v", err)
	}
	db := store.(*SqlStore).db
	if _, err := db.ExecContext(ctx, `UPDATE series SET book_count = 2, total_pages = 40 WHERE id = ?`, series.ID); err != nil {
		t.Fatalf("set series stats: %v", err)
	}
	return ctx, lib.ID, series.ID, b1.ID, b2.ID
}

func mkUser(t *testing.T, ctx context.Context, store Store, name, role string) int64 {
	t.Helper()
	u, err := store.CreateUser(ctx, CreateUserParams{Username: name, PasswordHash: "x", Role: role})
	if err != nil {
		t.Fatalf("create user %s: %v", name, err)
	}
	return u.ID
}

func TestUserBookProgressIsolationAndAggregation(t *testing.T) {
	store := newStoreForTest(t)
	ctx, libID, seriesID, book1, book2 := seedUserProgressFixture(t, store)
	u1 := mkUser(t, ctx, store, "alice", RoleAdmin)
	u2 := mkUser(t, ctx, store, "bob", RoleRegular)
	now := time.Now()

	// 两个用户对同一本书有不同进度；u1 还读完了 book2。
	if err := store.SetUserBookProgress(ctx, u1, book1, 10, now); err != nil {
		t.Fatalf("set u1/book1: %v", err)
	}
	if err := store.SetUserBookProgress(ctx, u2, book1, 5, now); err != nil {
		t.Fatalf("set u2/book1: %v", err)
	}
	if err := store.SetUserBookProgress(ctx, u1, book2, 20, now); err != nil {
		t.Fatalf("set u1/book2: %v", err)
	}

	// 隔离性：各看各的进度。
	if p, ok, _ := store.GetUserBookProgress(ctx, u1, book1); !ok || p.LastReadPage.Int64 != 10 {
		t.Fatalf("u1/book1 want 10, got %v ok=%v", p.LastReadPage, ok)
	}
	if p, ok, _ := store.GetUserBookProgress(ctx, u2, book1); !ok || p.LastReadPage.Int64 != 5 {
		t.Fatalf("u2/book1 want 5, got %v ok=%v", p.LastReadPage, ok)
	}
	if _, ok, _ := store.GetUserBookProgress(ctx, u2, book2); ok {
		t.Fatal("u2/book2 should have no progress")
	}

	// 每用户已读数。
	if n, _ := store.GetUserReadBooksCount(ctx, u1); n != 2 {
		t.Fatalf("u1 read count want 2 got %d", n)
	}
	if n, _ := store.GetUserReadBooksCount(ctx, u2); n != 1 {
		t.Fatalf("u2 read count want 1 got %d", n)
	}

	// 派生系列聚合：u1 read_book_count=2 completed=1 read_pages=30；u2 read_book_count=1。
	assertSeriesAgg := func(userID int64, wantRead, wantCompleted, wantPages int64) {
		t.Helper()
		var rb, cb, rp int64
		err := store.(*SqlStore).db.QueryRowContext(ctx,
			`SELECT read_book_count, completed_book_count, read_pages FROM user_series_progress WHERE user_id=? AND series_id=?`,
			userID, seriesID).Scan(&rb, &cb, &rp)
		if err != nil {
			t.Fatalf("agg user %d: %v", userID, err)
		}
		if rb != wantRead || cb != wantCompleted || rp != wantPages {
			t.Fatalf("user %d agg = (read=%d completed=%d pages=%d), want (%d %d %d)", userID, rb, cb, rp, wantRead, wantCompleted, wantPages)
		}
	}
	assertSeriesAgg(u1, 2, 1, 30)
	assertSeriesAgg(u2, 1, 0, 5)

	// 阅读状态筛选按用户：u1 "reading"（有读但未全读完）能命中；全新用户 "unread" 命中、"reading" 空。
	u3 := mkUser(t, ctx, store, "carol", RoleRegular)
	countFor := func(userID int64, state string) int {
		t.Helper()
		_, total, err := store.SearchSeriesPaged(ctx, libID, SeriesListFilters{UserID: userID, ReadState: state}, 50, 0, "name")
		if err != nil {
			t.Fatalf("search user %d state %s: %v", userID, state, err)
		}
		return total
	}
	if got := countFor(u1, "reading"); got != 1 {
		t.Fatalf("u1 reading want 1 got %d", got)
	}
	if got := countFor(u1, "unread"); got != 0 {
		t.Fatalf("u1 unread want 0 got %d", got)
	}
	if got := countFor(u3, "unread"); got != 1 {
		t.Fatalf("u3 unread want 1 got %d", got)
	}
	if got := countFor(u3, "reading"); got != 0 {
		t.Fatalf("u3 reading want 0 got %d", got)
	}

	// 清除 u1/book1 后重算：read_book_count 降为 1。
	if err := store.ClearUserBookProgress(ctx, u1, book1); err != nil {
		t.Fatalf("clear u1/book1: %v", err)
	}
	assertSeriesAgg(u1, 1, 1, 20)
}

func TestMigrateGlobalProgressToUser(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, seriesID, book1, _ := seedUserProgressFixture(t, store)

	// 旧的全局进度（迁移前的世界）。
	if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 12, Valid: true}, LastReadAt: sql.NullTime{Time: time.Now(), Valid: true}, ID: book1,
	}); err != nil {
		t.Fatalf("seed global progress: %v", err)
	}

	admin := mkUser(t, ctx, store, "root", RoleAdmin)
	if err := store.MigrateGlobalProgressToUser(ctx, admin); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if p, ok, _ := store.GetUserBookProgress(ctx, admin, book1); !ok || p.LastReadPage.Int64 != 12 {
		t.Fatalf("migrated progress want 12, got %v ok=%v", p.LastReadPage, ok)
	}
	// 迁移也回填了系列聚合。
	var rb int64
	if err := store.(*SqlStore).db.QueryRowContext(ctx,
		`SELECT read_book_count FROM user_series_progress WHERE user_id=? AND series_id=?`, admin, seriesID).Scan(&rb); err != nil {
		t.Fatalf("agg after migrate: %v", err)
	}
	if rb != 1 {
		t.Fatalf("migrated agg read_book_count want 1 got %d", rb)
	}
}
