package maintenance

import (
	"path/filepath"
	"strconv"
	"sync"

	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
)

// ThumbnailProgress 聚合「重建缩略图」任务在多个资料库之间的进度。
//
// 该任务会依次全量扫描每个资料库，而每次扫描只报自己那一个库的 metrics；要在任务面板上显示
// 一个全局进度，就得把「已完成库的最终值（baseline）」与「进行中库的最新快照」合并起来。
// 后者由每个库自己的**扫描观察者**（rebuildThumbLibrary）持有——它本来就「是」那个库，
// 不必再拿库 id 去查表。
//
// 须经 NewThumbnailProgress 构造；所有方法对 nil 接收者安全，
// 未开启聚合时不向任何运行写入进度。
type ThumbnailProgress struct {
	mu     sync.Mutex
	writes sync.Mutex

	// progress 是任务体交来的运行句柄；为 nil 表示当前没有在跑重建任务，所有更新都是无操作。
	// 它不只是一个状态位：拿不到句柄的代码没有任何办法写这个任务的进度。
	progress *runhandle.Handle

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
// 四个 ScanObserver 方法（Progress / Metrics / ItemFailed / Warn）实现在
// ThumbnailProgress 的观察者方法。
type rebuildThumbLibrary struct {
	agg *ThumbnailProgress
	lib database.Library

	// pending 是该库尚未定版的最新 metrics 快照，定版后清空。
	pending   map[string]int64
	finalized bool
}

// rebuildThumbSnapshot 是聚合器对外暴露的只读视图，也是运行句柄的交付方式。
// 所有读取路径都走它：调用方不得自行持锁拼装 merged，否则合并规则会散成好几份各自漂移。
type rebuildThumbSnapshot struct {
	// Progress 为 nil 表示当前没有在跑重建任务，取到快照的一方应直接跳过。
	Progress       *runhandle.Handle
	Metrics        map[string]int64
	DoneLibraries  int
	TotalLibraries int
	CurrentLibName string
	CurrentLibPath string
}

func NewThumbnailProgress() *ThumbnailProgress {
	return &ThumbnailProgress{}
}

// begin 开启一轮聚合并重置全部状态，progress 是任务体交来的运行句柄。
func (a *ThumbnailProgress) begin(progress *runhandle.Handle, totalLibraries int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
	a.progress = progress
	a.totalLibraries = totalLibraries
}

// end 结束本轮聚合并交回运行句柄；之后的更新都是无操作，直到下一次 begin。
//
// 已交出去的每库观察者不会被收回：交回句柄之后它们的写入全部退化为无操作，
// 这正是「重建不在进行中」应有的表现。
func (a *ThumbnailProgress) end() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reset()
}

