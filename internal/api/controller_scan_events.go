package api

import (
	"path/filepath"
	"strconv"

	"manga-manager/internal/application/scanning"
	"manga-manager/internal/database"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/scanner"
)

func (c *Controller) handleScannerBatchEvent(action string) { c.scans.BatchEvent(action) }

// newTaskScanObserver is the compatibility observer for maintenance tasks.
func newTaskScanObserver(progress *runhandle.Handle) scanner.ScanObserver {
	return scanning.NewObserver(progress)
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
// 而这条运行跨着全部资料库，理由见 launchRebuildThumbnailsTask。哪个数走指标、哪个走参数，
// 判据见 scanning.NewObserver。
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

func (c *Controller) initRebuildThumbAggregator(progress *runhandle.Handle, totalLibraries int) {
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
