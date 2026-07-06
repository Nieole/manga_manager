// 业务说明：本文件是查询计划(EXPLAIN QUERY PLAN)回归测试，锁定资源库全景相关热查询在大数据量下
// 真正命中预期索引，防止后续重构悄悄让某条路径退化为全表扫描 / filesort。
// 用真实 Migrate 建库 + 少量数据，断言规划器输出里出现预期索引名（数据量小不影响 EXPLAIN 的结构判断）。

package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// explainPlan 返回某查询的 EXPLAIN QUERY PLAN 明细拼接串（每行一个 detail）。
func explainPlan(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain failed: %v\nquery: %s", err, query)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows err: %v", err)
	}
	return strings.Join(lines, "\n")
}

// TestFranchiseTargetIndexUsed 验证 series_relations(target_series_id) 索引让反向边查询与递归 CTE 的
// 反向支走索引 seek，而非全表扫描（这是关系图谱在大库下的主要成本来源之一）。
func TestFranchiseTargetIndexUsed(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	db := store.(*SqlStore).db

	// 反向关系查询：WHERE target_series_id = ?
	reverse := explainPlan(t, db,
		`SELECT id, source_series_id FROM series_relations WHERE target_series_id = ?`, 1)
	if !strings.Contains(reverse, "idx_series_relations_target") {
		t.Fatalf("reverse relation lookup should use idx_series_relations_target, got plan:\n%s", reverse)
	}

	// 递归 CTE 反向支：JOIN ... ON sr.target_series_id = c.id
	cte := explainPlan(t, db, `
		WITH RECURSIVE connected(id) AS (
			SELECT CAST(? AS INTEGER)
			UNION
			SELECT target_series_id FROM series_relations sr JOIN connected c ON sr.source_series_id = c.id
			UNION
			SELECT source_series_id FROM series_relations sr JOIN connected c ON sr.target_series_id = c.id
		)
		SELECT id FROM connected`, 1)
	if !strings.Contains(cte, "idx_series_relations_target") {
		t.Fatalf("recursive CTE backward edge should use idx_series_relations_target, got plan:\n%s", cte)
	}
}

// TestSeriesListBrowsePlanUsesLibraryIndex 用较大数据量 + ANALYZE 断言真实的完整 buildSeriesSearchQuery
// （含 last_read_page 关联子查询、sc/ss 两个 LEFT JOIN）在默认排序下仍走 series(library_id, sortcol, ...)
// 覆盖索引、不产生 TEMP B-TREE 排序——cmd/queryplan 用的是简化查询，这里守护真实查询的计划。
// 同时观察 UserID>0（进度来源 user_series_progress）readState 筛选路径的计划。
func TestSeriesListBrowsePlanUsesLibraryIndex(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "plan2.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ss := store.(*SqlStore)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "L", Path: filepath.Join(t.TempDir(), "l"), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("lib: %v", err)
	}
	// 批量插入 ~2000 系列，让规划器像生产库那样按代价而非小表启发式选计划。
	tx, err := ss.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("tx: %v", err)
	}
	q := ss.Queries.WithTx(tx)
	for i := 0; i < 2000; i++ {
		s, err := q.CreateSeries(ctx, CreateSeriesParams{
			LibraryID: lib.ID, Name: initialName(i), Path: filepath.Join(lib.Path, initialName(i)),
			NameInitial: string(rune('A' + i%26)),
		})
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("series %d: %v", i, err)
		}
		if _, err := q.CreateBook(ctx, CreateBookParams{
			SeriesID: s.ID, LibraryID: lib.ID, Name: initialName(i) + ".cbz",
			Path: filepath.Join(lib.Path, initialName(i)+".cbz"), Size: 1, FileModifiedAt: time.Now(), PageCount: 100,
		}); err != nil {
			_ = tx.Rollback()
			t.Fatalf("book %d: %v", i, err)
		}
		if err := q.RefreshSeriesStats(ctx, s.ID); err != nil {
			_ = tx.Rollback()
			t.Fatalf("refresh %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := ss.db.ExecContext(ctx, `ANALYZE`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	assemble := func(f SeriesListFilters, sortBy string) string {
		base, _, whereClause, _, _, _ := buildSeriesSearchQuery(lib.ID, f)
		spec := parseSeriesSearchSort(sortBy)
		q := base + whereClause + " ORDER BY " + seriesSearchOffsetOrderClause(spec) + " LIMIT 50 OFFSET 0"
		return strings.ReplaceAll(q, "?", "1") // 占位符全替换为常量，便于无参 EXPLAIN
	}

	// 常见的降序排序(updated/created/rating/books/pages/favorite)生成 `<expr> DESC, s.name ASC, s.id ASC`
	// 混合方向的 ORDER BY，全 ASC 的复合索引无法满足，会对整库 filesort。方向匹配的 DESC 复合索引应消除它。
	for _, sortBy := range []string{"updated_desc", "created_desc", "name_asc", "rating_desc", "books_desc", "pages_desc", "favorite_desc"} {
		plan := explainPlan(t, ss.db, assemble(SeriesListFilters{}, sortBy))
		t.Logf("global browse sort=%s plan:\n%s", sortBy, plan)
		if strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY") {
			t.Errorf("global browse sort=%s filesorts the whole library (mixed-direction ORDER BY needs a DESC-matching index), got:\n%s", sortBy, plan)
		}
		if !strings.Contains(plan, "idx_series_library_") {
			t.Errorf("global browse sort=%s should use a series(library_id,...) index, got:\n%s", sortBy, plan)
		}
	}

	// UserID>0 的 readState 筛选：主表 series 仍应走 library 索引；进度来源 user_series_progress 应按
	// 主键点查而非 SCAN。记录计划以便回归观察（rank 3 的结构性诊断依据）。
	userPlan := explainPlan(t, ss.db, assemble(SeriesListFilters{UserID: 1, ReadState: "reading"}, "name_asc"))
	t.Logf("per-user readState=reading plan:\n%s", userPlan)
	if strings.Contains(userPlan, "SCAN user_series_progress") {
		t.Errorf("per-user progress source should be probed by PK, not scanned, got:\n%s", userPlan)
	}
}

