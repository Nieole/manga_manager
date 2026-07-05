// 业务说明：本文件是业务回归测试，聚焦系列列表/搜索查询构造器（buildSeriesSearchQuery）与其
// 两条分页入口 SearchSeriesPaged（offset）/ SearchSeriesCursor（keyset）。重点保护「全局 vs 每用户」
// 双进度来源（series_stats vs user_series_progress）在读数/最近阅读列上的隔离与正确性，以及关键字/
// 首字母/状态/分页边界与游标错误路径，防止未来对 SQL 拼装或占位符顺序的重构悄悄改变行为。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestSearchSeriesPagedDualProgressSourceIsolation 验证同一系列在全局(UserID=0)、用户 u1、用户 u2
// 三条路径下读数与 last_read_* 各自独立：全局取 series_stats/books，用户取 user_series_progress/user_book_progress。
func TestSearchSeriesPagedDualProgressSourceIsolation(t *testing.T) {
	store := newStoreForTest(t)
	ctx, libID, _, book1, _ := seedUserProgressFixture(t, store)
	u1 := mkUser(t, ctx, store, "alice", RoleAdmin)
	u2 := mkUser(t, ctx, store, "bob", RoleRegular)
	now := time.Now()

	// 全局进度：book1 读到第 10 页（旧全局世界，刷新 series_stats）。
	if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
		LastReadPage: sql.NullInt64{Int64: 10, Valid: true},
		LastReadAt:   sql.NullTime{Time: now, Valid: true},
		ID:           book1,
	}); err != nil {
		t.Fatalf("global progress: %v", err)
	}
	// u1 对同一本书读到第 5 页；u2 完全未读。
	if err := store.SetUserBookProgress(ctx, u1, book1, 5, now); err != nil {
		t.Fatalf("u1 progress: %v", err)
	}

	rowFor := func(userID int64) SearchSeriesPagedRow {
		t.Helper()
		rows, _, err := store.SearchSeriesPaged(ctx, libID, SeriesListFilters{UserID: userID}, 10, 0, "name_asc")
		if err != nil {
			t.Fatalf("search user %d: %v", userID, err)
		}
		if len(rows) != 1 {
			t.Fatalf("user %d expected 1 row got %d", userID, len(rows))
		}
		return rows[0]
	}

	// 全局路径：读数=10，last_read_book_id=book1，last_read_page 从 books 取=10。
	g := rowFor(0)
	if g.ReadCount != 10 {
		t.Fatalf("global read_count want 10 got %d", g.ReadCount)
	}
	if !g.LastReadBookID.Valid || g.LastReadBookID.Int64 != book1 {
		t.Fatalf("global last_read_book_id want %d got %+v", book1, g.LastReadBookID)
	}
	if !g.LastReadPage.Valid || g.LastReadPage.Int64 != 10 {
		t.Fatalf("global last_read_page want 10 got %+v", g.LastReadPage)
	}

	// u1 路径：读数=5，last_read_page 从 user_book_progress 取=5（与全局 10 隔离）。
	a := rowFor(u1)
	if a.ReadCount != 5 {
		t.Fatalf("u1 read_count want 5 got %d", a.ReadCount)
	}
	if !a.LastReadBookID.Valid || a.LastReadBookID.Int64 != book1 {
		t.Fatalf("u1 last_read_book_id want %d got %+v", book1, a.LastReadBookID)
	}
	if !a.LastReadPage.Valid || a.LastReadPage.Int64 != 5 {
		t.Fatalf("u1 last_read_page want 5 got %+v", a.LastReadPage)
	}

	// u2 路径：无任何进度，三个 last_read 列全 NULL，读数=0。
	b := rowFor(u2)
	if b.ReadCount != 0 {
		t.Fatalf("u2 read_count want 0 got %d", b.ReadCount)
	}
	if b.LastReadAt.Valid || b.LastReadBookID.Valid || b.LastReadPage.Valid {
		t.Fatalf("u2 expected all last_read_* NULL, got %+v", b)
	}
}

