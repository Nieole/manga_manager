// 守改名重连在 watch 模式下不被同轮的库清理抢跑。
//
// 改名产生 Rename(旧名)+Create(新名) 两个事件，分别排期清理与扫描。清理只做 stat，比要走完
// 整棵树、末尾才写改名重连的扫描快得多，并发派发时必然抢先把旧行当成「文件已消失」删掉，挂在
// 这本书上的记录随级联一起没——所以同一个库的清理必须排在这次扫描之后。

package scanner

import (
	"context"
	"errors"
	"path/filepath"
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
	fw.pending[libID] = past        // Create(新名) 排期的扫描
	fw.pendingCleanup[libID] = past // Rename(旧名) 排期的清理
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
	fw.pendingCleanup[libID] = past
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
