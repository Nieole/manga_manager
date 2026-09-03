package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// LIMIT/OFFSET 分页要求 ORDER BY 是全序：末位必须是一个唯一列。少了它，平局行之间的先后
// 由查询计划决定，翻页会在页边界重复或漏掉条目。
var (
	sqlQueryNamePattern  = regexp.MustCompile(`(?m)^-- name: (\S+)`)
	sqlOrderByPattern    = regexp.MustCompile(`(?i)ORDER BY ([^\n]+)`)
	sqlUniqueTailPattern = regexp.MustCompile(`(?i)\bid\s+(ASC|DESC)\s*$`)
)

// TestPagedQueriesOrderByTotalOrder 扫过 sql/query.sql 里每一条带 OFFSET 的查询，要求其分页
// ORDER BY 以唯一列收尾。新增分页查询时漏掉 tie-break，这里先红。
func TestPagedQueriesOrderByTotalOrder(t *testing.T) {
	raw, err := os.ReadFile("../../sql/query.sql")
	if err != nil {
		t.Fatalf("read sql/query.sql failed: %v", err)
	}

	blocks := strings.Split(string(raw), "-- name: ")
	checked := 0
	for _, block := range blocks {
		name := sqlQueryNamePattern.FindStringSubmatch("-- name: " + block)
		if name == nil || !strings.Contains(strings.ToUpper(block), "OFFSET") {
			continue
		}
		orders := sqlOrderByPattern.FindAllStringSubmatch(block, -1)
		if orders == nil {
			t.Errorf("%s 分页却没有 ORDER BY，行序完全由查询计划决定", name[1])
			continue
		}
		checked++
		tail := strings.TrimSpace(orders[len(orders)-1][1])
		if !sqlUniqueTailPattern.MatchString(tail) {
			t.Errorf("%s 的分页 ORDER BY 不是全序（末位不是唯一列）：%s", name[1], tail)
		}
	}
	if checked == 0 {
		t.Fatal("没有扫到任何分页查询，测试自身失效了")
	}
	t.Logf("已检查 %d 条分页查询", checked)
}

// pagedOrderClauseBuilders 是「ORDER BY 由 Go 函数拼出来、SQL 里只留一个 %s」的分页查询。
// 这些站点在源码扫描里看不到真实排序键，只能反过来把每种入参下拼出的子句都过一遍。
// 新增同类查询必须登记到这里，否则 TestHandWrittenPagedQueriesAreTotalOrder 会因数量对不上而红。
var pagedOrderClauseBuilders = map[string]func() []string{
	"智能书架": func() []string {
		out := make([]string, 0)
		for _, field := range seriesSortFieldsForOrderGuard {
			for _, dir := range []string{"asc", "desc"} {
				out = append(out, smartCollectionOrderClause(SmartCollectionFilter{SortByField: field, SortDir: dir}))
			}
		}
		return out
	},
	"资料库列表": func() []string {
		out := make([]string, 0)
		for _, field := range seriesSortFieldsForOrderGuard {
			for _, dir := range []string{"asc", "desc"} {
				out = append(out, seriesSearchOffsetOrderClause(parseSeriesSearchSort(fmt.Sprintf("%s_%s", field, dir))))
			}
		}
		return out
	},
}

var seriesSortFieldsForOrderGuard = []string{"name", "rating", "books", "volumes", "pages", "read", "created", "updated", "favorite", ""}

var (
	pagedLimitOffsetPattern = regexp.MustCompile(`(?i)LIMIT\s+\?\s+OFFSET\s+\?`)
	sqlOrderByHeadPattern   = regexp.MustCompile(`(?i)ORDER BY`)
)

