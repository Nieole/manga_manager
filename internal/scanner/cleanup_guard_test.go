// 业务说明：本文件守卫 CleanupLibrary 的两道「防误删」闸门与 watcher 的端到端事件通路。
//
// CleanupLibrary 是全仓破坏性最强的一段代码：它按目录是否存在删除系列，而 DELETE 会
// CASCADE 到书籍与每用户阅读进度。两道闸门保护的都是同一类事故——存储离线、盘符漂移、
// UNC 断连时库内所有路径都会「看起来不存在」，一次自动清理就能抹掉整库的阅读记录。
// 这两道闸门此前没有任何用例，改动时无从知道它们还在不在。

package scanner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedSeriesDirs 在库根下建 n 个系列目录，各放一本 cbz。
func seedSeriesDirs(t *testing.T, libraryPath string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		dir := filepath.Join(libraryPath, "Series"+string(rune('A'+i)))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := writeScannerTestCBZ(filepath.Join(dir, "v1.cbz"),
			map[string][]byte{"001.png": testPNG1x1}); err != nil {
			t.Fatalf("write cbz: %v", err)
		}
	}
}

// TestCleanupAbortsWhenLibraryRootIsUnreachable：库根不可达时必须整体中止。
//
// 这是存储离线的典型形态（外置盘拔了、NAS 掉线）。此时库内每个 series.Path 都会
// os.IsNotExist，逐个删下去就是整库连同阅读进度一起蒸发。
func TestCleanupAbortsWhenLibraryRootIsUnreachable(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()
	s := newFormatTestScanner(t, store)

	seedSeriesDirs(t, libraryPath, 2)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	before, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListBooksByLibrary: %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("初始应有 2 本，实际 %d", len(before))
	}

	// 整个库根消失：模拟外置盘被拔掉。
	if err := os.RemoveAll(libraryPath); err != nil {
		t.Fatalf("rm: %v", err)
	}

	err = s.CleanupLibrary(ctx, lib.ID)
	if err == nil {
		t.Fatal("库根不可达时 CleanupLibrary 返回了成功 —— 存储离线会被当成「用户删了全部文件」")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("错误信息未指出库根不可达，运维无从判断该不该慌：%v", err)
	}

	after, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListBooksByLibrary: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("库根不可达时仍删掉了记录（%d -> %d）—— 阅读进度随 CASCADE 一起没了",
			len(before), len(after))
	}
}

// TestCleanupCircuitBreakerStopsMassDeletion：待删占比超过阈值时熔断。
//
// 覆盖的是「库根还在、但下面的内容整片不可见」的形态——挂载点空挂、权限被改、
// 同步盘还没拉回来。此时逐个删也是整库蒸发，只是根目录探测拦不住。
func TestCleanupCircuitBreakerStopsMassDeletion(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()
	s := newFormatTestScanner(t, store)

	const seriesCount = 4
	seedSeriesDirs(t, libraryPath, seriesCount)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}
	before, _ := store.ListBooksByLibrary(ctx, lib.ID)
	if len(before) != seriesCount {
		t.Fatalf("初始应有 %d 本，实际 %d", seriesCount, len(before))
	}

	// 删掉 3/4 的系列目录（> 50% 阈值），但保留库根本身。
	for i := 0; i < 3; i++ {
		if err := os.RemoveAll(filepath.Join(libraryPath, "Series"+string(rune('A'+i)))); err != nil {
			t.Fatalf("rm: %v", err)
		}
	}

	err := s.CleanupLibrary(ctx, lib.ID)
	if err == nil {
		t.Fatal("待删占比 75% 仍照删不误 —— 熔断没有生效")
	}
	if !strings.Contains(err.Error(), "cleanup aborted") {
		t.Errorf("错误信息未指出是熔断：%v", err)
	}
	after, _ := store.ListBooksByLibrary(ctx, lib.ID)
	if len(after) != len(before) {
		t.Fatalf("熔断后仍删掉了记录（%d -> %d）—— 熔断必须在删之前，而不是删一半再喊停",
			len(before), len(after))
	}
}

// TestCleanupStillRemovesMinorityMissingSeries：熔断不能把正常清理也一并挡掉。
//
// 没有这条，把阈值误设成 0 之类的改动会悄无声息地让清理彻底失效，幽灵记录永久堆积。
func TestCleanupStillRemovesMinorityMissingSeries(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()
	s := newFormatTestScanner(t, store)

	const seriesCount = 4
	seedSeriesDirs(t, libraryPath, seriesCount)
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// 只删 1/4（低于阈值），应当被正常清理。
	if err := os.RemoveAll(filepath.Join(libraryPath, "SeriesA")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := s.CleanupLibrary(ctx, lib.ID); err != nil {
		t.Fatalf("少数系列缺失时清理不该失败：%v", err)
	}

	after, _ := store.ListBooksByLibrary(ctx, lib.ID)
	if len(after) != seriesCount-1 {
		t.Fatalf("清理后剩 %d 本，期望 %d —— 熔断把正常清理也挡掉了，幽灵记录会永久堆积",
			len(after), seriesCount-1)
	}
}

// TestWatcherEventLoopSchedulesScanOnNewFile 是 watcher 的端到端判据：
// 真实的 fsnotify 事件 → 定位所属库 → 记入 pending。
//
// 事件循环此前完全没有用例，格式过滤、库归属判定、去抖这三段逻辑全靠人眼守。
func TestWatcherEventLoopSchedulesScanOnNewFile(t *testing.T) {
	fw := newLifecycleWatcher(t)
	t.Cleanup(fw.Stop)

	root := t.TempDir()
	const libID int64 = 7
	if err := fw.WatchLibrary(libID, root, "cbz"); err != nil {
		t.Fatalf("WatchLibrary: %v", err)
	}
	fw.Start(nil)

	// 先写一个被 scan_formats 排除的格式：不该排期。
	if err := os.WriteFile(filepath.Join(root, "ignored.cbr"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 再写一个白名单内的：应当排期。
	if err := os.WriteFile(filepath.Join(root, "new.cbz"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var scheduled bool
	for time.Now().Before(deadline) {
		fw.mu.Lock()
		_, scheduled = fw.pending[libID]
		fw.mu.Unlock()
		if scheduled {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !scheduled {
		t.Fatal("新增 .cbz 之后库没有被排期重扫 —— 热重载整条通路是断的")
	}
}

// TestWatcherEventLoopIgnoresFilteredFormat 单独钉住格式过滤这一段：
// 只写被排除的格式时，不该有任何排期。
func TestWatcherEventLoopIgnoresFilteredFormat(t *testing.T) {
	fw := newLifecycleWatcher(t)
	t.Cleanup(fw.Stop)

	root := t.TempDir()
	const libID int64 = 8
	if err := fw.WatchLibrary(libID, root, "cbz"); err != nil {
		t.Fatalf("WatchLibrary: %v", err)
	}
	fw.Start(nil)

	if err := os.WriteFile(filepath.Join(root, "ignored.cbr"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// 给事件充分的传递时间；这里等的是「什么都不该发生」，所以只能靠固定时长。
	time.Sleep(500 * time.Millisecond)

	fw.mu.Lock()
	_, scheduled := fw.pending[libID]
	fw.mu.Unlock()
	if scheduled {
		t.Fatal("被 scan_formats 排除的格式仍触发了重扫 —— 用户勾了「只扫 cbz」却照样为 rar 重扫全库")
	}
}
