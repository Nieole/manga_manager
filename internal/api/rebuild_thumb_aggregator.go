// 「重建缩略图」任务的跨库进度聚合，以及该任务**进度句柄**的保管处。
//
// 它的进度由任务体**之外**写入——写入者是扫描器的 goroutine，不在任务体的调用栈上。
// 任务体因此在开工时把句柄交给聚合器，写入者从快照里取句柄再写进度；
// 「有没有拿到句柄」既是写入资格，也就是「重建是否在进行中」这个判定本身。

package api

import (
	"sync"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
)

// rebuildThumbAggregator 聚合「重建缩略图」任务在多个资料库之间的进度。
//
// 该任务会依次全量扫描每个资料库，而扫描器只按单库上报 metrics；要在任务面板上显示一个
// 全局进度，就得把「已完成库的最终值（baseline）」与「进行中库的最新快照（perLibPending）」
// 合并起来。麻烦在于封面生成是异步的：某个库的主流程结束后，cover queue 仍会继续上报该库的
// generated_covers，若不加区分就会与已计入 baseline 的值重复累计（去重规则见 fixateLibrary）。
//
// 须经 newRebuildThumbAggregator 构造；所有方法对 nil 接收者安全，
// 与 sseBroker.publish 的既有写法一致——白盒测试会手工拼装 Controller，
// 漏掉某个组件时应表现为「该功能无操作」而不是 nil 解引用 panic。
type rebuildThumbAggregator struct {
	mu sync.Mutex

	// progress 是任务体交来的进度句柄；为 nil 表示当前没有在跑重建任务，所有更新都是无操作。
	// 它不只是一个状态位：拿不到句柄的代码没有任何办法写这个任务的进度。
	progress *TaskProgress

	totalLibraries int
	doneLibraries  int
	baseline       map[string]int64
	perLibPending  map[int64]map[string]int64
	finalizedLibs  map[int64]struct{}
	// finalizedCoverSeen[libID] = 该库 fixate 后从 progress 事件中观察到的 generated_covers 最大值，
	// 避免 cover queue 异步阶段对已 fixate 库的二次累计。
	finalizedCoverSeen map[int64]int64
	currentLibID       int64
	currentLibName     string
	currentLibPath     string
}

// rebuildThumbSnapshot 是聚合器对外暴露的只读视图，也是进度句柄的交付方式。
// 所有读取路径都走它：调用方不得自行持锁拼装 merged，否则合并规则会散成好几份各自漂移。
type rebuildThumbSnapshot struct {
	// Progress 为 nil 表示当前没有在跑重建任务，取到快照的一方应直接跳过。
	Progress       *TaskProgress
	Metrics        map[string]int64
	DoneLibraries  int
	TotalLibraries int
	CurrentLibName string
	CurrentLibPath string
}

func newRebuildThumbAggregator() *rebuildThumbAggregator {
	return &rebuildThumbAggregator{}
}

// begin 开启一轮聚合并重置全部状态，progress 是任务体交来的进度句柄。
func (a *rebuildThumbAggregator) begin(progress *TaskProgress, totalLibraries int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
	a.progress = progress
	a.totalLibraries = totalLibraries
}

// end 结束本轮聚合并交回进度句柄；之后的更新都是无操作，直到下一次 begin。
func (a *rebuildThumbAggregator) end() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
}

// reset 清空状态，调用方须持锁。
func (a *rebuildThumbAggregator) reset() {
	a.progress = nil
	a.totalLibraries = 0
	a.doneLibraries = 0
	a.baseline = make(map[string]int64)
	a.perLibPending = make(map[int64]map[string]int64)
	a.finalizedLibs = make(map[int64]struct{})
	a.finalizedCoverSeen = make(map[int64]int64)
	a.currentLibID = 0
	a.currentLibName = ""
	a.currentLibPath = ""
}

