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

// TestHandWrittenPagedOrderClausesAreTotalOrder 覆盖不经 sqlc、在 Go 里拼出来的两处分页排序。
func TestHandWrittenPagedOrderClausesAreTotalOrder(t *testing.T) {
	fields := []string{"name", "rating", "books", "volumes", "pages", "read", "created", "updated", "favorite", ""}
	dirs := []string{"asc", "desc"}

	for _, field := range fields {
		for _, dir := range dirs {
			smart := smartCollectionOrderClause(SmartCollectionFilter{SortByField: field, SortDir: dir})
			if !sqlUniqueTailPattern.MatchString(strings.TrimSpace(smart)) {
				t.Errorf("智能书架 sort=%q dir=%q 的 ORDER BY 不是全序：%s", field, dir, smart)
			}

			offset := seriesSearchOffsetOrderClause(parseSeriesSearchSort(fmt.Sprintf("%s_%s", field, dir)))
			if !sqlUniqueTailPattern.MatchString(strings.TrimSpace(offset)) {
				t.Errorf("资料库列表 sort=%q dir=%q 的 ORDER BY 不是全序：%s", field, dir, offset)
			}
		}
	}
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
