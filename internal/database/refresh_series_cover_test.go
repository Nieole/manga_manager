// 本文件守卫封面刷新的窄查询。
//
// 封面 worker 每生成一张封面就要刷一次系列封面。若走完整的 RefreshSeriesStats，
// 那 9 个相关子查询里多个是对该系列全部书的聚合——一个 K 卷系列首扫就是 K 次全系列聚合，
// 退化成 O(K²) 行扫描。RefreshSeriesCover 只碰真正变了的那两列。

package database

import (
	"context"
	"strings"
	"testing"
)

// TestRefreshSeriesCoverUsesCoverIndex 证明两个子查询都走分区索引，没有全系列聚合。
func TestRefreshSeriesCoverUsesCoverIndex(t *testing.T) {
	store := newExecTxTestStore(t)
	plan := explainPlanText(t, store, "EXPLAIN QUERY PLAN "+refreshSeriesCover, int64(1))

	upper := strings.ToUpper(plan)
	if !strings.Contains(upper, "IDX_BOOKS_COVER_PICK") {
		t.Fatalf("封面子查询没走 idx_books_cover_pick：\n%s", plan)
	}
	// 完整的 RefreshSeriesStats 会出现对整系列的聚合扫描；窄查询不该有。
	if strings.Contains(upper, "SCAN B") {
		t.Fatalf("出现了对 books 的扫描 —— 这条查询应当只做两次索引点查：\n%s", plan)
	}
}

// TestRefreshSeriesCoverUpdatesOnlyCoverColumns 守卫「不碰其它统计列」。
//
// 这一条同时是安全网：封面 worker 与入库侧的 dirtySeries 刷新是并发的，
// 若封面刷新顺手把 read_pages 之类算一遍，就会用一个可能过期的快照覆盖掉刚算好的值。
func TestRefreshSeriesCoverUpdatesOnlyCoverColumns(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "cover", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
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
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count, cover_path)
		 VALUES (?, ?, 'v1', '/lib/v1.cbz', 0, CURRENT_TIMESTAMP, 10, 'ab/v1.webp')`,
		series.ID, lib.ID); err != nil {
		t.Fatalf("insert book: %v", err)
	}

	// 预置一份「已经算好的」统计。
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO series_stats (series_id, cover_path, cover_book_id, read_pages, read_book_count, completed_book_count, tag_names_cache, updated_at)
		 VALUES (?, '', 0, 42, 7, 3, 'Action,Drama', CURRENT_TIMESTAMP)`, series.ID); err != nil {
		t.Fatalf("seed stats: %v", err)
	}

	if err := store.RefreshSeriesCover(ctx, series.ID); err != nil {
		t.Fatalf("RefreshSeriesCover: %v", err)
	}

	var coverPath, tags string
	var readPages, readBooks, completed int64
	if err := store.DB().QueryRowContext(ctx,
		`SELECT cover_path, read_pages, read_book_count, completed_book_count, COALESCE(tag_names_cache, '')
		   FROM series_stats WHERE series_id = ?`, series.ID).
		Scan(&coverPath, &readPages, &readBooks, &completed, &tags); err != nil {
		t.Fatalf("read stats: %v", err)
	}

	if coverPath != "ab/v1.webp" {
		t.Fatalf("cover_path = %q, want ab/v1.webp —— 封面没被刷新", coverPath)
	}
	if readPages != 42 || readBooks != 7 || completed != 3 || tags != "Action,Drama" {
		t.Fatalf("封面刷新把其它统计列改掉了：read_pages=%d read_books=%d completed=%d tags=%q",
			readPages, readBooks, completed, tags)
	}
}
