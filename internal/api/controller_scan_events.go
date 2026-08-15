// scanner 回调在 api 侧的落点：把批次/指标/进度事件翻成任务更新与 SSE。
//
// 一份报文最多落到两个任务上，两者的**进度句柄**各有保管处：扫描任务本身的在
// scanProgressHandles（按扫描对象索引），「重建缩略图」的在 rebuildThumbAggregator
// （逐库扫描合成单份进度）。取不到句柄就是「没有这样一个任务在跑」，本文件不另作判断。

package api

import (
	"manga-manager/internal/database"
	"manga-manager/internal/scanner"
	"path/filepath"
	"strconv"
)

func (c *Controller) handleScannerBatchEvent(action string) {
	c.invalidateDashboardStatsCache("scanner_" + action)
	if action == "scan_completed" {
		c.warmDashboardStatsCacheAsync("scanner_" + action)
	}
	c.PublishEvent(action)
}

func (c *Controller) handleScannerMetricsEvent(report scanner.ScanMetricsReport) {
	if progress := c.scanProgress.lookup(scanTargetOf(report.Scope, report.ID)); progress != nil {
		progress.MergeParams(map[string]string{
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
	c.fixateRebuildThumbBaseline(report)
}

func (c *Controller) handleScannerProgressEvent(report scanner.ScanProgressReport) {
	if progress := c.scanProgress.lookup(scanTargetOf(report.Scope, report.ID)); progress != nil {
		progress.Report(scanProgressFrame(report))
	}

	// 若正在执行缩略图重建，按全局视角同步那条任务的进度
	c.applyScannerProgressToRebuildThumbnails(report)
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

func (c *Controller) applyScannerProgressToRebuildThumbnails(report scanner.ScanProgressReport) {
	if report.Scope != "library" {
		return
	}
	snap := c.rebuildThumbAgg.applyProgress(report)
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

func (c *Controller) initRebuildThumbAggregator(progress *TaskProgress, totalLibraries int) {
	c.rebuildThumbAgg.begin(progress, totalLibraries)
}

func (c *Controller) releaseRebuildThumbAggregator() {
	c.rebuildThumbAgg.end()
}

// trackRebuildThumbLibraryProgress 在 runGlobalScan 的库切换边界更新聚合器，
// current 是已完成库数（progress 回调 i 表示"开始第 i+1 个"，i+1 表示"完成第 i+1 个"）。
func (c *Controller) trackRebuildThumbLibraryProgress(current, total int, lib database.Library) {
	c.rebuildThumbAgg.trackLibrary(current, total, lib)
}

// fixateRebuildThumbBaseline 在一个库的扫描主流程结束时把它的最终 metrics 计入 baseline，
// 并把这份报文累加进重建任务的指标与参数。
// 此刻封面队列可能仍在异步生成，之后还会有该库的 progress 事件带着同一份 metrics 的更新值到来；
// 定版后必须忽略该库的 perLibPending（直到 releaseRebuildThumbAggregator 或下一次扫描），否则会双计。
func (c *Controller) fixateRebuildThumbBaseline(report scanner.ScanMetricsReport) {
	if report.Scope != "library" {
		return
	}
	snap := c.rebuildThumbAgg.fixateLibrary(report)
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

// refreshRebuildThumbTaskFromAggregator 用聚合器中已记录的 metrics 立即刷新一次任务，
// 用于在 runGlobalScan 库切换边界（无 progress 事件携带 metrics 的时机）保持任务消息和当前库标签同步。
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
