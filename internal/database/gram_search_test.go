// 本文件守卫短关键字搜索的两件事：结果与 instr 全表扫**逐条一致**，
// 以及查询确实走了索引而不是退回全表。
//
// 性能优化最容易悄悄改变语义，而搜索的语义变化是静默的——用户只会觉得「搜不到了」。
// 所以这里的主判据是差分校验，不是计时。

package database

import (
	"context"
	"sort"
	"strings"
	"testing"
)

func seedGramSearchLibrary(t *testing.T, store *SqlStore) Library {
	t.Helper()
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "gram", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}

	seriesNames := []string{
		"海贼王", "进击的巨人", "钢之炼金术师", "海贼王 GRAND", "第一卷合集",
		"One Piece", "ONEPIECE Special", "灌篮高手", "名侦探柯南", "贼王传说",
	}
	for i, name := range seriesNames {
		series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{
			LibraryID: lib.ID, Name: name, Path: "/lib/s" + itoa(i),
		})
		if err != nil {
			t.Fatalf("UpsertSeriesByPath: %v", err)
		}
		for v := 1; v <= 3; v++ {
			bookName := name + " 第" + itoa(v) + "卷"
			if _, err := store.DB().ExecContext(ctx,
				`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
				 VALUES (?, ?, ?, ?, 0, CURRENT_TIMESTAMP, 20)`,
				series.ID, lib.ID, bookName, "/lib/s"+itoa(i)+"/v"+itoa(v)+".cbz"); err != nil {
				t.Fatalf("insert book: %v", err)
			}
		}
	}
	return lib
}

// TestGramSearchMatchesSubstringSemantics 是差分校验：
// 对每个短关键字，gram 索引的命中集必须与 instr(lower(name/title)) 的命中集完全相同。
func TestGramSearchMatchesSubstringSemantics(t *testing.T) {
	store := newExecTxTestStore(t)
	seedGramSearchLibrary(t, store)

	keywords := []string{
		"海", "贼", "海贼", "王", "巨人", "第", "一卷", "第1", "1卷", "卷",
		"On", "ON", "on", "ce", "CE", "PI", "  海贼", "海贼  ", " 第1 ",
		"炼金", "柯南", "高手", "不存在", "zz",
	}

	for _, kw := range keywords {
		t.Run("kw="+kw, func(t *testing.T) {
			expr, ok := gramMatchExpr(kw)
			if !ok {
				t.Skipf("关键字 %q 不走 gram 路径", kw)
			}

			gramIDs := queryIDs(t, store,
				`SELECT s.id FROM series s WHERE `+seriesGramFilter+` ORDER BY s.id`, expr)
			// 基准：instr 语义（gram 索引只覆盖 name/title，故基准也只比这两列）。
			trimmed := strings.TrimSpace(kw)
			instrIDs := queryIDs(t, store,
				`SELECT s.id FROM series s
				 WHERE instr(lower(s.name), lower(?)) > 0
				    OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0
				 ORDER BY s.id`, trimmed, trimmed)

			if !sameInt64Slice(gramIDs, instrIDs) {
				t.Fatalf("series 命中不一致\n gram  = %v\n instr = %v", gramIDs, instrIDs)
			}

			bookGramIDs := queryIDs(t, store,
				`SELECT b.id FROM books b JOIN series s ON s.id = b.series_id
				 WHERE `+bookGramFilter+` ORDER BY b.id`, expr, expr)
			bookInstrIDs := queryIDs(t, store,
				`SELECT b.id FROM books b JOIN series s ON s.id = b.series_id
				 WHERE instr(lower(b.name), lower(?)) > 0
				    OR instr(lower(COALESCE(b.title, '')), lower(?)) > 0
				    OR instr(lower(s.name), lower(?)) > 0
				    OR instr(lower(COALESCE(s.title, '')), lower(?)) > 0
				 ORDER BY b.id`, trimmed, trimmed, trimmed, trimmed)

			if !sameInt64Slice(bookGramIDs, bookInstrIDs) {
				t.Fatalf("books 命中不一致\n gram  = %v\n instr = %v", bookGramIDs, bookInstrIDs)
			}
		})
	}
}

