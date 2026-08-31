// 守改名重连在 watch 模式下不被库清理抢跑。
//
// 改名产生 Rename(旧名)+Create(新名) 两个事件，分别排期清理与扫描。清理只做 stat，比要走完
// 整棵树、末尾才写改名重连的扫描快得多，抢先跑就把旧行当成「文件已消失」删掉，挂在这本书上的
// 记录随级联一起没。两条排期由不同事件刷新、会分叉，因此判据是「重连了结了没」而非「本轮谁到期」。

package scanner

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestCleanupWaitsForSameLibraryScan 判据：同一个库的扫描与清理同轮到期时，清理不得在扫描
// 返回之前开始。扫描桩被 gate 卡住期间若已经看到清理启动，就是旧的并发派发行为。
func TestCleanupWaitsForSameLibraryScan(t *testing.T) {
	fw := newLifecycleWatcher(t)

	scanStarted := make(chan struct{}, 1)
	cleanupStarted := make(chan struct{}, 1)
	scanGate := make(chan struct{})

	fw.scanLibrary = func(ctx context.Context, _ int64, _ string, _ bool) error {
		scanStarted <- struct{}{}
		select {
		case <-scanGate:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	}
	fw.cleanupLibrary = func(_ context.Context, _ int64) error {
		cleanupStarted <- struct{}{}
		return nil
	}

	const libID int64 = 1
	past := time.Now().Add(-10 * time.Second) // 已越过 5 秒去抖阈值
	fw.mu.Lock()
	fw.libs[filepath.FromSlash("/data/manga")] = libID
	fw.pending[libID] = past                                                      // Create(新名) 排期的扫描
	fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: past, lastEvent: past} // Rename(旧名) 排期的清理
	fw.mu.Unlock()

	fw.Start(nil)
	t.Cleanup(fw.Stop)

	select {
	case <-scanStarted:
	case <-time.After(6 * time.Second):
		t.Fatal("去抖到期后没有派发扫描")
	}

	// 扫描仍卡在 gate 上。此刻清理若已启动，改名重连就已经输掉了这场竞速。
	select {
	case <-cleanupStarted:
		t.Fatal("清理在扫描返回之前就开始了：改名重连会被它抢跑，旧行连同阅读进度一起被删")
	case <-time.After(300 * time.Millisecond):
	}

	close(scanGate)

	select {
	case <-cleanupStarted:
	case <-time.After(6 * time.Second):
		t.Fatal("扫描完成后清理没有跑：幽灵记录会一直留着")
	}
}

// TestCleanupSkippedWhenSameLibraryScanFails 判据：扫描没跑成时不得清理——改名重连可能还没
// 发生，此刻删行就是丢数据。清理要重新排期，留给下一个去抖窗口。
func TestCleanupSkippedWhenSameLibraryScanFails(t *testing.T) {
	fw := newLifecycleWatcher(t)

	scanStarted := make(chan struct{}, 1)
	cleanupStarted := make(chan struct{}, 1)

	fw.scanLibrary = func(_ context.Context, _ int64, _ string, _ bool) error {
		select {
		case scanStarted <- struct{}{}:
		default:
		}
		return errors.New("scan failed")
	}
	fw.cleanupLibrary = func(_ context.Context, _ int64) error {
		select {
		case cleanupStarted <- struct{}{}:
		default:
		}
		return nil
	}

	const libID int64 = 1
	past := time.Now().Add(-10 * time.Second)
	fw.mu.Lock()
	fw.libs[filepath.FromSlash("/data/manga")] = libID
	fw.pending[libID] = past
	fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: past, lastEvent: past}
	fw.mu.Unlock()

	fw.Start(nil)
	t.Cleanup(fw.Stop)

	select {
	case <-scanStarted:
	case <-time.After(6 * time.Second):
		t.Fatal("去抖到期后没有派发扫描")
	}

	select {
	case <-cleanupStarted:
		t.Fatal("扫描失败后仍然清理了：改名重连尚未发生，删行会丢掉阅读进度")
	case <-time.After(500 * time.Millisecond):
	}

	// 清理没有被丢弃，而是重新排期等下一个窗口。
	fw.mu.Lock()
	_, rescheduled := fw.pendingCleanup[libID]
	fw.mu.Unlock()
	if !rescheduled {
		t.Fatal("扫描失败后清理既没跑也没重新排期，幽灵记录再也不会被清掉")
	}
}

