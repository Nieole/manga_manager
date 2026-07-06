// 业务说明：本文件是业务回归测试，覆盖智能合集筛选器 buildSmartCollectionBaseQuery / smartCollectionOrderClause。
// 重点保护阅读状态(未读/在读/完成)、评分区间、进度区间、加入天数、排序方向，以及「全局 vs 每用户」进度来源切换，
// 确保智能合集的取数口径与常规库列表(buildSeriesSearchQuery)保持一致，防止筛选/排序在重构后漂移。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// smartFixture 建一个库含 3 个系列并设置好统计列/评分/全局进度，返回库与系列 id。
//   - SA：2 本各 20 页(book_count=2,total_pages=40)，book1 全局读到 10 页 → 在读、进度 25%、评分 8
//   - SB：1 本 20 页，无进度 → 未读、进度 0%、评分 3
//   - SC：1 本 20 页，全局读完 → 完成、进度 100%、评分 6
type smartFixture struct {
	libID            int64
	saID, sbID, scID int64
	saBook1, scBook1 int64
}

func newSmartFixture(t *testing.T, ctx context.Context, store Store) smartFixture {
	t.Helper()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "Smart", Path: filepath.Join(t.TempDir(), "smart"), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("create lib: %v", err)
	}
	db := store.(*SqlStore).db
	mkSeries := func(name string) int64 {
		s, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID: lib.ID, Name: name, Path: filepath.Join(lib.Path, name), NameInitial: name[:1],
		})
		if err != nil {
			t.Fatalf("create series %s: %v", name, err)
		}
		return s.ID
	}
	mkBook := func(seriesID int64, name string) int64 {
		b, err := store.CreateBook(ctx, CreateBookParams{
			SeriesID: seriesID, LibraryID: lib.ID, Name: name,
			Path: filepath.Join(lib.Path, name), Size: 1, FileModifiedAt: time.Now(), PageCount: 20,
		})
		if err != nil {
			t.Fatalf("create book %s: %v", name, err)
		}
		return b.ID
	}

	fx := smartFixture{libID: lib.ID}
	fx.saID = mkSeries("SA")
	fx.saBook1 = mkBook(fx.saID, "sa01.cbz")
	mkBook(fx.saID, "sa02.cbz")
	fx.sbID = mkSeries("SB")
	mkBook(fx.sbID, "sb01.cbz")
	fx.scID = mkSeries("SC")
	fx.scBook1 = mkBook(fx.scID, "sc01.cbz")

	// 扫描器维护的冗余统计列 + 评分。
	set := func(id int64, rating float64, books, pages int) {
		if _, err := db.ExecContext(ctx, `UPDATE series SET rating=?, book_count=?, total_pages=? WHERE id=?`, rating, books, pages, id); err != nil {
			t.Fatalf("set fields %d: %v", id, err)
		}
	}
	set(fx.saID, 8, 2, 40)
	set(fx.sbID, 3, 1, 20)
	set(fx.scID, 6, 1, 20)

	// 全局进度：SA 读到 10，SC 读完 20（刷新 series_stats）。
	upd := func(bookID int64, page int) {
		if err := store.UpdateBookProgress(ctx, UpdateBookProgressParams{
			LastReadPage: sql.NullInt64{Int64: int64(page), Valid: true},
			LastReadAt:   sql.NullTime{Time: time.Now(), Valid: true},
			ID:           bookID,
		}); err != nil {
			t.Fatalf("progress book %d: %v", bookID, err)
		}
	}
	upd(fx.saBook1, 10)
	upd(fx.scBook1, 20)
	return fx
}

