// 业务说明：本文件守卫封面路径遍历的**流式**性质与查询形态。
//
// 缩略图清理是唯一调用方，它只需要把路径折进一个 map。全量读进切片再丢掉，是纯粹的
// 内存浪费；而 DISTINCT / UNION 会引入 temp B-tree 把结果整份物化，同样是白付的。

package database

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestReferencedCoverPathsQueryShape 把「不物化」钉进查询本身。
// DISTINCT 与 UNION 都会让 SQLite 建 temp B-tree；UNION ALL 才是边扫边出。
func TestReferencedCoverPathsQueryShape(t *testing.T) {
	if !strings.Contains(referencedCoverPathsQuery, "UNION ALL") {
		t.Error("查询应当用 UNION ALL —— 调用方用 map 去重，SQL 侧再去一遍纯属白付")
	}
	if strings.Contains(strings.ToUpper(referencedCoverPathsQuery), "DISTINCT") {
		t.Error("查询里出现了 DISTINCT，会引入 temp B-tree 把结果整份物化")
	}

	store := newExecTxTestStore(t)
	plan := explainCoverPathsPlan(t, store)
	if strings.Contains(strings.ToUpper(plan), "TEMP B-TREE") {
		t.Fatalf("查询计划里出现了 TEMP B-TREE，结果被整份物化了：\n%s", plan)
	}
}

func explainCoverPathsPlan(t *testing.T, store *SqlStore) string {
	t.Helper()
	rows, err := store.DB().QueryContext(context.Background(), "EXPLAIN QUERY PLAN "+referencedCoverPathsQuery)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
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
	return strings.Join(lines, "\n")
}

// TestForEachReferencedCoverPathStreams 证明回调**发生在游标打开期间**。
//
// 判据是连接池的 InUse：流式实现此刻 rows 还开着、连接处于占用；
// 而缓冲实现（先读完再遍历）在回调时早已 rows.Close() 把连接还回了池子。
// 这条断言不依赖计时或分配量，且正好测的是 cover_paths.go 里那条契约
// （回调期间占着连接与读快照，所以里面不能等暂停闸）。
func TestForEachReferencedCoverPathStreams(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	seedCoverPaths(t, store, 200)

	var inUseAtFirstCallback int
	var seen int
	if err := store.ForEachReferencedCoverPath(ctx, func(string) error {
		if seen == 0 {
			inUseAtFirstCallback = store.DB().Stats().InUse
		}
		seen++
		return nil
	}); err != nil {
		t.Fatalf("ForEachReferencedCoverPath: %v", err)
	}

	if seen == 0 {
		t.Fatal("一条路径都没遍历到，用例前提不成立")
	}
	if inUseAtFirstCallback != 1 {
		t.Fatalf("首次回调时 InUse = %d, want 1 —— 说明结果已被读完并归还连接，不是流式", inUseAtFirstCallback)
	}
}

// TestForEachReferencedCoverPathAbortsOnError 保证回调返回错误能中止遍历并原样上抛。
func TestForEachReferencedCoverPathAbortsOnError(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	seedCoverPaths(t, store, 50)

	sentinel := errors.New("stop here")
	calls := 0
	err := store.ForEachReferencedCoverPath(ctx, func(string) error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Fatalf("回调被调用了 %d 次, want 1 —— 出错后没有立刻中止", calls)
	}
}

func seedCoverPaths(t *testing.T, store *SqlStore, n int) {
	t.Helper()
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "covers", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
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
	for i := 0; i < n; i++ {
		name := "vol" + strings.Repeat("0", 3-len(itoa(i))) + itoa(i)
		if _, err := store.DB().ExecContext(ctx,
			`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, cover_path)
			 VALUES (?, ?, ?, ?, 0, CURRENT_TIMESTAMP, ?)`,
			series.ID, lib.ID, name, "/lib/"+name+".cbz", "ab/"+name+".webp"); err != nil {
			t.Fatalf("insert book: %v", err)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