// TestGramSearchSurvivesRename 守卫触发器：改名后索引必须跟着更新，删除后必须清掉。
func TestGramSearchSurvivesRename(t *testing.T) {
	store := newExecTxTestStore(t)
	lib := seedGramSearchLibrary(t, store)
	ctx := context.Background()

	series, err := store.UpsertSeriesByPath(ctx, UpsertSeriesByPathParams{
		LibraryID: lib.ID, Name: "旧名字", Path: "/lib/rename-me",
	})
	if err != nil {
		t.Fatalf("UpsertSeriesByPath: %v", err)
	}

	oldExpr, _ := gramMatchExpr("旧名")
	if got := queryIDs(t, store, `SELECT s.id FROM series s WHERE `+seriesGramFilter, oldExpr); len(got) != 1 {
		t.Fatalf("新建后搜旧名命中 %d 条, want 1", len(got))
	}

	if _, err := store.DB().ExecContext(ctx, `UPDATE series SET name = '新名字' WHERE id = ?`, series.ID); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got := queryIDs(t, store, `SELECT s.id FROM series s WHERE `+seriesGramFilter, oldExpr); len(got) != 0 {
		t.Fatalf("改名后搜旧名仍命中 %d 条 —— 触发器没有更新 gram 索引", len(got))
	}
	newExpr, _ := gramMatchExpr("新名")
	if got := queryIDs(t, store, `SELECT s.id FROM series s WHERE `+seriesGramFilter, newExpr); len(got) != 1 {
		t.Fatalf("改名后搜新名命中 %d 条, want 1", len(got))
	}

	if _, err := store.DB().ExecContext(ctx, `DELETE FROM series WHERE id = ?`, series.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := queryIDs(t, store, `SELECT s.id FROM series s WHERE `+seriesGramFilter, newExpr); len(got) != 0 {
		t.Fatalf("删除后仍命中 %d 条", len(got))
	}
}

// TestGramSearchUsesIndex 断言查询计划是索引扫描，而非全表扫。
//
// 判据用「外层对 books 是主键点查」而不是「不含 SCAN b」：
// 子查询里的 `SCAN book_gram_fts VIRTUAL TABLE ...` 本身就含子串 SCAN b，
// 用否定式子串匹配会在正确实现上也报红。
func TestGramSearchUsesIndex(t *testing.T) {
	store := newExecTxTestStore(t)
	seedGramSearchLibrary(t, store)
	expr, _ := gramMatchExpr("海贼")

	seriesPlan := explainPlanText(t, store,
		`EXPLAIN QUERY PLAN SELECT s.id FROM series s WHERE `+seriesGramFilter, expr)
	if !strings.Contains(strings.ToUpper(seriesPlan), "SERIES_GRAM_FTS") {
		t.Fatalf("series 查询没有用到 gram 索引：\n%s", seriesPlan)
	}

	bookPlan := explainPlanText(t, store,
		`EXPLAIN QUERY PLAN SELECT b.id FROM books b JOIN series s ON s.id = b.series_id WHERE `+bookGramFilter,
		expr, expr)
	upper := strings.ToUpper(bookPlan)
	if !strings.Contains(upper, "SEARCH B USING INTEGER PRIMARY KEY") {
		t.Fatalf("books 外层不是主键点查 —— 仍在全表扫：\n%s", bookPlan)
	}
	// 两条来源都必须走 gram 索引后再 UNION 合并。series 侧在计划里显示为别名 sg，不是表名。
	if !strings.Contains(upper, "SCAN BOOK_GRAM_FTS") {
		t.Fatalf("books 侧没有用到 book_gram_fts：\n%s", bookPlan)
	}
	if !strings.Contains(upper, "SCAN SG VIRTUAL TABLE") {
		t.Fatalf("series 侧没有用到 series_gram_fts（计划里别名为 sg）：\n%s", bookPlan)
	}
	if !strings.Contains(upper, "MERGE (UNION)") {
		t.Fatalf("两条来源没有走 UNION 合并 —— 跨表 OR 形态用不了索引，外层会退回全表扫：\n%s", bookPlan)
	}
}

// TestGramMatchExprEncoding 覆盖编码本身，包括「不能用 Unicode 折叠」这条约束。
func TestGramMatchExprEncoding(t *testing.T) {
	cases := []struct {
		keyword string
		want    string
		ok      bool
	}{
		{"", "", false},
		{"   ", "", false},
		{"abc", "", false}, // >= 3 rune 交给 trigram FTS
		{"海", `"E6B5B7"*`, true},
		{"海贼", `"E6B5B7E8B4BC"`, true},
		{"AB", `"6162"`, true},             // ASCII 折叠成小写
		{"  海贼  ", `"E6B5B7E8B4BC"`, true}, // 必须先 TrimSpace，否则恒定 0 结果
	}
	for _, tc := range cases {
		got, ok := gramMatchExpr(tc.keyword)
		if ok != tc.ok {
			t.Errorf("gramMatchExpr(%q) ok = %v, want %v", tc.keyword, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("gramMatchExpr(%q) = %q, want %q", tc.keyword, got, tc.want)
		}
	}

	// 索引侧用的是 SQL 的 lower()（只折叠 ASCII）。若这里改用 strings.ToLower，
	// 土耳其语 İ、希腊语 Σ 之类会被 Unicode 规则折叠，与索引对不上并静默搜不到。
	if asciiLower("İ") != "İ" {
		t.Error("asciiLower 折叠了非 ASCII 字符 —— 会与 SQL 侧的 lower() 产生分歧")
	}
	if asciiLower("ABZ") != "abz" {
		t.Error("asciiLower 没有折叠 ASCII 大写")
	}
}

func queryIDs(t *testing.T, store *SqlStore, query string, args ...interface{}) []int64 {
	t.Helper()
	rows, err := store.DB().QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("query: %v\nSQL: %s", err, query)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sameInt64Slice(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
