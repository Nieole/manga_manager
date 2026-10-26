package organize

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
