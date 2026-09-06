// 守「投递与列表交出去的那份快照，与仍在被写入的那份不是同一批 map」。Params/Metrics/Labels/
// MessageParams 都是 map，而结构体拷贝共享同一 map header：进度上报在引擎的临界区里写，
// listRunStatuses 交出去的快照则在锁外被 json.Marshal 遍历。共享一份就会撞成
// `fatal error: concurrent map read and map write`——runtime throw，recover 拦不住，整个进程退出。

package api

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"manga-manager/internal/runhandle"
)

// TestTaskSnapshotsAreClonedAcrossCriticalSection 并发跑「进度更新」与「落盘 + 列表」，
// 让写侧与读侧的窗口充分重叠。必须在 -race 下运行才有意义：不加检测器时，
// 逃逸出去的共享 map 只是概率性地撞成 fatal，多数运行看起来是绿的。
func TestTaskSnapshotsAreClonedAcrossCriticalSection(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	const taskKey = "scan:library:1"
	progress := seedTask(t, controller.taskEngine, taskSeed{Key: taskKey, Identity: systemTask("library_scan", variantSole), Total: 1000})

	stop := make(chan struct{})
	var writer, readers sync.WaitGroup

	// 写侧：模拟扫描器的高频进度回调（真实场景每 250ms 一次，回填任务每本书两次——
	// **运行句柄**一次、任务参数一次，两次都在锁内原地写同一批 map）。
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
			progress.Advance(i, 1000, "task.msg.scan_library.progress", map[string]string{"current": strconv.Itoa(i)})
			progress.Report(runhandle.Frame{Item: "item"})
			progress.Report(runhandle.Frame{Metrics: map[string]int64{"processed": int64(i)}})
			progress.Report(runhandle.Frame{Labels: map[string]string{"library": "alpha"}})
			progress.MergeParams(map[string]string{"library": "alpha"})
		}
	}()

	// 读侧：任务列表接口，锁外由 json.Marshal 遍历返回的快照。
	readers.Add(1)
	go func() {
		defer readers.Done()
		for range 200 {
			items, err := controller.taskEngine.listRunStatuses(context.Background(), taskFilters{Limit: 50})
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
	settleSeededTask(t, controller.taskEngine, taskKey, nil)
}