// seedFilterLibrary 建一个含 5 个不同首字母/状态系列的库，供关键字/首字母/状态/分页筛选测试复用。
func seedFilterLibrary(t *testing.T, ctx context.Context, store Store) (libID int64, ids map[string]int64) {
	t.Helper()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Filter", Path: filepath.Join(t.TempDir(), "flib"), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("create lib: %v", err)
	}
	ids = make(map[string]int64)
	type spec struct{ name, initial, status string }
	specs := []spec{
		{"Apple", "A", "ongoing"},
		{"Avocado", "A", "completed"},
		{"Banana", "B", "ongoing"},
		{"Cherry", "C", ""},
		{"9Lives", "#", ""},
	}
	db := store.(*SqlStore).db
	for _, sp := range specs {
		s, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID: lib.ID, Name: sp.name, Path: filepath.Join(lib.Path, sp.name), NameInitial: sp.initial,
		})
		if err != nil {
			t.Fatalf("create series %s: %v", sp.name, err)
		}
		ids[sp.name] = s.ID
		if sp.status != "" {
			if _, err := db.ExecContext(ctx, `UPDATE series SET status = ? WHERE id = ?`, sp.status, s.ID); err != nil {
				t.Fatalf("set status %s: %v", sp.name, err)
			}
		}
	}
	return lib.ID, ids
}

