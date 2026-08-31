// 守监听器派生工作的生命周期与递归注册的容错，两者失效都是静默的。
//
// 去抖后触发的 ScanLibrary / CleanupLibrary 必须可取消并被 Stop 等待，否则优雅关闭返回之后
// 它们还在往一个即将关掉的 store 里写。watchRecursive 遇到不可读的子目录必须继续，否则它之后
// 的整片子树静默失监——用户以为热重载开着，实际大半个库的改动永远发现不了。

package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// newLifecycleWatcher 造一个不挂真实 Scanner 的 watcher，扫描/清理走注入桩。
func newLifecycleWatcher(t *testing.T) *FileWatcher {
	t.Helper()
	fw, err := NewFileWatcher(nil)
	if err != nil {
		t.Fatalf("NewFileWatcher: %v", err)
	}
	return fw
}

// TestStopCancelsAndWaitsForDispatchedWork 是 V3 的行为判据。
//
// 用例走的是真实派发路径：预置 fw.pending / fw.pendingCleanup 为 10 秒前，
// 事件循环的 2 秒 ticker 一到就会越过 5 秒去抖阈值，直接命中那两处派发点，
// 不需要真的产生 fsnotify 事件。
//
// 判据有两条，缺一不可：
//  1. 传下去的 ctx 必须随 Stop 取消（旧代码是 context.Background()，永不取消）；
//  2. Stop 必须等它们退出（旧代码是裸 goroutine，Stop 立刻返回）。
func TestStopCancelsAndWaitsForDispatchedWork(t *testing.T) {
	fw := newLifecycleWatcher(t)

	var (
		scanStarted    = make(chan struct{}, 1)
		cleanupStarted = make(chan struct{}, 1)
		scanDone       atomic.Bool
		cleanupDone    atomic.Bool
		scanSawCancel  atomic.Bool
		cleanSawCancel atomic.Bool
	)

	// 两个桩都阻塞到 ctx 被取消为止：ctx 若永不取消（旧行为），它们就永远不返回。
	fw.scanLibrary = func(ctx context.Context, _ int64, _ string, _ bool) error {
		select {
		case scanStarted <- struct{}{}:
		default:
		}
		<-ctx.Done()
		scanSawCancel.Store(true)
		scanDone.Store(true)
		return ctx.Err()
	}
	fw.cleanupLibrary = func(ctx context.Context, _ int64) error {
		select {
		case cleanupStarted <- struct{}{}:
		default:
		}
		<-ctx.Done()
		cleanSawCancel.Store(true)
		cleanupDone.Store(true)
		return ctx.Err()
	}

	// 扫描与清理分属两个库。同一个库的清理如今接在该库扫描之后串行（见
	// TestCleanupWaitsForSameLibraryScan），而这里要验的是两类派发各自可取消、各自被 Stop
	// 等待——得让它们同时在途，因此分开两个库。
	const scanLibID int64 = 1
	const cleanupLibID int64 = 2
	past := time.Now().Add(-10 * time.Second) // 已越过 5 秒去抖阈值
	fw.mu.Lock()
	fw.libs[filepath.FromSlash("/data/manga")] = scanLibID
	fw.libs[filepath.FromSlash("/data/manga-2")] = cleanupLibID
	fw.pending[scanLibID] = past
	fw.pendingCleanup[cleanupLibID] = past
	fw.mu.Unlock()

	fw.Start(nil)

	// 2 秒 ticker，给足余量。
	for _, c := range []struct {
		name string
		ch   chan struct{}
	}{{"扫描", scanStarted}, {"清理", cleanupStarted}} {
		select {
		case <-c.ch:
		case <-time.After(6 * time.Second):
			t.Fatalf("去抖到期后没有派发%s", c.name)
		}
	}

	if scanDone.Load() || cleanupDone.Load() {
		t.Fatal("桩在 Stop 之前就返回了，用例失去判别力")
	}

	stopped := make(chan struct{})
	go func() {
		fw.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop 没有返回 —— 派生的扫描/清理拿到的 ctx 没有随 Stop 取消（旧代码用的是 context.Background()）")
	}

	// Stop 已返回，此刻两个桩必须都已退出：这正是「优雅关闭之后还有人在写 store」的反面。
	if !scanDone.Load() {
		t.Error("Stop 返回时派生的扫描仍在运行 —— 裸 goroutine 不被停机追踪")
	}
	if !cleanupDone.Load() {
		t.Error("Stop 返回时派生的清理仍在运行 —— 裸 goroutine 不被停机追踪")
	}
	if !scanSawCancel.Load() || !cleanSawCancel.Load() {
		t.Error("派生的工作没有观察到取消信号")
	}

	fw.Stop() // 幂等：重复 Stop 不该 panic（close of closed channel）
}