// reset 清空状态，调用方须持锁。
func (a *ThumbnailProgress) reset() {
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
func (a *ThumbnailProgress) beginLibrary(lib database.Library, totalLibraries int) *rebuildThumbLibrary {
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
	if l.agg.progress == nil || l.finalized {
		return rebuildThumbSnapshot{}
	}

	snapshot := make(map[string]int64, len(report.Metrics))
	for k, v := range report.Metrics {
		snapshot[k] = v
	}
	l.pending = snapshot
	return l.agg.snapshotLocked()
}

// fixate 在本库扫描完成时把最终 metrics 计入 baseline，返回定版后的快照。
//
// 扫描完成后忽略本库迟到的进度快照；排出的封面归独立的封面运行。
func (l *rebuildThumbLibrary) fixate(report scanner.ScanMetricsReport) rebuildThumbSnapshot {
	if l == nil || l.agg == nil {
		return rebuildThumbSnapshot{}
	}
	l.agg.mu.Lock()
	defer l.agg.mu.Unlock()
	if l.agg.progress == nil || l.finalized {
		return rebuildThumbSnapshot{}
	}

	l.pending = nil
	l.finalized = true
	l.agg.doneLibraries++
	base := l.agg.baseline
	base["discovered_archives"] += report.DiscoveredArchives
	base["skipped_archives"] += report.SkippedArchives
	base["processed_archives"] += report.ProcessedArchives
	base["opened_archives"] += report.OpenedArchives
	base["hashed_files"] += report.HashedFiles
	base["queued_covers"] += report.QueuedCovers
	base["failed_archives"] += report.FailedArchives
	base["io_wait_ms"] += report.IOWaitMillis
	base["paused_ms"] += report.PausedMillis
	return l.agg.snapshotLocked()
}

// snapshot 返回当前聚合状态的只读视图。
func (a *ThumbnailProgress) snapshot() rebuildThumbSnapshot {
	if a == nil {
		return rebuildThumbSnapshot{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked()
}

// snapshotLocked 合并 baseline 与各库尚未定版的快照，调用方须持锁。
func (a *ThumbnailProgress) snapshotLocked() rebuildThumbSnapshot {
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

// Progress 把本库的一份进度报文并进跨库聚合，再按全局视角写一帧重建任务的进度。
func (l *rebuildThumbLibrary) Progress(report scanner.ScanProgressReport) {
	if l == nil || l.agg == nil {
		return
	}
	l.agg.writes.Lock()
	defer l.agg.writes.Unlock()
	snap := l.absorbProgress(report)
	if snap.Progress == nil {
		return
	}
	currentLibName := snap.CurrentLibName
	doneLibs := snap.DoneLibraries
	totalLibs := snap.TotalLibraries

	phase := report.Phase
	if phase == "" {
		phase = "reading_metadata"
	}
	currentItem := report.CurrentItem
	displayName := filepath.Base(report.CurrentItem)
	var code string
	var msgParams map[string]string
	switch {
	case currentItem == "" && currentLibName != "":
		code = "task.msg.rebuild_thumbnails.rebuilding_library_progress"
		msgParams = map[string]string{"lib": currentLibName, "done": strconv.Itoa(doneLibs + 1), "total": strconv.Itoa(totalLibs)}
	case displayName != "" && currentLibName != "":
		code = "task.msg.rebuild_thumbnails.rebuilding_item_in_library"
		msgParams = map[string]string{"lib": currentLibName, "done": strconv.Itoa(doneLibs + 1), "total": strconv.Itoa(totalLibs), "item": displayName}
	case displayName != "":
		code = "task.msg.rebuild_thumbnails.rebuilding_item"
		msgParams = map[string]string{"item": displayName}
	default:
		code = "task.msg.rebuild_thumbnails.rebuilding"
	}
	if currentItem == "" {
		currentItem = snap.CurrentLibPath
	}
	writeRebuildThumbProgress(snap, runhandle.Frame{
		Phase:  phase,
		Item:   currentItem,
		Code:   code,
		Params: msgParams,
		Labels: map[string]string{"current_library": currentLibName},
	})
}

// ItemFailed 与 Warn 直接落在重建任务那条运行上：跨库聚合的是计数，而失败明细与告警
// 本来就带着自己的文件路径，聚合会把它们抹成一个数。
func (l *rebuildThumbLibrary) ItemFailed(failure scanner.ItemFailure) {
	if progress := l.agg.snapshot().Progress; progress != nil {
		progress.ItemFailed(failure.Path, failure.Reason)
	}
}

func (l *rebuildThumbLibrary) Warn(warning scanner.ScanWarning) {
	if progress := l.agg.snapshot().Progress; progress != nil {
		progress.Warn(warning.Code, warning.Detail, warning.Count)
	}
}

// Metrics 在本库扫描主流程结束时定版它的指标，并把这份报文累加进重建任务。
//
// 报文里那四个描述性值（存储画像、卷键、两个并发数）不随指标写上去：它们描述的是这一个库，
// 而这条运行跨着全部资料库，理由见 Service.RebuildThumbnails。哪个数走指标、哪个走参数，
// 判据见 scanning.NewObserver。
func (l *rebuildThumbLibrary) Metrics(report scanner.ScanMetricsReport) {
	if l == nil || l.agg == nil {
		return
	}
	l.agg.writes.Lock()
	defer l.agg.writes.Unlock()
	snap := l.fixate(report)
	if snap.Progress == nil {
		return
	}
	// 先累加再写帧：帧里的指标是聚合器算出的绝对值，会按键覆盖同名项。反过来先写帧再累加，
	// 增量就加在自己刚写下的绝对值上，任务面板上的指标凭空翻倍。
	snap.Progress.AddMetrics(map[string]int64{
		"discovered_archives": report.DiscoveredArchives,
		"skipped_archives":    report.SkippedArchives,
		"processed_archives":  report.ProcessedArchives,
		"opened_archives":     report.OpenedArchives,
		"hashed_files":        report.HashedFiles,
		"queued_covers":       report.QueuedCovers,
		"failed_archives":     report.FailedArchives,
		"io_wait_ms":          report.IOWaitMillis,
		"paused_ms":           report.PausedMillis,
		"duration_ms":         report.DurationMillis,
	}, nil)

	code := "task.msg.rebuild_thumbnails.rebuilding"
	var msgParams map[string]string
	if snap.TotalLibraries > 0 {
		code = "task.msg.rebuild_thumbnails.libraries_completed"
		msgParams = map[string]string{"done": strconv.Itoa(snap.DoneLibraries), "total": strconv.Itoa(snap.TotalLibraries)}
	}
	// 不动**阶段**：这一帧说的是「又完成一个库」，而此刻正在做什么由下一个库的首帧接着说。
	writeRebuildThumbProgress(snap, runhandle.Frame{
		Code:   code,
		Params: msgParams,
	})
}

func (c *ThumbnailProgress) Begin(progress *runhandle.Handle, totalLibraries int) {
	if c == nil {
		return
	}
	c.writes.Lock()
	defer c.writes.Unlock()
	c.begin(progress, totalLibraries)
}

func (c *ThumbnailProgress) End() {
	if c == nil {
		return
	}
	c.writes.Lock()
	defer c.writes.Unlock()
	c.end()
}

// BeginLibrary 开始一个资料库，返回它在当前重建中的观察者，
// 返回这次扫描的**扫描观察者**；重建未在进行时返回 nil 接口值。
func (c *ThumbnailProgress) BeginLibrary(lib database.Library, totalLibraries int) scanner.ScanObserver {
	entry := c.beginLibrary(lib, totalLibraries)
	if entry == nil {
		return nil
	}
	c.Refresh(lib)
	return entry
}

// Refresh 用聚合器中已记录的 metrics 立即刷新一次任务，
// 用于在库切换边界（此刻还没有任何本库报文携带 metrics）把任务消息与当前库标签同步过去。
func (c *ThumbnailProgress) Refresh(lib database.Library) {
	if c == nil {
		return
	}
	c.writes.Lock()
	defer c.writes.Unlock()
	snap := c.snapshot()
	if snap.Progress == nil {
		return
	}
	code := "task.msg.rebuild_thumbnails.rebuilding_library"
	msgParams := map[string]string{"lib": lib.Name}
	if snap.TotalLibraries > 0 {
		code = "task.msg.rebuild_thumbnails.rebuilding_library_progress"
		msgParams = map[string]string{"lib": lib.Name, "done": strconv.Itoa(snap.DoneLibraries + 1), "total": strconv.Itoa(snap.TotalLibraries)}
	}
	writeRebuildThumbProgress(snap, runhandle.Frame{
		Phase:  "reading_metadata",
		Item:   lib.Path,
		Code:   code,
		Params: msgParams,
		Labels: map[string]string{"current_library": lib.Name},
	})
}

// writeRebuildThumbProgress 把一份聚合快照连同本次事件的展示信息写成**一帧**任务进度：
// 累计指标与计数由快照补齐，其余字段由调用方按本次事件填好。
//
// 外部写入点都走它，「一份快照怎么翻成一帧进度」因此只有一份实现。经 runhandle.Handle.Report
// 一次报完，理由见那里——拆开报会撕帧。
func writeRebuildThumbProgress(snap rebuildThumbSnapshot, frame runhandle.Frame) {
	if snap.Progress == nil {
		return
	}
	frame.Metrics = snap.Metrics
	if current, total, known := rebuildThumbProgressFromMetrics(snap.Metrics); known {
		frame.Current, frame.Total = &current, &total
	}
	snap.Progress.Report(frame)
}

// rebuildThumbProgressFromMetrics 把「重建缩略图」的进度算成单阶段：归档处理
// （processed+skipped / discovered）。分母只含归档处理——封面生成是**另一条运行**，
// 有自己的进度条，拼进来只会让两件事共用一根条子，而其中一件早已跑完。
//
// 还没有分母时返回 known=false：此刻一条**计数推进**也报不出来，调用方应当干脆不报，
// 而不是把总数按 0 写下去。「别动总数」因此由「不调用计数推进」表达，不需要哨兵值。
func rebuildThumbProgressFromMetrics(merged map[string]int64) (current int, total int, known bool) {
	processedArchives := merged["processed_archives"] + merged["skipped_archives"]
	discoveredArchives := merged["discovered_archives"]
	if discoveredArchives < processedArchives {
		discoveredArchives = processedArchives
	}
	current = int(processedArchives)
	total = int(discoveredArchives)
	if total <= 0 {
		return 0, 0, false
	}
	return current, total, true
}