// TestSearchSeriesPagedKeywordLetterStatusFilters 覆盖关键字子串、首字母(含大小写归一与 '#')、状态筛选。
func TestSearchSeriesPagedKeywordLetterStatusFilters(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	libID, _ := seedFilterLibrary(t, ctx, store)

	names := func(f SeriesListFilters) []string {
		t.Helper()
		rows, total, err := store.SearchSeriesPaged(ctx, libID, f, 50, 0, "name_asc")
		if err != nil {
			t.Fatalf("search: %v", err)
		}
		if total != len(rows) {
			t.Fatalf("total %d != returned %d (limit large enough)", total, len(rows))
		}
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.Name
		}
		return out
	}
	sameSet := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		m := map[string]int{}
		for _, g := range got {
			m[g]++
		}
		for _, w := range want {
			m[w]--
		}
		for _, v := range m {
			if v != 0 {
				return false
			}
		}
		return true
	}

	cases := []struct {
		name string
		f    SeriesListFilters
		want []string
	}{
		{"letter-A", SeriesListFilters{Letter: "A"}, []string{"Apple", "Avocado"}},
		{"letter-lowercase-a-normalizes", SeriesListFilters{Letter: "a"}, []string{"Apple", "Avocado"}},
		{"letter-hash", SeriesListFilters{Letter: "#"}, []string{"9Lives"}},
		{"status-ongoing", SeriesListFilters{Status: "ongoing"}, []string{"Apple", "Banana"}},
		{"status-completed", SeriesListFilters{Status: "completed"}, []string{"Avocado"}},
		{"keyword-substring", SeriesListFilters{Keyword: "err"}, []string{"Cherry"}},
		{"keyword-no-match", SeriesListFilters{Keyword: "zzz"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := names(tc.f)
			if !sameSet(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestSearchSeriesPagedOffsetPaginationAndCount 验证 offset 分页跨页拼接出全部有序结果，
// 且 total 反映全量而非单页长度。
func TestSearchSeriesPagedOffsetPaginationAndCount(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	libID, _ := seedFilterLibrary(t, ctx, store)

	var all []string
	var lastTotal int
	for offset := int32(0); ; offset += 2 {
		rows, total, err := store.SearchSeriesPaged(ctx, libID, SeriesListFilters{}, 2, offset, "name_asc")
		if err != nil {
			t.Fatalf("page offset %d: %v", offset, err)
		}
		lastTotal = total
		for _, r := range rows {
			all = append(all, r.Name)
		}
		if len(rows) < 2 {
			break
		}
	}
	if lastTotal != 5 {
		t.Fatalf("total want 5 got %d", lastTotal)
	}
	// BINARY 排序：'9'(0x39) 在大写字母之前。
	want := []string{"9Lives", "Apple", "Avocado", "Banana", "Cherry"}
	if len(all) != len(want) {
		t.Fatalf("paged names %v want %v", all, want)
	}
	for i := range want {
		if all[i] != want[i] {
			t.Fatalf("paged order %v want %v", all, want)
		}
	}
}

// TestSearchSeriesCursorErrorPaths 覆盖游标分页的三条错误路径：不支持的排序、非法游标串、排序不匹配。
func TestSearchSeriesCursorErrorPaths(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	libID, _ := seedFilterLibrary(t, ctx, store)

	// 不支持游标的排序（rating 非 keyset 字段）应报错。
	if _, _, _, err := store.SearchSeriesCursor(ctx, libID, SeriesListFilters{}, 2, "rating_desc", ""); err == nil {
		t.Fatal("expected error for unsupported cursor sort")
	}

	// 非法游标串（非 base64）应报错。
	if _, _, _, err := store.SearchSeriesCursor(ctx, libID, SeriesListFilters{}, 2, "name_asc", "!!!not-base64!!!"); err == nil {
		t.Fatal("expected decode error for malformed cursor")
	}

	// 用 name_asc 生成的游标喂给 created_asc 请求应报「排序不匹配」。
	firstPage, _, err := store.SearchSeriesPaged(ctx, libID, SeriesListFilters{}, 2, 0, "name_asc")
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(firstPage) == 0 {
		t.Fatal("no rows for cursor seed")
	}
	cursor := NextSeriesSearchCursor("name_asc", firstPage[len(firstPage)-1])
	if cursor == "" {
		t.Fatal("expected non-empty name cursor")
	}
	if _, _, _, err := store.SearchSeriesCursor(ctx, libID, SeriesListFilters{}, 2, "created_asc", cursor); err == nil {
		t.Fatal("expected sort-mismatch error")
	}
}

// TestSearchSeriesCursorPerUserFullTraversal 验证 UserID>0（每用户进度来源）下游标分页能正确
// 交织 leadingArgs(两个 user_id 占位符) + seek 实参，跨页无重复无遗漏地遍历全部有序结果。
func TestSearchSeriesCursorPerUserFullTraversal(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	libID, _ := seedFilterLibrary(t, ctx, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)

	var got []string
	seen := map[string]bool{}
	cursor := ""
	for {
		rows, next, hasMore, err := store.SearchSeriesCursor(ctx, libID, SeriesListFilters{UserID: u}, 2, "name_asc", cursor)
		if err != nil {
			t.Fatalf("cursor page: %v", err)
		}
		for _, r := range rows {
			if seen[r.Name] {
				t.Fatalf("duplicate row %q across pages", r.Name)
			}
			seen[r.Name] = true
			got = append(got, r.Name)
		}
		if !hasMore {
			break
		}
		if next == "" {
			t.Fatal("expected non-empty next cursor while hasMore")
		}
		cursor = next
	}
	want := []string{"9Lives", "Apple", "Avocado", "Banana", "Cherry"}
	if len(got) != len(want) {
		t.Fatalf("traversal %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("traversal order %v want %v", got, want)
		}
	}
}

// TestSearchSeriesPagedUserReadStateCountUsesUserSource 验证带筛选的计数在 UserID>0 时对进度来源
// JOIN 补齐 user_id 占位符：u1 在读 -> reading 计数=1，另一空用户 reading 计数=0、unread=1。
func TestSearchSeriesPagedUserReadStateCountUsesUserSource(t *testing.T) {
	store := newStoreForTest(t)
	ctx, libID, _, book1, _ := seedUserProgressFixture(t, store)
	u1 := mkUser(t, ctx, store, "alice", RoleAdmin)
	u2 := mkUser(t, ctx, store, "bob", RoleRegular)

	if err := store.SetUserBookProgress(ctx, u1, book1, 5, time.Now()); err != nil {
		t.Fatalf("u1 progress: %v", err)
	}

	count := func(userID int64, state string) int {
		t.Helper()
		rows, total, err := store.SearchSeriesPaged(ctx, libID, SeriesListFilters{UserID: userID, ReadState: state}, 50, 0, "name_asc")
		if err != nil {
			t.Fatalf("count user %d state %s: %v", userID, state, err)
		}
		if total != len(rows) {
			t.Fatalf("filtered total %d != rows %d", total, len(rows))
		}
		return total
	}
	if c := count(u1, "reading"); c != 1 {
		t.Fatalf("u1 reading want 1 got %d", c)
	}
	if c := count(u2, "reading"); c != 0 {
		t.Fatalf("u2 reading want 0 got %d", c)
	}
	if c := count(u2, "unread"); c != 1 {
		t.Fatalf("u2 unread want 1 got %d", c)
	}
}