// TestSeriesListKeywordFTS 验证库内关键字筛选：>=3 rune 走 series_search_fts(命中 name 与 title 子串、
// 与 instr 结果等价、EXPLAIN 用 FTS 而非全表扫)，<3 rune 回退 instr。
func TestSeriesListKeywordFTS(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "kw.db")
	if err := Migrate(dbPath); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ss := store.(*SqlStore)
	ctx := context.Background()

	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "L", Path: filepath.Join(t.TempDir(), "l"), ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("lib: %v", err)
	}
	mk := func(name, title string) {
		p := CreateSeriesParams{LibraryID: lib.ID, Name: name, Path: filepath.Join(lib.Path, name), NameInitial: name[:1]}
		if title != "" {
			p.Title = sql.NullString{String: title, Valid: true}
		}
		if _, err := store.CreateSeries(ctx, p); err != nil {
			t.Fatalf("series %s: %v", name, err)
		}
	}
	mk("Berserk", "")
	mk("Bereft Souls", "")
	mk("Naruto", "Berserk Gaiden") // 标题含 Berserk
	mk("One Piece", "")

	names := func(keyword string) map[string]bool {
		rows, _, err := store.SearchSeriesPaged(ctx, lib.ID, SeriesListFilters{Keyword: keyword}, 50, 0, "name_asc")
		if err != nil {
			t.Fatalf("search %q: %v", keyword, err)
		}
		m := map[string]bool{}
		for _, r := range rows {
			m[r.Name] = true
		}
		return m
	}

	// >=3 rune 走 FTS："ber" 子串命中 Berserk、Bereft(name) 与 Naruto(title Berserk Gaiden)。
	got := names("ber")
	if !got["Berserk"] || !got["Bereft Souls"] || !got["Naruto"] {
		t.Fatalf("keyword 'ber' should match Berserk/Bereft/Naruto(title), got %v", got)
	}
	if got["One Piece"] {
		t.Fatalf("keyword 'ber' should not match One Piece, got %v", got)
	}

	// <3 rune 回退 instr："on" 命中 One Piece。
	got2 := names("on")
	if !got2["One Piece"] || len(got2) != 1 {
		t.Fatalf("keyword 'on' (instr fallback) should match only One Piece, got %v", got2)
	}

	// EXPLAIN：>=3 rune 关键字应命中 series_search_fts，而非对 series 全表扫做 instr。
	base, _, whereClause, _, _, _ := buildSeriesSearchQuery(lib.ID, SeriesListFilters{Keyword: "ber"})
	full := base + whereClause + " ORDER BY s.name ASC, s.id ASC LIMIT 50 OFFSET 0"
	plan := explainPlan(t, ss.db, strings.ReplaceAll(full, "?", "'\"ber\"'"))
	if !strings.Contains(plan, "series_search_fts") {
		t.Fatalf("keyword FTS browse should use series_search_fts, got:\n%s", plan)
	}
}

func initialName(i int) string {
	// 稳定的可排序名字：字母前缀 + 零填充序号，保证 name 排序确定。
	return string(rune('A'+i%26)) + "-" + zeroPad(i)
}

func zeroPad(i int) string {
	s := ""
	for _, d := range []int{1000, 100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}