// newRenameOrderWatcher 造一个节拍调快的 watcher。时序用例要在秒内跑完，
// 生产的 2 秒轮询 + 5 秒去抖会把每条判据拖到十几秒。
func newRenameOrderWatcher(t *testing.T) *FileWatcher {
	t.Helper()
	fw := newLifecycleWatcher(t)
	fw.timings = watcherTimings{
		tick:               20 * time.Millisecond,
		scanDebounce:       100 * time.Millisecond,
		cleanupDebounce:    100 * time.Millisecond,
		cleanupMaxDeferral: 600 * time.Millisecond,
	}
	return fw
}

// keepWriting 模拟「一边改名一边往库里拷新书」：不断刷新该库的扫描排期，
// 让扫描的去抖窗口一直后移。返回的函数停止刷新并等刷新协程退出。
func keepWriting(fw *FileWatcher, libID int64) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-done:
				return
			default:
			}
			fw.mu.Lock()
			fw.pending[libID] = time.Now()
			fw.mu.Unlock()
			time.Sleep(5 * time.Millisecond)
		}
	}()
	return func() {
		close(done)
		<-stopped
	}
}

// TestCleanupHoldsForUnsettledRehome 判据：只要该库还有「没了结的改名重连」，清理就不许跑。
// 两条排期由不同事件刷新，会分叉——扫描本轮不到期时，同轮取走逻辑根本走不到，
// 独立的清理派发照样把清理单独放出去。
func TestCleanupHoldsForUnsettledRehome(t *testing.T) {
	const libID int64 = 1
	libPath := filepath.FromSlash("/data/manga")

	t.Run("扫描去抖窗口持续后移时清理不抢跑", func(t *testing.T) {
		fw := newRenameOrderWatcher(t)
		fw.timings.cleanupMaxDeferral = time.Hour // 本条只验「不抢跑」，别让推迟上限插手

		cleanupStarted := make(chan struct{}, 1)
		fw.scanLibrary = func(_ context.Context, _ int64, _ string, _ bool) error { return nil }
		fw.cleanupLibrary = func(_ context.Context, _ int64) error {
			select {
			case cleanupStarted <- struct{}{}:
			default:
			}
			return nil
		}

		fw.mu.Lock()
		fw.libs[libPath] = libID
		fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: time.Now().Add(-time.Second), lastEvent: time.Now().Add(-time.Second)} // Rename(旧名) 排的清理，早已到期
		fw.pending[libID] = time.Now()                                                                                                // Create/Write 排的扫描，窗口刚被刷新
		fw.mu.Unlock()

		fw.Start(nil)
		t.Cleanup(fw.Stop)
		stopWriting := keepWriting(fw, libID)
		defer stopWriting()

		select {
		case <-cleanupStarted:
			t.Fatal("扫描还排着队没跑、改名尚未重连，清理就单独派发了：旧行连同阅读进度会被删掉")
		case <-time.After(500 * time.Millisecond): // 25 个 tick
		}
	})

	t.Run("写入停下后清理跟在扫描之后跑掉", func(t *testing.T) {
		fw := newRenameOrderWatcher(t)
		fw.timings.cleanupMaxDeferral = time.Hour // 这条走的是正常路径，不靠上限兜底

		var scanReturned atomic.Bool
		cleanupSawScan := make(chan bool, 1)
		fw.scanLibrary = func(_ context.Context, _ int64, _ string, _ bool) error {
			time.Sleep(30 * time.Millisecond)
			scanReturned.Store(true)
			return nil
		}
		fw.cleanupLibrary = func(_ context.Context, _ int64) error {
			select {
			case cleanupSawScan <- scanReturned.Load():
			default:
			}
			return nil
		}

		fw.mu.Lock()
		fw.libs[libPath] = libID
		fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: time.Now().Add(-time.Second), lastEvent: time.Now().Add(-time.Second)}
		fw.pending[libID] = time.Now()
		fw.mu.Unlock()

		fw.Start(nil)
		t.Cleanup(fw.Stop)
		stopWriting := keepWriting(fw, libID)
		time.Sleep(300 * time.Millisecond) // 拷贝还在进行
		stopWriting()

		select {
		case sawScan := <-cleanupSawScan:
			if !sawScan {
				t.Fatal("清理跑在扫描返回之前：改名重连是扫描末尾才写的，此刻删行就是丢数据")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("写入停下、扫描跑完之后清理仍然没跑：幽灵记录再也清不掉")
		}
	})

	t.Run("上一轮扫描还在飞时清理不抢跑", func(t *testing.T) {
		fw := newRenameOrderWatcher(t)
		fw.timings.cleanupMaxDeferral = time.Hour

		scanStarted := make(chan struct{}, 1)
		scanGate := make(chan struct{})
		openGate := sync.OnceFunc(func() { close(scanGate) })
		var scanReturned atomic.Bool
		cleanupSawScan := make(chan bool, 1)

		fw.scanLibrary = func(ctx context.Context, _ int64, _ string, _ bool) error {
			select {
			case scanStarted <- struct{}{}:
			default:
			}
			select {
			case <-scanGate:
			case <-ctx.Done():
				return ctx.Err()
			}
			scanReturned.Store(true)
			return nil
		}
		fw.cleanupLibrary = func(_ context.Context, _ int64) error {
			select {
			case cleanupSawScan <- scanReturned.Load():
			default:
			}
			return nil
		}

		fw.mu.Lock()
		fw.libs[libPath] = libID
		fw.pending[libID] = time.Now().Add(-time.Second) // 本轮就派发
		fw.mu.Unlock()

		fw.Start(nil)
		t.Cleanup(func() { openGate(); fw.Stop() })

		select {
		case <-scanStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("去抖到期后没有派发扫描")
		}

		// 扫描刚派发出去、还卡在 gate 上，此时才发生删除/改名：清理年龄不到去抖阈值，
		// 同轮取走逻辑不会触发；下一轮 pending 已空，没有扫描可依附。
		fw.mu.Lock()
		fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: time.Now().Add(-time.Second), lastEvent: time.Now().Add(-time.Second)}
		fw.mu.Unlock()

		select {
		case <-cleanupSawScan:
			t.Fatal("上一轮扫描还在跑，清理就单独派发了：它的 RehomeBookPath 末尾才写，行会被抢先删掉")
		case <-time.After(400 * time.Millisecond):
		}

		openGate()
		select {
		case sawScan := <-cleanupSawScan:
			if !sawScan {
				t.Fatal("清理跑在扫描返回之前")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("在飞的扫描结束后清理仍然没跑")
		}
	})

	t.Run("别处发起的扫描在跑时清理不抢跑", func(t *testing.T) {
		fw := newRenameOrderWatcher(t)
		fw.timings.cleanupMaxDeferral = time.Hour

		var elsewhere atomic.Bool
		elsewhere.Store(true) // 任务面板的「扫描资料库」正在跑，watcher 一无所知
		fw.libraryScanRunning = func(int64) bool { return elsewhere.Load() }

		cleanupStarted := make(chan struct{}, 1)
		fw.scanLibrary = func(_ context.Context, _ int64, _ string, _ bool) error { return nil }
		fw.cleanupLibrary = func(_ context.Context, _ int64) error {
			select {
			case cleanupStarted <- struct{}{}:
			default:
			}
			return nil
		}

		past := time.Now().Add(-time.Second)
		fw.mu.Lock()
		fw.libs[libPath] = libID
		fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: past, lastEvent: past}
		fw.mu.Unlock()

		fw.Start(nil)
		t.Cleanup(fw.Stop)

		// 那次扫描的 RehomeBookPath 同样写在末尾，它没结束就等于重连没落地。
		select {
		case <-cleanupStarted:
			t.Fatal("别处发起的扫描还在跑，清理就跑了：它正要重连的那一行会被删掉")
		case <-time.After(400 * time.Millisecond):
		}

		elsewhere.Store(false)
		select {
		case <-cleanupStarted:
		case <-time.After(3 * time.Second):
			t.Fatal("那次扫描结束后清理仍然没跑")
		}
	})

	t.Run("同轮扫描失败后清理不会在下一个窗口裸奔", func(t *testing.T) {
		fw := newRenameOrderWatcher(t)
		fw.timings.cleanupMaxDeferral = time.Hour

		var scans atomic.Int64
		cleanupStarted := make(chan struct{}, 1)
		fw.scanLibrary = func(_ context.Context, _ int64, _ string, _ bool) error {
			scans.Add(1)
			return errors.New("scan failed")
		}
		fw.cleanupLibrary = func(_ context.Context, _ int64) error {
			select {
			case cleanupStarted <- struct{}{}:
			default:
			}
			return nil
		}

		past := time.Now().Add(-time.Second)
		fw.mu.Lock()
		fw.libs[libPath] = libID
		fw.pending[libID] = past
		fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: past, lastEvent: past}
		fw.mu.Unlock()

		fw.Start(nil)
		t.Cleanup(fw.Stop)

		// 扫描一直失败，清理就一直不许跑：重连没落地，删行就是丢数据。
		// 只把清理放回排期而不把扫描一起放回，下一个窗口会因为「没有排期中的扫描」判定安全
		// 并直接删行——那正是这条判据要挡住的裸奔。
		select {
		case <-cleanupStarted:
			t.Fatal("扫描一直没跑成，清理却在后续窗口里单独跑掉了")
		case <-time.After(600 * time.Millisecond):
		}
		if scans.Load() < 2 {
			t.Fatalf("扫描只被试了 %d 次：清理连同扫描一起重排才有转绿的一天", scans.Load())
		}
	})

	t.Run("写入永不停歇时清理仍会在推迟上限内跑掉", func(t *testing.T) {
		fw := newRenameOrderWatcher(t) // cleanupMaxDeferral = 600ms

		var scanReturned atomic.Bool
		cleanupSawScan := make(chan bool, 1)
		fw.scanLibrary = func(_ context.Context, _ int64, _ string, _ bool) error {
			scanReturned.Store(true)
			return nil
		}
		fw.cleanupLibrary = func(_ context.Context, _ int64) error {
			select {
			case cleanupSawScan <- scanReturned.Load():
			default:
			}
			return nil
		}

		fw.mu.Lock()
		fw.libs[libPath] = libID
		fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: time.Now(), lastEvent: time.Now()}
		fw.pending[libID] = time.Now()
		fw.mu.Unlock()

		fw.Start(nil)
		t.Cleanup(fw.Stop)
		stopWriting := keepWriting(fw, libID) // 全程不停：扫描的去抖窗口永远不到期
		defer stopWriting()

		select {
		case sawScan := <-cleanupSawScan:
			if !sawScan {
				t.Fatal("推迟上限把清理放出去了，却没先跑一次扫描：改名重连仍然没发生")
			}
		case <-time.After(4 * time.Second):
			t.Fatal("写入不停歇时清理被无限期推迟：删掉的书会永远以幽灵记录留在库里")
		}
	})
}
