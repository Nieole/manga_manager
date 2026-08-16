// scanner 报文在 api 侧的落点：把批次事件翻成缓存失效与 SSE，把进度与指标翻成任务更新。
//
// 这里定义两个**扫描观察者**——发起扫描的一方交出哪一个，就决定了这次扫描的报文写到哪：
// 资料库/系列扫描任务交出 taskScanObserver，「重建缩略图」逐库交出 rebuildThumbLibrary。
// 无归属的扫描（守护扫描、watcher 派生、建库后首扫）交出 nil，报文无处可报。

package api

import (
	"path/filepath"
	"strconv"

	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
)

func (c *Controller) handleScannerBatchEvent(action string) {
	c.invalidateDashboardStatsCache("scanner_" + action)
	if action == "scan_completed" {
		c.warmDashboardStatsCacheAsync("scanner_" + action)
	}
	c.PublishEvent(action)
}

// taskScanObserver 把一次扫描的报文写进它所属任务的**进度句柄**。
//
// 它只是句柄的一层翻译，不持有任何状态：谁有资格写这个任务，由「谁拿到了这个句柄」回答，
// 与句柄本身的所有权模型一致（见 TaskProgress）。
type taskScanObserver struct {
	progress *TaskProgress
}

// newTaskScanObserver 把进度句柄包成一个扫描观察者；句柄为 nil 时返回 nil 接口值，
// 于是「这次扫描不属于任何任务」在扫描器那边与其余无归属扫描是同一个形状。
func newTaskScanObserver(progress *TaskProgress) scanner.ScanObserver {
	if progress == nil {
		return nil
	}
	return &taskScanObserver{progress: progress}
}

func (o *taskScanObserver) Progress(report scanner.ScanProgressReport) {
	o.progress.Report(scanProgressFrame(report))
}

func (o *taskScanObserver) Metrics(report scanner.ScanMetricsReport) {
	o.progress.MergeParams(map[string]string{
		"storage_profile":          report.StorageProfile,
		"volume_key":               report.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(report.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(report.CoverConcurrency),
		"discovered_archives":      strconv.FormatInt(report.DiscoveredArchives, 10),
		"skipped_archives":         strconv.FormatInt(report.SkippedArchives, 10),
		"processed_archives":       strconv.FormatInt(report.ProcessedArchives, 10),
		"opened_archives":          strconv.FormatInt(report.OpenedArchives, 10),
		"hashed_files":             strconv.FormatInt(report.HashedFiles, 10),
		"queued_covers":            strconv.FormatInt(report.QueuedCovers, 10),
		"generated_covers":         strconv.FormatInt(report.GeneratedCovers, 10),
		"failed_archives":          strconv.FormatInt(report.FailedArchives, 10),
		"rehomed_books":            strconv.FormatInt(report.RehomedBooks, 10),
		"stale_series_stats":       strconv.FormatInt(report.StaleSeriesStats, 10),
		"format_filtered_archives": strconv.FormatInt(report.FormatFilteredArchives, 10),
		"io_wait_ms":               strconv.FormatInt(report.IOWaitMillis, 10),
		"paused_ms":                strconv.FormatInt(report.PausedMillis, 10),
		"thumbnail_write_ms":       strconv.FormatInt(report.ThumbnailWriteMillis, 10),
		"duration_ms":              strconv.FormatInt(report.DurationMillis, 10),
	})
}

// scanProgressFrame 把扫描器的一份进度报文翻成**一帧**任务进度。
//
// 一份报文里计数、阶段、当前条目与指标同时变，只能整帧报（理由见 TaskProgress.Report）。
func scanProgressFrame(report scanner.ScanProgressReport) TaskFrame {
	metrics := make(map[string]int64, len(report.Metrics))
	for key, value := range report.Metrics {
		metrics[key] = value
	}
	current := int(report.Current)
	total := int(report.Total)
	frame := TaskFrame{
		Current: &current,
		Total:   &total,
		Phase:   report.Phase,
		Item:    report.CurrentItem,
		Code:    "task.msg.scan.scanning",
		Metrics: metrics,
	}
	if report.CurrentItem != "" {
		frame.Code = "task.msg.scan.scanning_item"
		frame.Params = map[string]string{"item": filepath.Base(report.CurrentItem)}
	}
	return frame
}

// Progress 把本库的一份进度报文并进跨库聚合，再按全局视角写一帧重建任务的进度。
func (l *rebuildThumbLibrary) Progress(report scanner.ScanProgressReport) {
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
	case phase == "queueing_covers" && displayName != "":
		code = "task.msg.rebuild_thumbnails.generating_cover"
		msgParams = map[string]string{"item": displayName, "generated": strconv.FormatInt(snap.Metrics["generated_covers"], 10)}
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
	writeRebuildThumbProgress(snap, TaskFrame{
		Phase:  phase,
		Item:   currentItem,
		Code:   code,
		Params: msgParams,
		Labels: map[string]string{"current_library": currentLibName},
	})
}

// Metrics 在本库扫描主流程结束时定版它的指标，并把这份报文累加进重建任务。
func (l *rebuildThumbLibrary) Metrics(report scanner.ScanMetricsReport) {
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
		"generated_covers":    report.GeneratedCovers,
		"failed_archives":     report.FailedArchives,
		"io_wait_ms":          report.IOWaitMillis,
		"paused_ms":           report.PausedMillis,
		"thumbnail_write_ms":  report.ThumbnailWriteMillis,
		"duration_ms":         report.DurationMillis,
	}, map[string]string{
		"storage_profile":          report.StorageProfile,
		"volume_key":               report.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(report.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(report.CoverConcurrency),
	})

	code := "task.msg.rebuild_thumbnails.rebuilding"
	var msgParams map[string]string
	if snap.TotalLibraries > 0 {
		code = "task.msg.rebuild_thumbnails.libraries_completed"
		msgParams = map[string]string{"done": strconv.Itoa(snap.DoneLibraries), "total": strconv.Itoa(snap.TotalLibraries)}
	}
	writeRebuildThumbProgress(snap, TaskFrame{
		Phase:  "queueing_covers",
		Code:   code,
		Params: msgParams,
	})
}