func smartIDs(t *testing.T, ctx context.Context, store Store, f SmartCollectionFilter) []int64 {
	t.Helper()
	rows, total, err := store.SearchSmartCollectionSeries(ctx, f, 50, 0)
	if err != nil {
		t.Fatalf("smart search: %v", err)
	}
	if total != len(rows) {
		t.Fatalf("smart total %d != rows %d (limit large)", total, len(rows))
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

func hasExactly(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	m := map[int64]int{}
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

// TestSmartCollectionReadStateAndRanges 覆盖全局路径下的读状态、评分区间、进度区间筛选。
func TestSmartCollectionReadStateAndRanges(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	fx := newSmartFixture(t, ctx, store)

	f := func(mut func(*SmartCollectionFilter)) SmartCollectionFilter {
		base := SmartCollectionFilter{LibraryID: fx.libID}
		mut(&base)
		return base
	}
	fptr := func(v float64) *float64 { return &v }

	cases := []struct {
		name string
		f    SmartCollectionFilter
		want []int64
	}{
		{"unread", f(func(c *SmartCollectionFilter) { c.ReadState = "unread" }), []int64{fx.sbID}},
		{"reading", f(func(c *SmartCollectionFilter) { c.ReadState = "reading" }), []int64{fx.saID}},
		{"completed", f(func(c *SmartCollectionFilter) { c.ReadState = "completed" }), []int64{fx.scID}},
		{"minRating>=5", f(func(c *SmartCollectionFilter) { c.MinRating = fptr(5) }), []int64{fx.saID, fx.scID}},
		{"maxRating<=5", f(func(c *SmartCollectionFilter) { c.MaxRating = fptr(5) }), []int64{fx.sbID}},
		{"rating 4..7", f(func(c *SmartCollectionFilter) { c.MinRating = fptr(4); c.MaxRating = fptr(7) }), []int64{fx.scID}},
		{"minProgress>=50", f(func(c *SmartCollectionFilter) { c.MinProgress = fptr(50) }), []int64{fx.scID}},
		{"maxProgress<=30", f(func(c *SmartCollectionFilter) { c.MaxProgress = fptr(30) }), []int64{fx.saID, fx.sbID}},
		{"progress 10..50", f(func(c *SmartCollectionFilter) { c.MinProgress = fptr(10); c.MaxProgress = fptr(50) }), []int64{fx.saID}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := smartIDs(t, ctx, store, tc.f)
			if !hasExactly(got, tc.want...) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestSmartCollectionOrdering 验证排序子句：评分降序 + 名称升序默认。
func TestSmartCollectionOrdering(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	fx := newSmartFixture(t, ctx, store)

	// 评分降序：SA(8) > SC(6) > SB(3)。
	got := smartIDs(t, ctx, store, SmartCollectionFilter{LibraryID: fx.libID, SortByField: "rating", SortDir: "desc"})
	want := []int64{fx.saID, fx.scID, fx.sbID}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("rating desc order got %v want %v", got, want)
	}

	// 默认名称升序：SA < SB < SC。
	got = smartIDs(t, ctx, store, SmartCollectionFilter{LibraryID: fx.libID})
	want = []int64{fx.saID, fx.sbID, fx.scID}
	if got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("name asc order got %v want %v", got, want)
	}
}

// TestSmartCollectionAddedWithinDays 验证「加入天数」筛选排除较早加入的系列。
func TestSmartCollectionAddedWithinDays(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	fx := newSmartFixture(t, ctx, store)
	db := store.(*SqlStore).db

	// 把 SB 的 created_at 挪到 10 天前。
	if _, err := db.ExecContext(ctx, `UPDATE series SET created_at = datetime('now','-10 days') WHERE id = ?`, fx.sbID); err != nil {
		t.Fatalf("age SB: %v", err)
	}
	days := 1
	got := smartIDs(t, ctx, store, SmartCollectionFilter{LibraryID: fx.libID, AddedWithinDays: &days})
	if !hasExactly(got, fx.saID, fx.scID) {
		t.Fatalf("addedWithin1d got %v want [SA SC]", got)
	}
}

// TestSmartCollectionTagAuthorFilters 覆盖 ActiveTag / ActiveAuthor（改写为 EXISTS 子查询、去掉
// tags×authors 交叉 JOIN + GROUP BY 之后）：命中正确、多标签系列不因筛选而重复计数，且 tags_string
// 来自预计算的 series_stats.tag_names_cache。
func TestSmartCollectionTagAuthorFilters(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	fx := newSmartFixture(t, ctx, store)

	linkTag := func(seriesID int64, name string) {
		tag, err := store.UpsertTag(ctx, name)
		if err != nil {
			t.Fatalf("upsert tag %s: %v", name, err)
		}
		if err := store.LinkSeriesTag(ctx, LinkSeriesTagParams{SeriesID: seriesID, TagID: tag.ID}); err != nil {
			t.Fatalf("link tag %s: %v", name, err)
		}
	}
	linkAuthor := func(seriesID int64, name string) {
		a, err := store.UpsertAuthor(ctx, UpsertAuthorParams{Name: name, Role: "writer"})
		if err != nil {
			t.Fatalf("upsert author %s: %v", name, err)
		}
		if err := store.LinkSeriesAuthor(ctx, LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: a.ID}); err != nil {
			t.Fatalf("link author %s: %v", name, err)
		}
	}

	// SA: 两个标签 Action+Adventure（验证多标签不导致重复行）+ 作者 Writer X
	// SB: 标签 Action
	// SC: 标签 Drama + 作者 Writer Y
	linkTag(fx.saID, "Action")
	linkTag(fx.saID, "Adventure")
	linkAuthor(fx.saID, "Writer X")
	linkTag(fx.sbID, "Action")
	linkTag(fx.scID, "Drama")
	linkAuthor(fx.scID, "Writer Y")
	for _, id := range []int64{fx.saID, fx.sbID, fx.scID} {
		if err := store.RefreshSeriesStats(ctx, id); err != nil {
			t.Fatalf("refresh stats %d: %v", id, err)
		}
	}

	base := func(mut func(*SmartCollectionFilter)) SmartCollectionFilter {
		f := SmartCollectionFilter{LibraryID: fx.libID}
		mut(&f)
		return f
	}
	cases := []struct {
		name string
		f    SmartCollectionFilter
		want []int64
	}{
		{"tag Action", base(func(c *SmartCollectionFilter) { c.ActiveTag = "Action" }), []int64{fx.saID, fx.sbID}},
		{"tag Drama", base(func(c *SmartCollectionFilter) { c.ActiveTag = "Drama" }), []int64{fx.scID}},
		{"tag Adventure (SA only, no dup)", base(func(c *SmartCollectionFilter) { c.ActiveTag = "Adventure" }), []int64{fx.saID}},
		{"author Writer Y", base(func(c *SmartCollectionFilter) { c.ActiveAuthor = "Writer Y" }), []int64{fx.scID}},
		{"tag Action + author Writer X", base(func(c *SmartCollectionFilter) { c.ActiveTag = "Action"; c.ActiveAuthor = "Writer X" }), []int64{fx.saID}},
		{"tag Action + author Writer Y (disjoint)", base(func(c *SmartCollectionFilter) { c.ActiveTag = "Action"; c.ActiveAuthor = "Writer Y" }), []int64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := smartIDs(t, ctx, store, tc.f)
			if !hasExactly(got, tc.want...) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}

	// tags_string 应来自 series_stats.tag_names_cache（含 SA 的两个标签），而非 GROUP_CONCAT。
	rows, _, err := store.SearchSmartCollectionSeries(ctx, SmartCollectionFilter{LibraryID: fx.libID, ActiveTag: "Adventure"}, 50, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(rows) != 1 || rows[0].TagsString == nil {
		t.Fatalf("expected 1 row with non-nil tags_string, got %+v", rows)
	}
	if ts := *rows[0].TagsString; !containsAll(ts, "Action", "Adventure") {
		t.Fatalf("tags_string %q should contain Action and Adventure", ts)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestSmartCollectionUserProgressSource 验证 UserID>0 时智能合集读状态改用 user_series_progress，
// 与全局隔离：给用户 u 在 SA 记进度 -> u 的 reading={SA}；空用户 unread=全部、reading=空。
func TestSmartCollectionUserProgressSource(t *testing.T) {
	store := newStoreForTest(t)
	ctx := context.Background()
	fx := newSmartFixture(t, ctx, store)
	u := mkUser(t, ctx, store, "alice", RoleAdmin)
	empty := mkUser(t, ctx, store, "bob", RoleRegular)

	if err := store.SetUserBookProgress(ctx, u, fx.saBook1, 10, time.Now()); err != nil {
		t.Fatalf("u progress: %v", err)
	}

	if got := smartIDs(t, ctx, store, SmartCollectionFilter{LibraryID: fx.libID, UserID: u, ReadState: "reading"}); !hasExactly(got, fx.saID) {
		t.Fatalf("u reading got %v want [SA]", got)
	}
	// 空用户：无 user_series_progress 行 → 全部未读，reading 为空。
	if got := smartIDs(t, ctx, store, SmartCollectionFilter{LibraryID: fx.libID, UserID: empty, ReadState: "reading"}); len(got) != 0 {
		t.Fatalf("empty user reading want none got %v", got)
	}
	if got := smartIDs(t, ctx, store, SmartCollectionFilter{LibraryID: fx.libID, UserID: empty, ReadState: "unread"}); !hasExactly(got, fx.saID, fx.sbID, fx.scID) {
		t.Fatalf("empty user unread want all got %v", got)
	}
}
