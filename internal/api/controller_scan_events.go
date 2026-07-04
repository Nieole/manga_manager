// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的扫描事件子域，负责处理扫描器批次/指标/进度回调，并驱动缩略图重建任务的聚合进度。

package api

import (
	"fmt"
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
	taskKey := ""
	switch report.Scope {
	case "series":
		taskKey = fmt.Sprintf("scan_series_%d", report.ID)
	default:
		taskKey = fmt.Sprintf("scan_library_%d", report.ID)
	}
	c.mergeTaskParams(taskKey, map[string]string{
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
		"io_wait_ms":               strconv.FormatInt(report.IOWaitMillis, 10),
		"paused_ms":                strconv.FormatInt(report.PausedMillis, 10),
		"thumbnail_write_ms":       strconv.FormatInt(report.ThumbnailWriteMillis, 10),
		"duration_ms":              strconv.FormatInt(report.DurationMillis, 10),
	})
	c.mergeTaskParams("rebuild_thumbnails", map[string]string{
		"storage_profile":          report.StorageProfile,
		"volume_key":               report.VolumeKey,
		"archive_open_concurrency": strconv.Itoa(report.ArchiveOpenConcurrency),
		"cover_concurrency":        strconv.Itoa(report.CoverConcurrency),
	})
	c.mergeRunningTaskMetricSums("rebuild_thumbnails", map[string]int64{
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
	}, nil)
	c.fixateRebuildThumbBaseline(report)
}

func (c *Controller) handleScannerProgressEvent(report scanner.ScanProgressReport) {
	taskKey := ""
	switch report.Scope {
	case "series":
		taskKey = fmt.Sprintf("scan_series_%d", report.ID)
	default:
		taskKey = fmt.Sprintf("scan_library_%d", report.ID)
	}
	metrics := make(map[string]int64, len(report.Metrics))
	for key, value := range report.Metrics {
		metrics[key] = value
	}
	current := int(report.Current)
	total := int(report.Total)
	message := "扫描中"
	if report.CurrentItem != "" {
		message = fmt.Sprintf("扫描: %s", filepath.Base(report.CurrentItem))
	}
	c.updateTaskDetails(taskKey, current, total, message, report.Phase, report.CurrentItem, metrics, nil)

	// 若正在执行缩略图重建，按全局视角同步 rebuild_thumbnails 任务进度
	c.applyScannerProgressToRebuildThumbnails(report)
}

func (c *Controller) applyScannerProgressToRebuildThumbnails(report scanner.ScanProgressReport) {
	if report.Scope != "library" {
		return
	}
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	if agg.perLibPending == nil {
		agg.perLibPending = make(map[int64]map[string]int64)
	}
	if agg.finalizedCoverSeen == nil {
		agg.finalizedCoverSeen = make(map[int64]int64)
	}
	if _, finalized := agg.finalizedLibs[report.ID]; finalized {
		// 库 fixate 时已把当时的 generated_covers 计入 baseline。这里只把 progress 事件中
		// 新增的 generated_covers 增量补回 baseline，其它 metrics 不再变更。
		newSeen := report.Metrics["generated_covers"]
		if newSeen > agg.finalizedCoverSeen[report.ID] {
			agg.baseline["generated_covers"] += newSeen - agg.finalizedCoverSeen[report.ID]
			agg.finalizedCoverSeen[report.ID] = newSeen
		}
	} else {
		snapshot := make(map[string]int64, len(report.Metrics))
		for k, v := range report.Metrics {
			snapshot[k] = v
		}
		agg.perLibPending[report.ID] = snapshot
	}

	merged := make(map[string]int64, len(agg.baseline)+8)
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	currentLibName := agg.currentLibName
	currentLibPath := agg.currentLibPath
	doneLibs := agg.doneLibraries
	totalLibs := agg.totalLibraries
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	phase := report.Phase
	if phase == "" {
		phase = "reading_metadata"
	}
	currentItem := report.CurrentItem
	displayName := filepath.Base(report.CurrentItem)
	var message string
	switch {
	case phase == "queueing_covers" && displayName != "":
		message = fmt.Sprintf("生成缩略图: %s (已生成 %d)", displayName, merged["generated_covers"])
	case currentItem == "" && currentLibName != "":
		message = fmt.Sprintf("正在重建缩略图: %s (%d/%d 资源库)", currentLibName, doneLibs+1, totalLibs)
	case displayName != "" && currentLibName != "":
		message = fmt.Sprintf("[%s %d/%d] 重建: %s", currentLibName, doneLibs+1, totalLibs, displayName)
	case displayName != "":
		message = fmt.Sprintf("重建缩略图: %s", displayName)
	default:
		message = "正在重建缩略图"
	}
	if currentItem == "" {
		currentItem = currentLibPath
	}
	labels := map[string]string{
		"current_library": currentLibName,
	}
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, phase, currentItem, merged, labels)
}