func (c *Controller) initRebuildThumbAggregator(progress *TaskProgress, totalLibraries int) {
	c.rebuildThumbAgg.begin(progress, totalLibraries)
}

func (c *Controller) releaseRebuildThumbAggregator() {
	c.rebuildThumbAgg.end()
}

// beginRebuildThumbLibrary 是交给 runGlobalScan 的观察者工厂：开始一个资料库，
// 返回这次扫描的**扫描观察者**；重建未在进行时返回 nil 接口值。
func (c *Controller) beginRebuildThumbLibrary(lib database.Library, totalLibraries int) scanner.ScanObserver {
	entry := c.rebuildThumbAgg.beginLibrary(lib, totalLibraries)
	if entry == nil {
		return nil
	}
	c.refreshRebuildThumbTaskFromAggregator(lib)
	return entry
}

// refreshRebuildThumbTaskFromAggregator 用聚合器中已记录的 metrics 立即刷新一次任务，
// 用于在库切换边界（此刻还没有任何本库报文携带 metrics）把任务消息与当前库标签同步过去。
func (c *Controller) refreshRebuildThumbTaskFromAggregator(lib database.Library) {
	snap := c.rebuildThumbAgg.snapshot()
	if snap.Progress == nil {
		return
	}
	code := "task.msg.rebuild_thumbnails.rebuilding_library"
	msgParams := map[string]string{"lib": lib.Name}
	if snap.TotalLibraries > 0 {
		code = "task.msg.rebuild_thumbnails.rebuilding_library_progress"
		msgParams = map[string]string{"lib": lib.Name, "done": strconv.Itoa(snap.DoneLibraries + 1), "total": strconv.Itoa(snap.TotalLibraries)}
	}
	writeRebuildThumbProgress(snap, TaskFrame{
		Phase:  "reading_metadata",
		Item:   lib.Path,
		Code:   code,
		Params: msgParams,
		Labels: map[string]string{"current_library": lib.Name},
	})
}

// refreshRebuildThumbTaskMessage 在阶段切换（如等待封面队列收尾）时刷新任务消息和阶段，
// 但保留聚合器累计的 current/total——用占位 total 覆盖会把进度条重置成 100%。
func (c *Controller) refreshRebuildThumbTaskMessage(code string, params map[string]string, phase string) {
	snap := c.rebuildThumbAgg.snapshot()
	if snap.Progress == nil {
		return
	}
	writeRebuildThumbProgress(snap, TaskFrame{
		Phase:  phase,
		Code:   code,
		Params: params,
	})
}

// writeRebuildThumbProgress 把一份聚合快照连同本次事件的展示信息写成**一帧**任务进度：
// 累计指标与两阶段计数由快照补齐，其余字段由调用方按本次事件填好。
//
// 外部写入点都走它，「一份快照怎么翻成一帧进度」因此只有一份实现。经 TaskProgress.Report
// 一次报完，理由见那里——拆开报会撕帧。
func writeRebuildThumbProgress(snap rebuildThumbSnapshot, frame TaskFrame) {
	if snap.Progress == nil {
		return
	}
	frame.Metrics = snap.Metrics
	if current, total, known := rebuildThumbProgressFromMetrics(snap.Metrics); known {
		frame.Current, frame.Total = &current, &total
	}
	snap.Progress.Report(frame)
}

// rebuildThumbProgressFromMetrics 把"重建缩略图"任务的进度展开成两阶段：
// 归档处理 (processed+skipped/discovered) 和封面生成 (generated/queued)，分别贡献分子分母。
// 这样归档全部入队时进度只走到 ~50%，cover queue 异步生成时进度继续推进，避免视觉上"过早 100%"。
//
// 两阶段都还没有分母时返回 known=false：此刻一条**计数推进**也报不出来，调用方应当干脆不报，
// 而不是把总数按 0 写下去。「别动总数」因此由「不调用计数推进」表达，不需要哨兵值。
func rebuildThumbProgressFromMetrics(merged map[string]int64) (current int, total int, known bool) {
	processedArchives := merged["processed_archives"] + merged["skipped_archives"]
	discoveredArchives := merged["discovered_archives"]
	if discoveredArchives < processedArchives {
		discoveredArchives = processedArchives
	}
	generatedCovers := merged["generated_covers"]
	queuedCovers := merged["queued_covers"]
	if queuedCovers < generatedCovers {
		queuedCovers = generatedCovers
	}
	current = int(processedArchives + generatedCovers)
	total = int(discoveredArchives + queuedCovers)
	if total < current {
		total = current
	}
	if total <= 0 {
		return 0, 0, false
	}
	return current, total, true
}
