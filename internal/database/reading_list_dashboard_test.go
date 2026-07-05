// 业务说明：本文件是业务回归测试，覆盖阅读清单进度聚合 GetReadingListItemProgress（全局 vs 每用户
// 双来源）与全局统计看板 GetDashboardStats / Structural / Volatile。保护「看板/清单进度取数口径」在
// series_stats 与 user_series_progress 之间正确切换，以及结构性统计(系列/书/页/库体积)与易变统计(已读/活跃天数)的准确性。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestGetReadingListItemProgressDualSource 验证清单进度在全局(userID=0)取 series_stats、
// 每用户(userID>0)取 user_series_progress，并与彼此隔离；无进度用户仍返回 0 且保留 TotalBooks。
func TestGetReadingListItemProgressDualSource(t *testing.T) {
	store := newStoreForTest(t)
	ctx, _, seriesID, book1, _ := seedUserProgressFixture(t, store)
	u1 := mkUser(t, ctx, store, "alice", RoleAdmin)
	u2 := mkUser(t, ctx, store, "bob", RoleRegular)

	list, err := store.CreateReadingList(ctx, CreateReadingListParams{Name: "My List", Description: ""})
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	if _, err := store.AddReadingListItem(ctx, AddReadingListItemParams{ReadingListID: list.ID, SeriesID: seriesID, Note: ""}); err != nil {
		t.Fatalf("add item: %v", err)
	}

	// 全局：book1 读完(20/20) → series_stats read=1 completed=1。
	if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 20, Valid: true},
		LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
		ID:           book1,
	}); err != nil {
		t.Fatalf("global progress: %v", err)
	}
	// u1：只读到 10（在读，未完成）。
	if err := store.SetUserBookProgress(ctx, u1, book1, 10, time.Now()); err != nil {
		t.Fatalf("u1 progress: %v", err)
	}

	// 全局路径。
	g, err := store.GetReadingListItemProgress(ctx, list.ID, 0)
	if err != nil {
		t.Fatalf("global list progress: %v", err)
	}
	gp := g[seriesID]
	if gp.ReadBooks != 1 || gp.CompletedBooks != 1 || gp.TotalBooks != 2 {
		t.Fatalf("global progress = %+v want read=1 completed=1 total=2", gp)
	}

	// u1 路径：读到但未完成。
	a, err := store.GetReadingListItemProgress(ctx, list.ID, u1)
	if err != nil {
		t.Fatalf("u1 list progress: %v", err)
	}
	ap := a[seriesID]
	if ap.ReadBooks != 1 || ap.CompletedBooks != 0 || ap.TotalBooks != 2 {
		t.Fatalf("u1 progress = %+v want read=1 completed=0 total=2", ap)
	}

	// u2 路径：无进度但仍在清单里，计数为 0、TotalBooks 保留。
	b, err := store.GetReadingListItemProgress(ctx, list.ID, u2)
	if err != nil {
		t.Fatalf("u2 list progress: %v", err)
	}
	bp, ok := b[seriesID]
	if !ok {
		t.Fatal("u2 should still see the series row in the list")
	}
	if bp.ReadBooks != 0 || bp.CompletedBooks != 0 || bp.TotalBooks != 2 {
		t.Fatalf("u2 progress = %+v want read=0 completed=0 total=2", bp)
	}
}

// TestGetDashboardStatsAggregates 验证看板结构性统计(系列/书/页/库体积)与易变统计(已读/近7天活跃)。
func TestGetDashboardStatsAggregates(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Dash", Path: filepath.Join(t.TempDir(), "dash"), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("create lib: %v", err)
	}
	s1, err := store.CreateSeries(ctx, CreateSeriesParams{LibraryID: lib.ID, Name: "One", Path: filepath.Join(lib.Path, "One"), NameInitial: "O"})
	if err != nil {
		t.Fatalf("create s1: %v", err)
	}
	s2, err := store.CreateSeries(ctx, CreateSeriesParams{LibraryID: lib.ID, Name: "Two", Path: filepath.Join(lib.Path, "Two"), NameInitial: "T"})
	if err != nil {
		t.Fatalf("create s2: %v", err)
	}
	mkBook := func(seriesID int64, name string, pages, size int64) int64 {
		b, err := store.CreateBook(ctx, CreateBookParams{
			SeriesID: seriesID, LibraryID: lib.ID, Name: name, Path: filepath.Join(lib.Path, name),
			Size: size, FileModifiedAt: time.Now(), PageCount: pages,
		})
		if err != nil {
			t.Fatalf("create book %s: %v", name, err)
		}
		return b.ID
	}
	b1 := mkBook(s1.ID, "o01.cbz", 20, 100)
	b2 := mkBook(s1.ID, "o02.cbz", 30, 200)
	mkBook(s2.ID, "t01.cbz", 10, 50)

	// 结构性统计：2 系列、3 书、60 页、库体积 350。
	structural, err := store.GetDashboardStructuralStats(ctx)
	if err != nil {
		t.Fatalf("structural: %v", err)
	}
	if structural.TotalSeries != 2 || structural.TotalBooks != 3 || structural.TotalPages != 60 {
		t.Fatalf("structural = %+v want series=2 books=3 pages=60", structural)
	}
	var libSize int64 = -1
	for _, ls := range structural.LibrarySizes {
		if ls.LibraryID == lib.ID {
			libSize = ls.TotalSize
		}
	}
	if libSize != 350 {
		t.Fatalf("library size want 350 got %d", libSize)
	}

	// 已读：给 b1、b2 记全局进度 → read_books=2。
	for _, id := range []int64{b1, b2} {
		if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: 5, Valid: true},
			LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
			ID:           id,
		}); err != nil {
			t.Fatalf("progress %d: %v", id, err)
		}
	}
	// 近 7 天活跃：3 个不同日期在窗口内 + 1 个 10 天前(应被排除) → active_days_7=3。
	db := store.(*SqlStore).db
	for _, d := range []string{dayStr(0), dayStr(-1), dayStr(-3), dayStr(-10)} {
		if _, err := db.ExecContext(ctx, `INSERT INTO reading_activity (book_id, date, pages_read) VALUES (?, ?, 5)`, b1, d); err != nil {
			t.Fatalf("insert activity %s: %v", d, err)
		}
	}

	volatile, err := store.GetDashboardVolatileStats(ctx)
	if err != nil {
		t.Fatalf("volatile: %v", err)
	}
	if volatile.ReadBooks != 2 {
		t.Fatalf("read_books want 2 got %d", volatile.ReadBooks)
	}
	if volatile.ActiveDays7 != 3 {
		t.Fatalf("active_days_7 want 3 got %d", volatile.ActiveDays7)
	}

	// 组合看板应等于两部分之并。
	combined, err := store.GetDashboardStats(ctx)
	if err != nil {
		t.Fatalf("combined: %v", err)
	}
	if combined.TotalSeries != 2 || combined.TotalBooks != 3 || combined.TotalPages != 60 ||
		combined.ReadBooks != 2 || combined.ActiveDays7 != 3 {
		t.Fatalf("combined = %+v", combined)
	}
}
