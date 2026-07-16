// 业务说明：本文件把 franchise 合集重建的「合并式调度」从 Controller 上帝对象里抽成独立组件。
// franchiseRebuilder 只负责调度状态（是否在跑 / 是否有待处理请求），通过注入的 rebuild / runBackground
// 回调与领域逻辑（RebuildFranchiseCollections）和生命周期（backgroundWG）解耦：并发触发时只合并成「至多
// 再跑一轮」，避免每次系列关联增删改都各起一个全图重建 goroutine 争抢 SQLite 写锁。调度状态全程由 mu 保护。

package api

import (
	"context"
	"log/slog"
	"sync"
)

type franchiseRebuilder struct {
	mu      sync.Mutex
	running bool
	pending bool

	rebuild       func(context.Context) error
	runBackground func(func())
}

func newFranchiseRebuilder(rebuild func(context.Context) error, runBackground func(func())) *franchiseRebuilder {
	return &franchiseRebuilder{rebuild: rebuild, runBackground: runBackground}
}

// schedule 合并触发一次 franchise 重建：已有重建在跑时只置 pending，否则起一个后台任务循环重建直到无待
// 处理请求。经注入的 runBackground 登记到生命周期 WaitGroup，关闭流程会等待其结束。
func (f *franchiseRebuilder) schedule() {
	f.mu.Lock()
	if f.running {
		f.pending = true
		f.mu.Unlock()
		return
	}
	f.running = true
	f.mu.Unlock()

	f.runBackground(func() {
		for {
			if err := f.rebuild(context.Background()); err != nil {
				slog.Error("Franchise rebuild failed", "error", err)
			}
			f.mu.Lock()
			if f.pending {
				f.pending = false
				f.mu.Unlock()
				continue
			}
			f.running = false
			f.mu.Unlock()
			return
		}
	})
}
