// 业务说明：本文件守卫外部库传输的规划阶段。
//
// 三个缺陷叠在同一段代码上：
//   - 规划被完整跑两遍（HTTP handler 一遍只为决定回 200 还是 202，随后整个丢掉，
//     后台任务再用同一份入参重算一遍）；
//   - 规划内部逐 seriesID 查一次 `SELECT *`（books 表 24 列、含 summary 长文本），
//     选几百个系列就是几百次往返，再乘以上面那个 2；
//   - series_ids 不去重，`[7,7]` 会把系列 7 的每本书规划两遍——用户看到的 missing_books
//     翻倍、任务进度条的分母也是错的，而且同一个文件会被拷两次。

package external

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"manga-manager/internal/database"
)

// countingTransferStore 统计两条查询各被调用了几次。
type countingTransferStore struct {
	database.Store
	perSeriesCalls atomic.Int64
	batchCalls     atomic.Int64
}

func (s *countingTransferStore) ListBooksBySeries(ctx context.Context, seriesID int64) ([]database.Book, error) {
	s.perSeriesCalls.Add(1)
	return s.Store.ListBooksBySeries(ctx, seriesID)
}

func (s *countingTransferStore) ListExternalTransferBooksBySeries(ctx context.Context, seriesIDs []int64) ([]database.ListExternalTransferBooksBySeriesRow, error) {
	s.batchCalls.Add(1)
	return s.Store.ListExternalTransferBooksBySeries(ctx, seriesIDs)
}

// seedTransferSeries 在库里造 n 个系列各一本书，并在磁盘上放出对应文件。
func seedTransferSeries(t *testing.T, store database.Store, lib database.Library, n int) []int64 {
	t.Helper()
	ctx := context.Background()
	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		name := "Series " + string(rune('A'+i))
		seriesPath := filepath.Join(lib.Path, name)
		if err := os.MkdirAll(seriesPath, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		series, err := store.CreateSeries(ctx, database.CreateSeriesParams{
			LibraryID:   lib.ID,
			Name:        name,
			Path:        seriesPath,
			NameInitial: database.SeriesInitial("", name),
		})
		if err != nil {
			t.Fatalf("CreateSeries: %v", err)
		}
		bookPath := filepath.Join(seriesPath, "vol1.cbz")
		if err := os.WriteFile(bookPath, []byte("x"), 0o644); err != nil {
			t.Fatalf("write book: %v", err)
		}
		if _, err := store.UpsertBookByPath(ctx, database.UpsertBookByPathParams{
			SeriesID:       series.ID,
			LibraryID:      lib.ID,
			Name:           "vol1.cbz",
			Path:           bookPath,
			Size:           1,
			FileModifiedAt: time.Now(),
			PageCount:      1,
		}); err != nil {
			t.Fatalf("UpsertBookByPath: %v", err)
		}
		ids = append(ids, series.ID)
	}
	return ids
}

func newReadyTransferSession(t *testing.T, m *Manager, store database.Store, lib database.Library) string {
	t.Helper()
	external := t.TempDir()
	snap, err := m.CreateSession(context.Background(), lib.ID, external, false)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := m.ScanSession(context.Background(), snap.SessionID, nil); err != nil {
		t.Fatalf("ScanSession: %v", err)
	}
	return snap.SessionID
}

func TestPrepareTransferBatchesBookLookups(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	counting := &countingTransferStore{Store: store}
	m := NewManager(counting, time.Hour)

	ids := seedTransferSeries(t, store, lib, 5)
	sessionID := newReadyTransferSession(t, m, store, lib)

	plan, err := m.PrepareTransfer(context.Background(), lib.ID, sessionID, ids)
	if err != nil {
		t.Fatalf("PrepareTransfer: %v", err)
	}
	if plan.MissingBooks != 5 || len(plan.Operations) != 5 {
		t.Fatalf("规划结果不对：missing=%d ops=%d，期望各 5", plan.MissingBooks, len(plan.Operations))
	}

	if got := counting.perSeriesCalls.Load(); got != 0 {
		t.Errorf("逐系列查了 %d 次 —— 选几百个系列就是几百次 SELECT *（24 列含长文本）", got)
	}
	if got := counting.batchCalls.Load(); got != 1 {
		t.Errorf("批量查询调用了 %d 次，期望 1 次", got)
	}
}

// TestPrepareTransferDeduplicatesSeriesIDs：重复 id 不该把同一本书规划两遍。
//
// 后果不只是慢：missing_books 翻倍、任务进度条的分母是错的，而且同一个源文件会被
// 拷贝两次到同一个目标路径。
func TestPrepareTransferDeduplicatesSeriesIDs(t *testing.T) {
	store, lib, _ := newExternalTestStore(t)
	m := NewManager(store, time.Hour)

	ids := seedTransferSeries(t, store, lib, 1)
	sessionID := newReadyTransferSession(t, m, store, lib)

	plan, err := m.PrepareTransfer(context.Background(), lib.ID, sessionID,
		[]int64{ids[0], ids[0], ids[0]})
	if err != nil {
		t.Fatalf("PrepareTransfer: %v", err)
	}
	if plan.SeriesCount != 1 {
		t.Errorf("series_count = %d，期望 1 —— 直接取切片长度会把重复 id 也算进去", plan.SeriesCount)
	}
	if plan.MissingBooks != 1 {
		t.Errorf("missing_books = %d，期望 1 —— 用户看到的数字和进度条分母都会翻倍",
			plan.MissingBooks)
	}
	if len(plan.Operations) != 1 {
		t.Fatalf("Operations 有 %d 条，期望 1 条 —— 同一个源文件会被拷贝多次到同一目标",
			len(plan.Operations))
	}
}

// TestPrepareTransferIgnoresBooksFromOtherLibraries 是隔离性护栏：
// 批量查询是按 series_id 查的，必须仍按 library_id 过滤，否则跨库串数据。
func TestPrepareTransferIgnoresBooksFromOtherLibraries(t *testing.T) {
	store, lib, root := newExternalTestStore(t)
	m := NewManager(store, time.Hour)
	ctx := context.Background()

	ids := seedTransferSeries(t, store, lib, 1)

	// 另一个库里造一个系列，但把它的 id 也一起传进去。
	otherPath := filepath.Join(root, "other-library")
	if err := os.MkdirAll(otherPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	otherLib, err := store.CreateLibrary(ctx, database.CreateLibraryParams{
		Name: "Other", Path: otherPath, ScanMode: "none", ScanInterval: 60, ScanFormats: "cbz",
	})
	if err != nil {
		t.Fatalf("CreateLibrary: %v", err)
	}
	otherIDs := seedTransferSeries(t, store, otherLib, 1)

	sessionID := newReadyTransferSession(t, m, store, lib)
	plan, err := m.PrepareTransfer(ctx, lib.ID, sessionID, append(ids, otherIDs...))
	if err != nil {
		t.Fatalf("PrepareTransfer: %v", err)
	}
	for _, op := range plan.Operations {
		// 用 filepath.Rel 判归属：HasPrefix 不认路径边界（/data/manga2 会被判成在 /data/manga 之内）。
		if rel, err := filepath.Rel(lib.Path, op.SourcePath); err != nil || strings.HasPrefix(rel, "..") {
			t.Errorf("规划里混进了别的库的书：%s —— 传输会把另一个库的文件拷到这个外部库去",
				op.SourcePath)
		}
	}
	if len(plan.Operations) != 1 {
		t.Errorf("Operations 有 %d 条，期望只有本库那 1 本", len(plan.Operations))
	}
}
