// 本文件守卫 AI 候选采样的两条性质：不做全表随机排序、按当前用户判「已读过半」。

package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func seedCandidateSeries(t *testing.T, store *SqlStore, n int, booksPerSeries int) (Library, []int64) {
	t.Helper()
	ctx := context.Background()
	lib, err := store.CreateLibrary(ctx, CreateLibraryParams{
		Name: "ai", Path: t.TempDir(), ScanMode: "none", ScanInterval: 60,
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		name := "series" + itoa(i)
		res, err := store.DB().ExecContext(ctx,
			`INSERT INTO series (library_id, name, path, summary, book_count, total_pages)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			lib.ID, name, "/lib/"+name, "a summary", booksPerSeries, booksPerSeries*10)
		if err != nil {
			t.Fatalf("insert series: %v", err)
		}
		sid, _ := res.LastInsertId()
		ids = append(ids, sid)
		for j := 0; j < booksPerSeries; j++ {
			bn := name + "-v" + itoa(j)
			if _, err := store.DB().ExecContext(ctx,
				`INSERT INTO books (series_id, library_id, name, path, size, file_modified_at, page_count)
				 VALUES (?, ?, ?, ?, 0, CURRENT_TIMESTAMP, 10)`,
				sid, lib.ID, bn, "/lib/"+bn+".cbz"); err != nil {
				t.Fatalf("insert book: %v", err)
			}
		}
	}
	return lib, ids
}

// TestSampleCandidateSeriesUsesPrimaryKeyProbes 断言查询计划命中主键范围探针、没有排序物化。
func TestSampleCandidateSeriesUsesPrimaryKeyProbes(t *testing.T) {
	store := newExecTxTestStore(t)
	seedCandidateSeries(t, store, 30, 2)

	plan := explainPlanText(t, store,
		"EXPLAIN QUERY PLAN "+candidateSeriesSelect+" AND s.id >= ?2 AND s.id < ?3 ORDER BY s.id LIMIT 1",
		int64(0), int64(1), int64(100))

	upper := strings.ToUpper(plan)
	if !strings.Contains(upper, "SEARCH S USING INTEGER PRIMARY KEY") {
		t.Fatalf("分层探针没走主键范围查找：\n%s", plan)
	}
	if strings.Contains(upper, "USE TEMP B-TREE FOR ORDER BY") {
		t.Fatalf("查询计划里仍有排序物化：\n%s", plan)
	}
}

func explainPlanText(t *testing.T, store *SqlStore, query string, args ...interface{}) string {
	t.Helper()
	rows, err := store.DB().QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN QUERY PLAN: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan: %v", err)
		}
		lines = append(lines, detail)
	}
	return strings.Join(lines, "\n")
}

// TestSampleCandidateSeriesRespectsUserProgress 是正确性那一半。
//
// 「排除已读过半」必须按当前用户读取 user_book_progress，不得依赖 books.last_read_page——
// 那是多用户改造后已停写的全局列，读它会让条件恒为真，读完的作品照样被推荐回来。
func TestSampleCandidateSeriesRespectsUserProgress(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	_, seriesIDs := seedCandidateSeries(t, store, 3, 4)

	user, err := store.CreateUser(ctx, CreateUserParams{
		Username: "reader", PasswordHash: "x", Role: "regular", DisplayName: "R",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// 把第一个系列的全部 4 本标记为已读（远超一半）。
	var bookIDs []int64
	rows, err := store.DB().QueryContext(ctx, `SELECT id FROM books WHERE series_id = ?`, seriesIDs[0])
	if err != nil {
		t.Fatalf("query books: %v", err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		bookIDs = append(bookIDs, id)
	}
	rows.Close()
	if err := store.SetUserBooksReadState(ctx, user.ID, bookIDs, true, time.Now()); err != nil {
		t.Fatalf("SetUserBooksReadState: %v", err)
	}

	got, err := store.SampleCandidateSeriesForAI(ctx, user.ID, 20)
	if err != nil {
		t.Fatalf("SampleCandidateSeriesForAI: %v", err)
	}
	for _, c := range got {
		if c.ID == seriesIDs[0] {
			t.Fatalf("已读完的系列 %d 仍被选为候选 —— 过滤读的还是全局进度列", c.ID)
		}
	}
	if len(got) != 2 {
		t.Fatalf("候选数 = %d, want 2（三个系列里读完一个）", len(got))
	}
}

// TestSampleCandidateSeriesRespectsLimitAndVaries 覆盖「取够」与「不是每次都一样」。
func TestSampleCandidateSeriesRespectsLimitAndVaries(t *testing.T) {
	store := newExecTxTestStore(t)
	ctx := context.Background()
	seedCandidateSeries(t, store, 60, 1)

	first, err := store.SampleCandidateSeriesForAI(ctx, 0, 20)
	if err != nil {
		t.Fatalf("SampleCandidateSeriesForAI: %v", err)
	}
	if len(first) != 20 {
		t.Fatalf("采样数 = %d, want 20", len(first))
	}

	// 采样应当是随机的：连采若干次，不该每次都得到同一批 id。
	same := 0
	for i := 0; i < 6; i++ {
		next, err := store.SampleCandidateSeriesForAI(ctx, 0, 20)
		if err != nil {
			t.Fatalf("SampleCandidateSeriesForAI: %v", err)
		}
		if sameIDSet(first, next) {
			same++
		}
	}
	if same == 6 {
		t.Fatal("连续 6 次采样得到完全相同的候选集 —— 采样没有随机性，推荐会一直是同一批作品")
	}
}

func sameIDSet(a, b []CandidateSeriesForAI) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[int64]struct{}, len(a))
	for _, c := range a {
		set[c.ID] = struct{}{}
	}
	for _, c := range b {
		if _, ok := set[c.ID]; !ok {
			return false
		}
	}
	return true
}
