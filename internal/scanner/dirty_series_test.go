// 守「系列读模型的刷新失败不会被静默丢弃」：刷新失败或扫描被取消时，脏标记必须留着。
//
// 丢了标记，那些系列的 book_count/total_pages 与 series_stats 就永久停在旧值，且没有任何后台
// 自愈路径——启动回填被 user_version 门控，api 侧也没有重算系列统计的维护任务，只有该系列
// 今后再次发生文件变动才会被纠正。

package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
)

// flakyRefreshStore 让 RefreshSeriesDerivedData 前 n 次调用失败，之后恢复正常。
type flakyRefreshStore struct {
	database.Store
	remainingFailures atomic.Int64
	calls             atomic.Int64
}

func (f *flakyRefreshStore) RefreshSeriesDerivedData(ctx context.Context, seriesID int64) error {
	f.calls.Add(1)
	if f.remainingFailures.Add(-1) >= 0 {
		return errors.New("simulated transient database failure")
	}
	return f.Store.RefreshSeriesDerivedData(ctx, seriesID)
}

func TestScanReportsStaleSeriesStatsInsteadOfSilentlyDropping(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name          string
		failures      int64
		wantStale     bool
		wantBookCount int64
	}{
		{
			// 刷新持续失败：脏标记被保留，扫描收尾把「还有多少系列统计不准」报出来。
			name: "刷新失败必须变成可见的指标", failures: 1000, wantStale: true, wantBookCount: 0,
		},
		{
			name: "刷新成功时不报陈旧", failures: 0, wantStale: false, wantBookCount: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, freshStore, freshLib, freshPath := newScannerTestLibrary(t)
			freshSeriesDir := filepath.Join(freshPath, "Series Alpha")
			for _, name := range []string{"Vol 01.cbz", "Vol 02.cbz"} {
				if err := writeScannerTestCBZ(filepath.Join(freshSeriesDir, name), map[string][]byte{
					"001.png": testPNG1x1, "002.png": testPNG1x1,
				}); err != nil {
					t.Fatalf("write cbz failed: %v", err)
				}
			}

			flaky := &flakyRefreshStore{Store: freshStore}
			flaky.remainingFailures.Store(tc.failures)

			cfg := &config.Config{}
			cfg.Scanner.Workers = 1
			cfg.Scanner.ScanProfile = config.ScanProfileFast
			cfg.Cache.Dir = t.TempDir()
			s := NewScanner(flaky, config.NewManager(cfg))

			var report ScanMetricsReport
			s.SetScanMetricsCallback(func(r ScanMetricsReport) { report = r })

			if err := s.ScanLibrary(ctx, freshLib.ID, freshLib.Path, false); err != nil {
				t.Fatalf("scan library failed: %v", err)
			}

			if tc.wantStale && report.StaleSeriesStats == 0 {
				t.Fatal("刷新失败却没有报告 stale_series_stats —— 统计不一致又变回静默了")
			}
			if !tc.wantStale && report.StaleSeriesStats != 0 {
				t.Fatalf("刷新成功却报告了 %d 个陈旧系列", report.StaleSeriesStats)
			}

			seriesList, err := freshStore.ListSeriesByLibraryLite(ctx, freshLib.ID)
			if err != nil {
				t.Fatalf("ListSeriesByLibraryLite: %v", err)
			}
			if len(seriesList) != 1 {
				t.Fatalf("期望 1 个系列，得到 %d 个", len(seriesList))
			}
			if got := seriesList[0].BookCount; got != tc.wantBookCount {
				t.Fatalf("book_count = %d, want %d", got, tc.wantBookCount)
			}
		})
	}
}

// TestScanDrainsDirtySeriesAfterCancel 覆盖取消路径。
//
// 取消要停的是「继续扫描」，不是「把已经写进去的东西算对」：收尾刷新用 WithoutCancel 派生的
// ctx，否则 ctx 已 Done 会让这次刷新立刻失败，而这之后再没有人会来刷这些脏标记了。
func TestScanDrainsDirtySeriesAfterCancel(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)

	seriesDir := filepath.Join(libraryPath, "Series Alpha")
	for i := 0; i < 6; i++ {
		name := filepath.Join(seriesDir, "Vol 0"+string(rune('1'+i))+".cbz")
		if err := writeScannerTestCBZ(name, map[string][]byte{"001.png": testPNG1x1}); err != nil {
			t.Fatalf("write cbz failed: %v", err)
		}
	}

	cfg := &config.Config{}
	cfg.Scanner.Workers = 1
	cfg.Scanner.ScanProfile = config.ScanProfileFast
	cfg.Cache.Dir = t.TempDir()
	s := NewScanner(store, config.NewManager(cfg))

	ctx, cancel := context.WithCancel(context.Background())
	// 用 batch 回调在第一批入库后立刻取消，模拟「扫描中途被用户取消」。
	s.SetBatchCallback(func(string) { cancel() })
	_ = s.ScanLibrary(ctx, lib.ID, lib.Path, false)

	seriesList, err := store.ListSeriesByLibraryLite(context.Background(), lib.ID)
	if err != nil {
		t.Fatalf("ListSeriesByLibraryLite: %v", err)
	}
	if len(seriesList) == 0 {
		t.Skip("取消得太早，没有系列入库，用例前提不成立")
	}
	// 已经写进去的书必须被统计到：BookCount 为 0 说明收尾刷新被取消的 ctx 打掉了。
	if got := seriesList[0].BookCount; got == 0 {
		books, _ := store.ListBooksByLibrary(context.Background(), lib.ID)
		if len(books) > 0 {
			t.Fatalf("库里已有 %d 本书，但 series.book_count 仍是 0 —— 取消后的收尾刷新没有执行", len(books))
		}
	}
}