// trackLibrary 在 runGlobalScan 的库切换边界更新聚合器，
// done 是已完成库数（progress 回调 i 表示「开始第 i+1 个」，i+1 表示「完成第 i+1 个」）。
func (a *rebuildThumbAggregator) trackLibrary(done, total int, lib database.Library) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil {
		return
	}
	a.totalLibraries = total
	a.doneLibraries = done
	a.currentLibID = lib.ID
	a.currentLibName = lib.Name
	a.currentLibPath = lib.Path
}

// applyProgress 吸收扫描器的单库进度事件，返回吸收后的快照。
func (a *rebuildThumbAggregator) applyProgress(report scanner.ScanProgressReport) rebuildThumbSnapshot {
	if a == nil {
		return rebuildThumbSnapshot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil {
		return rebuildThumbSnapshot{}
	}

	if _, finalized := a.finalizedLibs[report.ID]; finalized {
		// 库 fixate 时已把当时的 generated_covers 计入 baseline。这里只把 progress 事件中
		// 新增的 generated_covers 增量补回 baseline，其它 metrics 不再变更。
		newSeen := report.Metrics["generated_covers"]
		if newSeen > a.finalizedCoverSeen[report.ID] {
			a.baseline["generated_covers"] += newSeen - a.finalizedCoverSeen[report.ID]
			a.finalizedCoverSeen[report.ID] = newSeen
		}
	} else {
		snapshot := make(map[string]int64, len(report.Metrics))
		for k, v := range report.Metrics {
			snapshot[k] = v
		}
		a.perLibPending[report.ID] = snapshot
	}
	return a.snapshotLocked()
}

// fixateLibrary 在某个库扫描「主流程」完成时被调用（cover queue 仍可能在异步中），
// 把该库的最终 metrics 加到 baseline，并删除 perLibPending 中对应条目。
//
// 注意：cover queue 异步阶段的 generatedCovers 增量会通过 progress 事件继续更新该库的
// perLibPending，而 baseline 里已累计了最终值，直接相加就会双计。为避免双计，
// fixate 后由 applyProgress 走 finalizedLibs 分支只补增量。
func (a *rebuildThumbAggregator) fixateLibrary(report scanner.ScanMetricsReport) rebuildThumbSnapshot {
	if a == nil {
		return rebuildThumbSnapshot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil {
		return rebuildThumbSnapshot{}
	}

	delete(a.perLibPending, report.ID)
	a.finalizedLibs[report.ID] = struct{}{}
	a.finalizedCoverSeen[report.ID] = report.GeneratedCovers
	a.baseline["discovered_archives"] += report.DiscoveredArchives
	a.baseline["skipped_archives"] += report.SkippedArchives
	a.baseline["processed_archives"] += report.ProcessedArchives
	a.baseline["opened_archives"] += report.OpenedArchives
	a.baseline["hashed_files"] += report.HashedFiles
	a.baseline["queued_covers"] += report.QueuedCovers
	a.baseline["generated_covers"] += report.GeneratedCovers
	a.baseline["failed_archives"] += report.FailedArchives
	a.baseline["io_wait_ms"] += report.IOWaitMillis
	a.baseline["paused_ms"] += report.PausedMillis
	a.baseline["thumbnail_write_ms"] += report.ThumbnailWriteMillis
	return a.snapshotLocked()
}

// snapshot 返回当前聚合状态的只读视图。
func (a *rebuildThumbAggregator) snapshot() rebuildThumbSnapshot {
	if a == nil {
		return rebuildThumbSnapshot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked()
}

// snapshotLocked 合并 baseline 与各库 pending，调用方须持锁。
func (a *rebuildThumbAggregator) snapshotLocked() rebuildThumbSnapshot {
	if a.progress == nil {
		return rebuildThumbSnapshot{}
	}
	merged := make(map[string]int64, len(a.baseline)+8)
	for k, v := range a.baseline {
		merged[k] = v
	}
	for _, pending := range a.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	return rebuildThumbSnapshot{
		Progress:       a.progress,
		Metrics:        merged,
		DoneLibraries:  a.doneLibraries,
		TotalLibraries: a.totalLibraries,
		CurrentLibName: a.currentLibName,
		CurrentLibPath: a.currentLibPath,
	}
}