// TestCleanupLibraryStopsOnCancelledContext 证明取消能真正打断 CleanupLibrary。
//
// 只断言「不再往下删」：取消在这里只可能造成**少删**——seriesHasSurvivingBook 出错
// 保守返回 true、missingSeries 只会被低估、DeleteSeries 拿到已取消的 ctx 直接失败，
// 所以半途停下不会级联删除，更不会丢阅读进度。
func TestCleanupLibraryStopsOnCancelledContext(t *testing.T) {
	_, store, lib, libraryPath := newScannerTestLibrary(t)
	ctx := context.Background()
	s := newFormatTestScanner(t, store)

	dir := filepath.Join(libraryPath, "Series Alpha")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeScannerTestCBZ(filepath.Join(dir, "v1.cbz"), map[string][]byte{"001.png": testPNG1x1}); err != nil {
		t.Fatalf("write cbz: %v", err)
	}
	if err := s.ScanLibrary(ctx, lib.ID, lib.Path, false, nil); err != nil {
		t.Fatalf("ScanLibrary: %v", err)
	}

	// 整个系列目录删掉：正常清理会删系列 + 书。
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("rm: %v", err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err := s.CleanupLibrary(cancelled, lib.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CleanupLibrary 在已取消的 ctx 上返回 %v，期望 context.Canceled —— 取消打不断清理", err)
	}

	books, err := store.ListBooksByLibrary(ctx, lib.ID)
	if err != nil {
		t.Fatalf("ListBooksByLibrary: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("清理被取消后仍删掉了记录（剩 %d 本，期望 1 本）", len(books))
	}
}

// TestWatchRecursiveContinuesPastUnreadableDir 是 V4 的行为判据。
//
// 目录树刻意排成 a / b / c（WalkDir 按名字有序），中间那个不可读。
// 旧实现在 b 上直接 return，导致 c 永远不被监听且调用方只看到一条 Warn。
func TestWatchRecursiveContinuesPastUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上的权限模型不同，chmod 000 造不出不可读目录")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 无视目录权限位，造不出不可读目录")
	}

	fw := newLifecycleWatcher(t)
	t.Cleanup(fw.Stop)

	root := t.TempDir()
	bad := filepath.Join(root, "b")
	for _, name := range []string{"a", "b", "c"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	// 必须在 TempDir 清理之前放开权限，否则 t.TempDir 的 RemoveAll 会失败。
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })

	report := fw.watchRecursive(root)

	if report.OK() {
		t.Fatal("不可读目录竟被判为「全量注册成功」")
	}
	if report.FirstErr == nil {
		t.Error("失败原因没有被保留，运维无从诊断")
	}
	// 核心判据：坏目录**之后**的兄弟目录仍被注册。
	fw.mu.Lock()
	_, watchedC := fw.watched[filepath.Join(root, "c")]
	_, watchedA := fw.watched[filepath.Join(root, "a")]
	_, watchedBad := fw.watched[bad]
	fw.mu.Unlock()

	if !watchedA {
		t.Error("坏目录之前的兄弟目录没被监听")
	}
	if !watchedC {
		t.Error("坏目录之后的兄弟目录没被监听 —— 首错即止让整片子树静默失监")
	}
	if watchedBad {
		t.Error("注册失败的目录仍被记进 watched —— 之后会被 exists 短路永久跳过，再无重试机会")
	}

	// 记账契约：三项划分之和恒等于 Total。
	if got := report.Watched + report.AlreadyWatched + report.Failed; got != report.Total {
		t.Errorf("记账不自洽：watched(%d)+already(%d)+failed(%d)=%d, total=%d",
			report.Watched, report.AlreadyWatched, report.Failed, got, report.Total)
	}
	if report.Total != 4 { // root + a + b + c
		t.Errorf("Total = %d, want 4（root 与三个子目录各计一次，不重复计数）", report.Total)
	}
}

// TestWatchRecursiveIsIdempotent 保证重复注册被记成 AlreadyWatched 而非重复 Add。
func TestWatchRecursiveIsIdempotent(t *testing.T) {
	fw := newLifecycleWatcher(t)
	t.Cleanup(fw.Stop)

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	first := fw.watchRecursive(root)
	if !first.OK() || first.Watched != 2 {
		t.Fatalf("首次注册应监听 2 个目录，实际 %+v", first)
	}
	second := fw.watchRecursive(root)
	if !second.OK() {
		t.Fatalf("重复注册不该报错：%+v", second)
	}
	if second.Watched != 0 || second.AlreadyWatched != 2 {
		t.Fatalf("重复注册应全部记为 AlreadyWatched，实际 %+v", second)
	}
}
