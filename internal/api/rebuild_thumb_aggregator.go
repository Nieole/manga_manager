// 「重建缩略图」任务的跨库进度聚合，以及该任务**进度句柄**的保管处。
//
// 本文件只做记账：合并、去重、算分母。把一份快照翻成一帧任务进度（含文案码的挑选）
// 在 controller_scan_events.go，rebuildThumbLibrary 的两个**扫描观察者**方法也在那里。

package api

import (
	"sync"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
)

// rebuildThumbAggregator 聚合「重建缩略图」任务在多个资料库之间的进度。
//
// 该任务会依次全量扫描每个资料库，而每次扫描只报自己那一个库的 metrics；要在任务面板上显示
// 一个全局进度，就得把「已完成库的最终值（baseline）」与「进行中库的最新快照」合并起来。
// 后者由每个库自己的**扫描观察者**（rebuildThumbLibrary）持有——它本来就「是」那个库，
// 不必再拿库 id 去查表。
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
	// libraries 是本轮已开工的每库观察者，按开工顺序排列，供合并未定版的那几份快照。
	libraries      []*rebuildThumbLibrary
	currentLibName string
	currentLibPath string
}

// rebuildThumbLibrary 是重建任务交给**一个**资料库扫描的**扫描观察者**，同时也是那个库的记账处。
//
// 它的三项状态由聚合器的 mu 保护，而不是自带一把锁：pending 与 baseline 的合并必须原子，
// 分成两把锁就得规定加锁顺序，而这是记不住的。
//
// 两个 ScanObserver 方法（Progress / Metrics）实现在 controller_scan_events.go，
// 本文件只放它的记账。
type rebuildThumbLibrary struct {
	agg *rebuildThumbAggregator
	lib database.Library

	// pending 是该库尚未定版的最新 metrics 快照，定版后清空。
	pending map[string]int64
	// finalized 表示该库的扫描主流程已结束、最终 metrics 已计入 baseline。
	// 此后封面队列仍会异步推进 generated_covers，那些帧只补增量（见 absorbProgress）。
	finalized bool
	// coverSeen 是定版后已计入 baseline 的 generated_covers 最大值，用于只补增量、不双计。
	coverSeen int64
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
//
// 已交出去的每库观察者不会被收回——它们可能还握在封面队列手里。交回句柄之后，
// 它们的写入全部退化为无操作，这正是「重建不在进行中」应有的表现。
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
	a.libraries = nil
	a.currentLibName = ""
	a.currentLibPath = ""
}

// beginLibrary 开始第 doneLibraries+1 个资料库，返回交给这次扫描的**扫描观察者**。
//
// 调用它本身就是库切换的边界——不需要另一条「现在换库了」的通知，那条通知与创建观察者
// 一旦分家，就会有「观察者已经在收报文、界面上还是上一个库名」的窗口。
//
// 重建未在进行时返回 nil（而不是一个无操作的观察者）：交出去的东西为 nil，
// 扫描器那边判定得到的就是「这次扫描不属于任何任务」，与其余无归属扫描同一个形状。
func (a *rebuildThumbAggregator) beginLibrary(lib database.Library, totalLibraries int) *rebuildThumbLibrary {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.progress == nil {
		return nil
	}
	a.totalLibraries = totalLibraries
	a.currentLibName = lib.Name
	a.currentLibPath = lib.Path
	entry := &rebuildThumbLibrary{agg: a, lib: lib}
	a.libraries = append(a.libraries, entry)
	return entry
}

// absorbProgress 吸收本库的一份进度报文，返回吸收后的快照。
func (l *rebuildThumbLibrary) absorbProgress(report scanner.ScanProgressReport) rebuildThumbSnapshot {
	if l == nil || l.agg == nil {
		return rebuildThumbSnapshot{}
	}
	l.agg.mu.Lock()
	defer l.agg.mu.Unlock()
	if l.agg.progress == nil {
		return rebuildThumbSnapshot{}
	}

	if l.finalized {
		// 定版时已把当时的 generated_covers 计入 baseline。这里只把新增的那部分补回去，
		// 其余指标不再变更——报文带的是本库的全量快照，整份相加就会与 baseline 双计。
		if seen := report.Metrics["generated_covers"]; seen > l.coverSeen {
			l.agg.baseline["generated_covers"] += seen - l.coverSeen
			l.coverSeen = seen
		}
	} else {
		snapshot := make(map[string]int64, len(report.Metrics))
		for k, v := range report.Metrics {
			snapshot[k] = v
		}
		l.pending = snapshot
	}
	return l.agg.snapshotLocked()
}

// fixate 在本库扫描「主流程」完成时把最终 metrics 计入 baseline，返回定版后的快照。
//
// 此刻封面队列仍可能在异步生成，之后还会有本库的进度报文带着同一份 metrics 的更新值到来；
// 定版后由 absorbProgress 走 finalized 分支只补增量，避免双计。
func (l *rebuildThumbLibrary) fixate(report scanner.ScanMetricsReport) rebuildThumbSnapshot {
	if l == nil || l.agg == nil {
		return rebuildThumbSnapshot{}
	}
	l.agg.mu.Lock()
	defer l.agg.mu.Unlock()
	if l.agg.progress == nil {
		return rebuildThumbSnapshot{}
	}

	l.pending = nil
	l.finalized = true
	l.coverSeen = report.GeneratedCovers
	l.agg.doneLibraries++
	base := l.agg.baseline
	base["discovered_archives"] += report.DiscoveredArchives
	base["skipped_archives"] += report.SkippedArchives
	base["processed_archives"] += report.ProcessedArchives
	base["opened_archives"] += report.OpenedArchives
	base["hashed_files"] += report.HashedFiles
	base["queued_covers"] += report.QueuedCovers
	base["generated_covers"] += report.GeneratedCovers
	base["failed_archives"] += report.FailedArchives
	base["io_wait_ms"] += report.IOWaitMillis
	base["paused_ms"] += report.PausedMillis
	base["thumbnail_write_ms"] += report.ThumbnailWriteMillis
	return l.agg.snapshotLocked()
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

// snapshotLocked 合并 baseline 与各库尚未定版的快照，调用方须持锁。
func (a *rebuildThumbAggregator) snapshotLocked() rebuildThumbSnapshot {
	if a.progress == nil {
		return rebuildThumbSnapshot{}
	}
	merged := make(map[string]int64, len(a.baseline)+8)
	for k, v := range a.baseline {
		merged[k] = v
	}
	for _, lib := range a.libraries {
		for k, v := range lib.pending {
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