func (c *Controller) initRebuildThumbAggregator(totalLibraries int) {
	c.rebuildThumbAggMu.Lock()
	defer c.rebuildThumbAggMu.Unlock()
	c.rebuildThumbAgg = &rebuildThumbAggregator{
		totalLibraries:     totalLibraries,
		baseline:           make(map[string]int64),
		perLibPending:      make(map[int64]map[string]int64),
		finalizedLibs:      make(map[int64]struct{}),
		finalizedCoverSeen: make(map[int64]int64),
	}
}

func (c *Controller) releaseRebuildThumbAggregator() {
	c.rebuildThumbAggMu.Lock()
	c.rebuildThumbAgg = nil
	c.rebuildThumbAggMu.Unlock()
}

// trackRebuildThumbLibraryProgress 在 runGlobalScan 的库切换边界更新聚合器，
// current 是已完成库数（progress 回调 i 表示"开始第 i+1 个"，i+1 表示"完成第 i+1 个"）。
func (c *Controller) trackRebuildThumbLibraryProgress(current, total int, lib database.Library) {
	c.rebuildThumbAggMu.Lock()
	defer c.rebuildThumbAggMu.Unlock()
	if c.rebuildThumbAgg == nil {
		c.rebuildThumbAgg = &rebuildThumbAggregator{
			baseline:           make(map[string]int64),
			perLibPending:      make(map[int64]map[string]int64),
			finalizedLibs:      make(map[int64]struct{}),
			finalizedCoverSeen: make(map[int64]int64),
		}
	}
	c.rebuildThumbAgg.totalLibraries = total
	c.rebuildThumbAgg.doneLibraries = current
	c.rebuildThumbAgg.currentLibID = lib.ID
	c.rebuildThumbAgg.currentLibName = lib.Name
	c.rebuildThumbAgg.currentLibPath = lib.Path
}

