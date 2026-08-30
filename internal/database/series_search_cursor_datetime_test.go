package database

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestSeriesTimeColumnsStoreSQLiteDatetimeText 守住 sqliteDatetimeLayout 的前提：series 的两个
// 时间列由 CURRENT_TIMESTAMP 写入，存储文本是 UTC 秒精度、无时区后缀。游标边界谓词按这个格式
// 绑定参数，写入路径一旦改格式，这里先红。
func TestSeriesTimeColumnsStoreSQLiteDatetimeText(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	lib := newLibraryForCursorTest(t, store)

	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID: lib.ID, Name: "Alpha",
		Path: filepath.Join(lib.Path, "Alpha"), NameInitial: SeriesInitial("", "Alpha"),
	})
	if err != nil {
		t.Fatalf("create series failed: %v", err)
	}

	// CAST 成 BLOB 取原文，绕开驱动把 DATETIME 列解析成 time.Time 的这一步。
	var created, updated string
	if err := store.(*SqlStore).db.QueryRowContext(ctx,
		`SELECT CAST(created_at AS BLOB), CAST(updated_at AS BLOB) FROM series WHERE id = ?`,
		series.ID).Scan(&created, &updated); err != nil {
		t.Fatalf("read raw datetime text failed: %v", err)
	}
	for _, raw := range []string{created, updated} {
		if _, err := time.Parse(sqliteDatetimeLayout, raw); err != nil {
			t.Fatalf("时间列存储文本 %q 不再匹配 sqliteDatetimeLayout: %v", raw, err)
		}
	}
}

// TestSearchSeriesCursorTimeSortsMatchOffset 是时间排序的金标准对拍：按前端真实翻页流程
// （第 1 页走 offset，之后逐页走游标）走完全表，结果须与一次性 OFFSET 取数逐位相同——
// 不重复、不漏行。数据同时含唯一时间戳与同秒平局，两者都是生产常态（一次扫描会把成批系列
// 写在同一秒）。
func TestSearchSeriesCursorTimeSortsMatchOffset(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	lib := newLibraryForCursorTest(t, store)
	db := store.(*SqlStore).db

	base := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// offsets 里的重复值制造同秒平局，交给 (name, id) tie-break。
	offsets := []int{0, 1, 1, 1, 2, 3, 3, 4, 5, 5, 6}
	for idx, sec := range offsets {
		name := fmt.Sprintf("Series %02d", idx)
		series, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID: lib.ID, Name: name,
			Path: filepath.Join(lib.Path, name), NameInitial: SeriesInitial("", name),
		})
		if err != nil {
			t.Fatalf("create series %s failed: %v", name, err)
		}
		// 与 CURRENT_TIMESTAMP 同格式写入：绑 time.Time 会落成生产中不存在的另一种写法。
		created := base.Add(time.Duration(sec) * time.Second).Format(sqliteDatetimeLayout)
		updated := base.Add(time.Duration(len(offsets)-sec) * time.Second).Format(sqliteDatetimeLayout)
		if _, err := db.ExecContext(ctx,
			`UPDATE series SET created_at = ?, updated_at = ? WHERE id = ?`,
			created, updated, series.ID); err != nil {
			t.Fatalf("set time columns for %s failed: %v", name, err)
		}
	}

	for _, sortBy := range []string{"updated_desc", "updated_asc", "created_desc", "created_asc"} {
		want, total, err := store.SearchSeriesPaged(ctx, lib.ID, SeriesListFilters{}, int32(len(offsets)), 0, sortBy)
		if err != nil {
			t.Fatalf("sort %s: offset 全量取数失败: %v", sortBy, err)
		}
		if total != len(offsets) {
			t.Fatalf("sort %s: total=%d want %d", sortBy, total, len(offsets))
		}

		got := walkPagesLikeFrontend(t, store, lib.ID, sortBy, 3)
		if len(got) != len(want) {
			t.Fatalf("sort %s: 翻页走出 %d 条，offset 全量 %d 条", sortBy, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i].ID {
				wantIDs := make([]int64, len(want))
				for j, r := range want {
					wantIDs[j] = r.ID
				}
				t.Fatalf("sort %s: 翻页序列 %v != offset 序列 %v", sortBy, got, wantIDs)
			}
		}
		seen := map[int64]bool{}
		for _, id := range got {
			if seen[id] {
				t.Fatalf("sort %s: 条目 %d 在翻页中重复出现: %v", sortBy, id, got)
			}
			seen[id] = true
		}
	}
}

// walkPagesLikeFrontend 复刻前端翻页：第 1 页走 offset 分页并从末行取游标，第 2 页起走游标分页。
// 这条交接路径是重复条目真正的发生处——只走游标不经 offset 起手，边界错位就照不出来。
func walkPagesLikeFrontend(t *testing.T, store Store, libID int64, sortBy string, pageSize int32) []int64 {
	t.Helper()
	ctx := context.Background()

	first, total, err := store.SearchSeriesPaged(ctx, libID, SeriesListFilters{}, pageSize, 0, sortBy)
	if err != nil {
		t.Fatalf("sort %s: 第 1 页失败: %v", sortBy, err)
	}
	ids := make([]int64, 0, total)
	for _, row := range first {
		ids = append(ids, row.ID)
	}
	if len(first) == 0 || len(ids) >= total {
		return ids
	}

	cursor := NextSeriesSearchCursor(sortBy, first[len(first)-1])
	if cursor == "" {
		t.Fatalf("sort %s: 第 1 页未产出游标", sortBy)
	}
	for page := 2; page <= total; page++ {
		rows, next, hasMore, err := store.SearchSeriesCursor(ctx, libID, SeriesListFilters{}, pageSize, sortBy, cursor)
		if err != nil {
			t.Fatalf("sort %s: 第 %d 页游标取数失败: %v", sortBy, page, err)
		}
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		if !hasMore || next == "" {
			break
		}
		cursor = next
	}
	return ids
}

func newLibraryForCursorTest(t *testing.T, store Store) Library {
	t.Helper()
	lib, err := store.CreateLibrary(context.Background(), CreateLibraryParams{
		Name:         "Main",
		Path:         filepath.Join(t.TempDir(), "library"),
		ScanMode:     "none",
		ScanInterval: 60,
		ScanFormats:  "cbz",
	})
	if err != nil {
		t.Fatalf("create library failed: %v", err)
	}
	return lib
}