// TestHandWrittenPagedQueriesAreTotalOrder 覆盖不经 sqlc、在 Go 里拼出来的**全部**分页排序。
//
// 做法是扫源码而不是列举已知查询：包里每一处 `LIMIT ? OFFSET ?` 都被找出来，取它前面最近的
// 一条 ORDER BY，要求末位是唯一列。ORDER BY 整条是 `%s`（由 Go 函数拼）的那些站点没法在源码里
// 判断，改为要求它们的数量与 pagedOrderClauseBuilders 登记数一致，再逐一过子句——新增一处却
// 不登记就会在这里红。取「最近的一条 ORDER BY」在子查询自带排序时会看错对象，届时把那条查询
// 改成由 pagedOrderClauseBuilders 登记即可。
func TestHandWrittenPagedQueriesAreTotalOrder(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir failed: %v", err)
	}

	sites, builderSites := 0, 0
	for _, entry := range entries {
		name := entry.Name()
		// query.sql.go 由 sqlc 从 sql/query.sql 生成，那份源文件已由 TestPagedQueriesOrderByTotalOrder 守着。
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "query.sql.go" {
			continue
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s failed: %v", name, err)
		}
		src := string(raw)
		for _, loc := range pagedLimitOffsetPattern.FindAllStringIndex(src, -1) {
			sites++
			heads := sqlOrderByHeadPattern.FindAllStringIndex(src[:loc[0]], -1)
			if heads == nil {
				t.Errorf("%s 的第 %d 处分页前面找不到 ORDER BY，行序完全由查询计划决定", name, sites)
				continue
			}
			clause := strings.TrimSpace(src[heads[len(heads)-1][1]:loc[0]])
			if clause == "%s" {
				builderSites++
				continue
			}
			lines := strings.Split(clause, "\n")
			tail := strings.TrimSpace(lines[len(lines)-1])
			if !sqlUniqueTailPattern.MatchString(tail) {
				t.Errorf("%s 里的手写分页 ORDER BY 不是全序（末位不是唯一列）：%s", name, tail)
			}
		}
	}

	if sites == 0 {
		t.Fatal("没扫到任何手写分页查询，测试自身失效了")
	}
	if builderSites != len(pagedOrderClauseBuilders) {
		t.Fatalf("有 %d 处分页的 ORDER BY 由 Go 拼出，但 pagedOrderClauseBuilders 只登记了 %d 处：新增的那处没人检查它的末位唯一键", builderSites, len(pagedOrderClauseBuilders))
	}
	for label, build := range pagedOrderClauseBuilders {
		for _, clause := range build() {
			if !sqlUniqueTailPattern.MatchString(strings.TrimSpace(clause)) {
				t.Errorf("%s 拼出的 ORDER BY 不是全序：%s", label, clause)
			}
		}
	}
	t.Logf("已检查 %d 处手写分页（其中 %d 处的排序由 Go 拼出）", sites, builderSites)
}

// TestSmartCollectionPagingSurvivesFullTies 让一批系列在排序键与名称上完全平局——此时只剩 s.id
// 能定先后——再逐页翻完，序列须与一次性取数逐位相同：不重复、不漏行。
func TestSmartCollectionPagingSurvivesFullTies(t *testing.T) {
	ctx := context.Background()
	store := newStoreForTest(t)
	lib := newLibraryForCursorTest(t, store)

	const n = 9
	for i := 0; i < n; i++ {
		// 同名、同 book_count、同 rating：三个排序维度全平局。
		series, err := store.CreateSeries(ctx, CreateSeriesParams{
			LibraryID: lib.ID, Name: "Same Title",
			Path: filepath.Join(lib.Path, "same", strconv.Itoa(i)), NameInitial: SeriesInitial("", "Same Title"),
		})
		if err != nil {
			t.Fatalf("create series %d failed: %v", i, err)
		}
		if _, err := store.(*SqlStore).db.ExecContext(ctx,
			`UPDATE series SET book_count = 3, rating = 4.5 WHERE id = ?`, series.ID); err != nil {
			t.Fatalf("set tie columns for %d failed: %v", i, err)
		}
	}

	for _, field := range []string{"name", "rating", "books", "favorite"} {
		for _, dir := range []string{"asc", "desc"} {
			filter := SmartCollectionFilter{LibraryID: lib.ID, SortByField: field, SortDir: dir}

			want, total, err := store.(*SqlStore).SearchSmartCollectionSeries(ctx, filter, n, 0)
			if err != nil {
				t.Fatalf("sort %s_%s: 全量取数失败: %v", field, dir, err)
			}
			if total != n || len(want) != n {
				t.Fatalf("sort %s_%s: total=%d rows=%d, want %d", field, dir, total, len(want), n)
			}

			got := make([]int64, 0, n)
			const pageSize = 2
			for offset := 0; offset < n; offset += pageSize {
				page, _, err := store.(*SqlStore).SearchSmartCollectionSeries(ctx, filter, pageSize, offset)
				if err != nil {
					t.Fatalf("sort %s_%s: offset=%d 取数失败: %v", field, dir, offset, err)
				}
				for _, row := range page {
					got = append(got, row.ID)
				}
			}

			if len(got) != n {
				t.Fatalf("sort %s_%s: 翻页走出 %d 条，want %d: %v", field, dir, len(got), n, got)
			}
			for i := range want {
				if got[i] != want[i].ID {
					wantIDs := make([]int64, len(want))
					for j, r := range want {
						wantIDs[j] = r.ID
					}
					t.Fatalf("sort %s_%s: 翻页序列 %v != 全量序列 %v", field, dir, got, wantIDs)
				}
			}
			seen := map[int64]bool{}
			for _, id := range got {
				if seen[id] {
					t.Fatalf("sort %s_%s: 条目 %d 在翻页中重复: %v", field, dir, id, got)
				}
				seen[id] = true
			}
		}
	}
}