// fixateRebuildThumbBaseline 在某个库扫描"主流程"完成时被调用（cover queue 仍可能在异步中），
// 此时把该库的最终 metrics 加到 baseline，并删除 perLibPending 中对应条目。
// 注意：cover queue 异步阶段的 generatedCovers 增量会通过 progress 事件继续更新该库的 perLibPending，
// 但因为我们已把 baseline 中累计了最终值，再次出现的 perLibPending 反映的是同一份 metrics 的最新值，
// 这意味着会双计。为避免双计，fixate 后忽略后续 perLibPending（直到 release 或下次扫描）。
func (c *Controller) fixateRebuildThumbBaseline(report scanner.ScanMetricsReport) {
	if report.Scope != "library" {
		return
	}
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	if agg.baseline == nil {
		agg.baseline = make(map[string]int64)
	}
	if agg.finalizedLibs == nil {
		agg.finalizedLibs = make(map[int64]struct{})
	}
	if agg.finalizedCoverSeen == nil {
		agg.finalizedCoverSeen = make(map[int64]int64)
	}
	delete(agg.perLibPending, report.ID)
	agg.finalizedLibs[report.ID] = struct{}{}
	agg.finalizedCoverSeen[report.ID] = report.GeneratedCovers
	agg.baseline["discovered_archives"] += report.DiscoveredArchives
	agg.baseline["skipped_archives"] += report.SkippedArchives
	agg.baseline["processed_archives"] += report.ProcessedArchives
	agg.baseline["opened_archives"] += report.OpenedArchives
	agg.baseline["hashed_files"] += report.HashedFiles
	agg.baseline["queued_covers"] += report.QueuedCovers
	agg.baseline["generated_covers"] += report.GeneratedCovers
	agg.baseline["failed_archives"] += report.FailedArchives
	agg.baseline["io_wait_ms"] += report.IOWaitMillis
	agg.baseline["paused_ms"] += report.PausedMillis
	agg.baseline["thumbnail_write_ms"] += report.ThumbnailWriteMillis
	merged := make(map[string]int64, len(agg.baseline)+len(agg.perLibPending))
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	totalLibs := agg.totalLibraries
	doneLibs := agg.doneLibraries
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	message := "正在重建缩略图"
	if totalLibs > 0 {
		message = fmt.Sprintf("已完成 %d/%d 资源库", doneLibs, totalLibs)
	}
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, "queueing_covers", "", merged, nil)
}

// refreshRebuildThumbTaskFromAggregator 用聚合器中已记录的 metrics 立即刷新一次任务，
// 用于在 runGlobalScan 库切换边界（无 progress 事件携带 metrics 的时机）保持任务消息和当前库标签同步。
func (c *Controller) refreshRebuildThumbTaskFromAggregator(lib database.Library) {
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	merged := make(map[string]int64, len(agg.baseline)+8)
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	doneLibs := agg.doneLibraries
	totalLibs := agg.totalLibraries
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	var message string
	if totalLibs > 0 {
		message = fmt.Sprintf("正在重建缩略图: %s (%d/%d 资源库)", lib.Name, doneLibs+1, totalLibs)
	} else {
		message = fmt.Sprintf("正在重建缩略图: %s", lib.Name)
	}
	labels := map[string]string{"current_library": lib.Name}
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, "reading_metadata", lib.Path, merged, labels)
}

// refreshRebuildThumbTaskMessage 在阶段切换（如等待封面队列收尾）时刷新任务消息和阶段，
// 但保留聚合器累计的 current/total（避免被旧的占位 total 重置成 100%）。
func (c *Controller) refreshRebuildThumbTaskMessage(message, phase string) {
	c.rebuildThumbAggMu.Lock()
	agg := c.rebuildThumbAgg
	if agg == nil {
		c.rebuildThumbAggMu.Unlock()
		return
	}
	merged := make(map[string]int64, len(agg.baseline)+8)
	for k, v := range agg.baseline {
		merged[k] = v
	}
	for _, pending := range agg.perLibPending {
		for k, v := range pending {
			merged[k] += v
		}
	}
	c.rebuildThumbAggMu.Unlock()

	current, total := rebuildThumbProgressFromMetrics(merged)
	c.updateTaskDetails("rebuild_thumbnails", current, total, message, phase, "", merged, nil)
}

// rebuildThumbProgressFromMetrics 把"重建缩略图"任务的进度展开成两阶段：
// 归档处理 (processed+skipped/discovered) 和封面生成 (generated/queued)，分别贡献分子分母。
// 这样归档全部入队时进度只走到 ~50%，cover queue 异步生成时进度继续推进，避免视觉上"过早 100%"。
func rebuildThumbProgressFromMetrics(merged map[string]int64) (int, int) {
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
	current := int(processedArchives + generatedCovers)
	total := int(discoveredArchives + queuedCovers)
	if total < current {
		total = current
	}
	if total <= 0 {
		return current, -1
	}
	return current, total
}
