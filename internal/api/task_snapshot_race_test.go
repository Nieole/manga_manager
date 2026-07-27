// 业务说明：本文件是业务回归测试，属于后台任务系统，验证任务快照跨临界区传递时的并发安全。
// 它锁住「进度回调持锁写 map」与「落盘/序列化在锁外读同一 map」这条会导致进程 fatal 的路径。
// 维护时应保持用例在 -race 下运行，并确保任何新的快照逃逸点都被覆盖。

package api

import (
	"context"
	"sync"
	"testing"

	"manga-manager/internal/database"
)

// TestTaskSnapshotsAreClonedAcrossCriticalSection 复现并锁住 P0 并发缺陷。
//
// TaskStatus 的 Params/Metrics/Labels/MessageParams 都是 map，结构体拷贝共享同一 map header。
// 进度回调在持锁下原地写这些 map；而 flushTaskPersist（每 500ms）与 listTasks（HTTP 序列化）
// 都在释放锁之后才遍历它们。共享 map 会让两者重叠，触发
// `fatal error: concurrent map read and map write`——那是 runtime throw 而非 panic，
// recover 与 middleware.Recoverer 都拦不住，整个进程直接退出。
//
// 本用例在 -race 下并发跑「进度更新」与「落盘 + 列表」，任何快照逃逸都会被检出。
func TestTaskSnapshotsAreClonedAcrossCriticalSection(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	const taskKey = "scan:library:1"
	if !controller.startTask(taskKey, "library_scan", "scanning", 1000) {
		t.Fatal("expected task to start")
	}

	stop := make(chan struct{})
	var writer, readers sync.WaitGroup

	// 写侧：模拟扫描器的高频进度回调（真实场景每 250ms 一次，回填任务每本书两次）。
	// 持续到读侧跑完为止，保证读写窗口充分重叠。
	writer.Add(1)
	go func() {
		defer writer.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			controller.updateTaskDetails(taskKey, i, 1000, "scanning", "phase", "item",
				map[string]int64{"processed": int64(i)},
				map[string]string{"library": "alpha"})
		}
	}()

	// 读侧一：异步落盘，锁外遍历 persistPending 里的快照。
	readers.Add(1)
	go func() {
		defer readers.Done()
		for range 200 {
			controller.flushTaskPersist()
		}
	}()

	// 读侧二：任务列表接口，锁外由 json.Marshal 遍历返回的快照。
	readers.Add(1)
	go func() {
		defer readers.Done()
		for range 200 {
			items, err := controller.listTaskStatuses(context.Background(), database.TaskFilters{Limit: 50})
			if err != nil {
				continue
			}
			// 显式遍历 map，模拟 json.Marshal 的读取行为。
			for _, item := range items {
				for k := range item.Metrics {
					_ = item.Metrics[k]
				}
				for k := range item.Params {
					_ = item.Params[k]
				}
				for k := range item.Labels {
					_ = item.Labels[k]
				}
			}
		}
	}()

	// 必须先让写侧停下，再等它退出——否则 writer 永远收不到停止信号。
	readers.Wait()
	close(stop)
	writer.Wait()
	controller.finishTask(taskKey, "done")
}
