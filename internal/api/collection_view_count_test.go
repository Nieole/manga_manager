// 业务说明：本文件守卫「智能书架的计数与它的列表一致」。
//
// 书架列表页显示的系列数此前是 ListCollectionViews 内联的一套 SQL，与真正出列表的查询
// （buildSmartCollectionBaseQuery）是两套独立实现，有六处口径分歧——最刺眼的是
// completed 用 `=` 而列表用 `>=`，以及计数读全局 books.last_read_page 而列表读每用户进度。
// 用户看到的就是「书架上写着 12 个系列，点进去只有 9 个」。

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// TestSmartCollectionCountMatchesListing 是主判据：对同一个用户，
// 书架列表里的 SeriesCount 必须恒等于该书架详情返回的 total。
//
// 这条恒等式不依赖任何冗余列是否已回填——两侧走的是同一份查询，
// 因此无论数据处于什么状态，它都必须成立。
func TestSmartCollectionCountMatchesListing(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "smart", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	// 两个系列，各两本；一个被 alice 读完，另一个没人读。
	type seeded struct {
		seriesID int64
		bookIDs  []int64
	}
	var all []seeded
	for i := 0; i < 2; i++ {
		series, err := store.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
			LibraryID: lib.ID, Name: "S" + string(rune('A'+i)), Path: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("UpsertSeriesByPath: %v", err)
		}
		s := seeded{seriesID: series.ID}
		for v := 0; v < 2; v++ {
			res, err := store.(*database.SqlStore).DB().ExecContext(ctx,
				`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
				 VALUES (?, ?, ?, ?, 0, CURRENT_TIMESTAMP, 10)`,
				series.ID, lib.ID, "v", t.TempDir()+"/v.cbz")
			if err != nil {
				t.Fatalf("insert book: %v", err)
			}
			id, _ := res.LastInsertId()
			s.bookIDs = append(s.bookIDs, id)
		}
		// 冗余统计列：列表侧读它们，不回填的话 completed 判定恒不成立。
		if _, err := store.(*database.SqlStore).DB().ExecContext(ctx,
			`UPDATE series SET book_count = 2, total_pages = 20 WHERE id = ?`, series.ID); err != nil {
			t.Fatalf("update series stats: %v", err)
		}
		all = append(all, s)
	}

	alice := mkTestUser(t, store, "alice", database.RoleRegular)
	if err := store.SetUserBooksReadState(ctx, alice.ID, all[0].bookIDs, true, time.Now()); err != nil {
		t.Fatalf("SetUserBooksReadState: %v", err)
	}

	completed := "completed"
	filter, err := store.UpsertSmartFilter(ctx, database.UpsertSmartFilterParams{
		LibraryID: lib.ID,
		Name:      "已读完",
		ReadState: nullStringFromPointer(&completed),
	})
	if err != nil {
		t.Fatalf("UpsertSmartFilter: %v", err)
	}

	for _, tc := range []struct {
		name   string
		userID int64
	}{
		{"alice 读完了第一个系列", alice.ID},
		{"无用户上下文", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			views, err := controller.loadCollectionViews(ctx, tc.userID)
			if err != nil {
				t.Fatalf("loadCollectionViews: %v", err)
			}
			var gotCount int
			var found bool
			for _, v := range views {
				if v.Kind == "smart" && v.NumericID == filter.ID {
					gotCount = v.SeriesCount
					found = true
				}
			}
			if !found {
				t.Fatal("书架列表里找不到这个智能书架")
			}

			_, gotTotal, err := controller.loadSmartCollectionSeries(ctx, smartFilterFromDB(filter), 50, 0, tc.userID)
			if err != nil {
				t.Fatalf("loadSmartCollectionSeries: %v", err)
			}
			if gotCount != gotTotal {
				t.Fatalf("书架列表显示 %d 个系列，点进去是 %d 个 —— 计数与列表口径又分家了", gotCount, gotTotal)
			}
		})
	}
}

// TestSmartCollectionCountIsPerUser 覆盖多用户：同一个「已读完」书架，
// 读过的人和没读过的人看到的数字必须不同。计数若仍读全局进度列，两人会看到同一个数。
func TestSmartCollectionCountIsPerUser(t *testing.T) {
	controller, store, _, _ := newTestController(t)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "peruser", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	series, err := store.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
		LibraryID: lib.ID, Name: "S", Path: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("UpsertSeriesByPath: %v", err)
	}
	res, err := store.(*database.SqlStore).DB().ExecContext(ctx,
		`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
		 VALUES (?, ?, 'v1', ?, 0, CURRENT_TIMESTAMP, 10)`, series.ID, lib.ID, t.TempDir()+"/v1.cbz")
	if err != nil {
		t.Fatalf("insert book: %v", err)
	}
	bookID, _ := res.LastInsertId()
	if _, err := store.(*database.SqlStore).DB().ExecContext(ctx,
		`UPDATE series SET book_count = 1, total_pages = 10 WHERE id = ?`, series.ID); err != nil {
		t.Fatalf("update series stats: %v", err)
	}

	alice := mkTestUser(t, store, "alice", database.RoleRegular)
	bob := mkTestUser(t, store, "bob", database.RoleRegular)
	if err := store.SetUserBooksReadState(ctx, alice.ID, []int64{bookID}, true, time.Now()); err != nil {
		t.Fatalf("SetUserBooksReadState: %v", err)
	}

	completed := "completed"
	filter, err := store.UpsertSmartFilter(ctx, database.UpsertSmartFilterParams{
		LibraryID: lib.ID, Name: "已读完", ReadState: nullStringFromPointer(&completed),
	})
	if err != nil {
		t.Fatalf("UpsertSmartFilter: %v", err)
	}

	countFor := func(userID int64) int {
		views, err := controller.loadCollectionViews(ctx, userID)
		if err != nil {
			t.Fatalf("loadCollectionViews: %v", err)
		}
		for _, v := range views {
			if v.Kind == "smart" && v.NumericID == filter.ID {
				return v.SeriesCount
			}
		}
		t.Fatal("找不到智能书架")
		return -1
	}

	if got := countFor(alice.ID); got != 1 {
		t.Fatalf("alice 读完了，「已读完」书架应显示 1，实际 %d", got)
	}
	if got := countFor(bob.ID); got != 0 {
		t.Fatalf("bob 没读过，「已读完」书架应显示 0，实际 %d —— 计数读的还是全局进度列", got)
	}
}

// TestCollectionViewsEndpointStillServes 保证三个入口的 HTTP 层没被改坏。
func TestCollectionViewsEndpointStillServes(t *testing.T) {
	controller, _, _, _ := newTestController(t)
	rec := httptest.NewRecorder()
	controller.listCollectionViews(rec, httptest.NewRequest(http.MethodGet, "/api/collection-views", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("listCollectionViews = %d, body=%s", rec.Code, rec.Body.String())
	}
}
