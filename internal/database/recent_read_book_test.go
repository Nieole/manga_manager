// 本文件守「这个系列最近读的是哪一本」：取最近一条的两条实现在时间戳打平时必须给出同一本书。
//
// 打平是常态而非巧合——「把整个系列标记为已读」只取一次 time.Now()，整批共用它。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// seedTiedReadSeries 建一个库 + 一个系列 + 三本书，书按卷序建立（id 递增即卷序）。
func seedTiedReadSeries(t *testing.T, store Store) (ctx context.Context, libID, seriesID int64, bookIDs []int64) {
	t.Helper()
	ctx = context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Recent", Path: filepath.Join(t.TempDir(), "library"), ScanMode: "none",
		ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	series, err := store.CreateSeries(ctx, CreateSeriesParams{
		LibraryID: lib.ID, Name: "Series Alpha",
		Path: filepath.Join(lib.Path, "Series Alpha"), NameInitial: SeriesInitial("", "Series Alpha"),
	})
	if err != nil {
		t.Fatalf("CreateSeries: %v", err)
	}
	for _, name := range []string{"Vol.01.cbz", "Vol.02.cbz", "Vol.03.cbz"} {
		book, err := store.CreateBook(ctx, CreateBookParams{
			SeriesID: series.ID, LibraryID: lib.ID, Name: name,
			Path: filepath.Join(series.Path, name), Size: 1024, FileModifiedAt: time.Now(), PageCount: 20,
		})
		if err != nil {
			t.Fatalf("CreateBook %s: %v", name, err)
		}
		bookIDs = append(bookIDs, book.ID)
	}
	return ctx, lib.ID, series.ID, bookIDs
}

// TestRecentReadBookAgreesWhenTimestampsTie 把整个系列标成已读（全批同一个时间戳），再让两侧各自
// 回答「最近读的是哪一本」。
//
// 资料库页的「最近阅读的系列」走窗口函数取每系列 rn=1，看板与系列页的「继续阅读」走派生表里
// 的 last_read_book_id；窗口的排序键少了末位唯一键，平局时由查询计划挑一行，两处于是各说各话。
func TestRecentReadBookAgreesWhenTimestampsTie(t *testing.T) {
	cases := []struct {
		name string
		// markRead 让整个系列在同一时刻变成已读。
		markRead func(t *testing.T, ctx context.Context, store *SqlStore, userID int64, bookIDs []int64)
		// rankedSide 是窗口函数那一侧（资料库页「最近阅读的系列」）给出的书。
		rankedSide func(t *testing.T, ctx context.Context, store *SqlStore, userID, libID int64) int64
		// derivedSide 是派生表 last_read_book_id 那一侧（看板「继续阅读」）给出的书。
		derivedSide func(t *testing.T, ctx context.Context, store *SqlStore, userID int64) int64
	}{
		{
			name: "每用户进度",
			markRead: func(t *testing.T, ctx context.Context, store *SqlStore, userID int64, bookIDs []int64) {
				if err := store.SetUserBooksReadState(ctx, userID, bookIDs, true, time.Now()); err != nil {
					t.Fatalf("SetUserBooksReadState: %v", err)
				}
			},
			rankedSide: func(t *testing.T, ctx context.Context, store *SqlStore, userID, libID int64) int64 {
				rows, err := store.GetUserRecentReadSeries(ctx, userID, libID, 10)
				if err != nil {
					t.Fatalf("GetUserRecentReadSeries: %v", err)
				}
				if len(rows) != 1 {
					t.Fatalf("GetUserRecentReadSeries 返回 %d 行，期望 1 行", len(rows))
				}
				return rows[0].RecentBookID
			},
			derivedSide: func(t *testing.T, ctx context.Context, store *SqlStore, userID int64) int64 {
				rows, err := store.GetUserRecentReadAll(ctx, userID, 10)
				if err != nil {
					t.Fatalf("GetUserRecentReadAll: %v", err)
				}
				if len(rows) != 1 {
					t.Fatalf("GetUserRecentReadAll 返回 %d 行，期望 1 行", len(rows))
				}
				return rows[0].BookID
			},
		},
		{
			name: "全局进度",
			markRead: func(t *testing.T, ctx context.Context, store *SqlStore, _ int64, bookIDs []int64) {
				at := sql.NullTime{Time: time.Now(), Valid: true}
				for _, bookID := range bookIDs {
					if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
						LastReadPage: sql.NullInt64{Int64: 20, Valid: true},
						LastReadAt:   at,
						ID:           bookID,
					}); err != nil {
						t.Fatalf("UpdateBookProgress(%d): %v", bookID, err)
					}
				}
			},
			rankedSide: func(t *testing.T, ctx context.Context, store *SqlStore, _, libID int64) int64 {
				rows, err := store.GetRecentReadSeries(ctx, GetRecentReadSeriesParams{
					LibraryID: libID, LibraryID_2: libID, Limit: 10,
				})
				if err != nil {
					t.Fatalf("GetRecentReadSeries: %v", err)
				}
				if len(rows) != 1 {
					t.Fatalf("GetRecentReadSeries 返回 %d 行，期望 1 行", len(rows))
				}
				return rows[0].RecentBookID
			},
			derivedSide: func(t *testing.T, ctx context.Context, store *SqlStore, _ int64) int64 {
				rows, err := store.GetRecentReadAll(ctx, 10)
				if err != nil {
					t.Fatalf("GetRecentReadAll: %v", err)
				}
				if len(rows) != 1 {
					t.Fatalf("GetRecentReadAll 返回 %d 行，期望 1 行", len(rows))
				}
				return rows[0].BookID
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newStoreForTest(t).(*SqlStore)
			ctx, libID, _, bookIDs := seedTiedReadSeries(t, store)
			userID := mkUser(t, ctx, store, "reader", "regular")

			tc.markRead(t, ctx, store, userID, bookIDs)

			ranked := tc.rankedSide(t, ctx, store, userID, libID)
			derived := tc.derivedSide(t, ctx, store, userID)
			if ranked != derived {
				t.Errorf("「最近阅读的系列」指向书 %d，「继续阅读」指向书 %d —— 同一个系列，两处给出不同的书",
					ranked, derived)
			}
			last := bookIDs[len(bookIDs)-1]
			if ranked != last {
				t.Errorf("「最近阅读的系列」指向书 %d，期望最后一卷 %d —— 平局时该由末位唯一键定先后",
					ranked, last)
			}
		})
	}
}
