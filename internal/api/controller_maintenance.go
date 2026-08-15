// 本文件由 controller.go 拆分而来，属于后端 API 层的维护任务子域，负责全库扫描、索引重建、缩略图重建/清理、文件指纹重建与低优先级全量哈希回填等运维任务的编排与接口。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/koreader"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// triggerGlobalScan 对所有资料库做一次强制全量扫描，返回前等待全部完成。
//
// 旧实现给每个库起一个裸 goroutine 就直接返回：这些 goroutine 不在 backgroundWG 里，
// 优雅停机不会等它们，进程退出时 sql.DB 已关而扫描还在写库。改为受调用方 ctx 约束、
// 同步等待——调用方本就跑在 runBackground 里，等待不会阻塞任何请求。
func (c *Controller) triggerGlobalScan(ctx context.Context) {
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		slog.Error("Global scan aborted: failed to list libraries", "error", err)
		return
	}

	var wg sync.WaitGroup
	for _, lib := range libs {
		wg.Add(1)
		go func(lib database.Library) {
			defer wg.Done()
			defer c.purgeReadingPathCaches()
			if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, true); err != nil {
				slog.Error("Global scan of library failed", "library_id", lib.ID, "path", lib.Path, "error", err)
			}
		}(lib)
	}
	wg.Wait()
}

// clearThumbnailDir 清空缩略图目录，但保留页图磁盘缓存子目录。
//
// 缩略图目录就是 cache.dir 本身，而页图磁盘缓存在 <cache.dir>/pages/ 下，二者必须分开清：
// 直接 os.RemoveAll(cache.dir) 会连页图缓存一并抹掉，用户只想修封面，代价却是之后
// 每一页都要重新解码转码一遍。
func (c *Controller) clearThumbnailDir(thumbDir string) error {
	pageCacheDir := filepath.Clean(c.processedImageCacheDir())

	entries, err := os.ReadDir(thumbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		full := filepath.Join(thumbDir, entry.Name())
		if entry.IsDir() && filepath.Clean(full) == pageCacheDir {
			continue // 页图缓存与缩略图无关，别误伤
		}
		if err := os.RemoveAll(full); err != nil {
			return err
		}
	}
	return nil
}

// clearAllCoverPaths 把数据库中 books 与 series_stats 的 cover_path 字段清空，
// 用于"重建缩略图缓存"任务在删盘后强制让 scanner 重新生成所有缩略图。
func (c *Controller) clearAllCoverPaths(ctx context.Context) error {
	if err := c.store.ClearAllBookCoverPaths(ctx); err != nil {
		return fmt.Errorf("clear book cover paths: %w", err)
	}
	if err := c.store.ClearAllSeriesStatsCoverPaths(ctx); err != nil {
		return fmt.Errorf("clear series cover paths: %w", err)
	}
	return nil
}

// runGlobalScan 依次强制重扫全部资料库。
//
// ignoreFormatFilter 只在「重建缩略图」时为真：该任务先删光缩略图文件并清空所有 cover_path，
// 再靠这次扫描重建。若仍按库的 scan_formats 过滤，被排除格式的书就再也不会被访问到，
// 它们的封面会永久消失——而格式过滤的语义是「导入哪些文件」，不该殃及已入库的内容。
func (c *Controller) runGlobalScan(ctx context.Context, force bool, ignoreFormatFilter bool, progress func(current, total int, lib database.Library)) error {
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	total := len(libs)
	for i, lib := range libs {
		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if progress != nil {
			progress(i, total, lib)
		}
		if err := c.scanner.ScanLibraryWithOptions(ctx, lib.ID, lib.Path, scanner.LibraryScanOptions{
			Force:              force,
			IgnoreFormatFilter: ignoreFormatFilter,
		}); err != nil {
			return err
		}
		c.purgeReadingPathCaches()
		if progress != nil {
			progress(i+1, total, lib)
		}
	}
	return nil
}

// launchRebuildIndexTask 异步重建 FTS 索引并随后做一次全量扫描。
//
// 必须整体作为可取消的后台任务跑，且派生的扫描要挂在 backgroundWG 上、不能裸 goroutine：
// FTS 重灌与随后的全库扫描在大库上都可能跑到分钟级，同步做会把请求一直挂到反代超时；
// 脱离 backgroundWG 的话，Close() 不会等它，进程退出后 store.Close() 关闭 sql.DB
// 时扫描可能仍在写库。
func (c *Controller) launchRebuildIndexTask() error {
	if !c.taskEngine.startTaskMsg("rebuild_index", "rebuild_index", "task.msg.rebuild_index.start", nil, 1) {
		return errTaskAlreadyRunning
	}
	c.taskEngine.setTaskMetadata("rebuild_index", nil, "")

	taskCtx, release := c.taskEngine.newTaskContext("rebuild_index")
	c.runBackgroundTask("rebuild_index", func() {
		defer release()

		if err := c.store.RebuildSeriesSearchIndex(taskCtx); err != nil {
			c.taskEngine.failTaskWithError("rebuild_index", fmt.Sprintf("SQLite series search index rebuild failed: %v", err), err.Error())
			return
		}
		if err := c.store.RebuildBookSearchIndex(taskCtx); err != nil {
			c.taskEngine.failTaskWithError("rebuild_index", fmt.Sprintf("SQLite book search index rebuild failed: %v", err), err.Error())
			return
		}
		if err := taskCtx.Err(); err != nil {
			c.taskEngine.failTaskWithError("rebuild_index", "Search index rebuild cancelled", err.Error())
			return
		}

		c.triggerGlobalScan(taskCtx)
		c.taskEngine.finishTaskMsg("rebuild_index", "task.msg.rebuild_index.complete", nil)
	})
	return nil
}

func (c *Controller) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildIndexTask(); err != nil {
		if strings.Contains(err.Error(), "already running") {
			jsonResponse(w, http.StatusConflict, map[string]string{"error": "A search index rebuild is already running"})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to rebuild search index")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.search_index_rebuilt")})
}

// launchRebuildThumbnailsTask 是缩略图重建任务的启动点，走引擎的启动入口。
//
// 任务体开工第一件事是把进度句柄交给 rebuildThumbAggregator：这个任务的进度由任务体
// 之外写入，所有权模型见那里。
func (c *Controller) launchRebuildThumbnailsTask() error {
	cfg := c.currentConfig()
	policy := config.ResolveStoragePolicy(cfg, "")
	thumbDir := filepath.Join(".", "data", "thumbnails")
	if cfg.Cache.Dir != "" {
		thumbDir = cfg.Cache.Dir
	}

	spec := TaskSpec{
		Key:       "rebuild_thumbnails",
		Type:      "rebuild_thumbnails",
		StartCode: "task.msg.rebuild_thumbnails.start",
		CanCancel: true,
		CanPause:  true,
		Metadata: map[string]string{
			"storage_profile":   policy.StorageProfile,
			"volume_key":        policy.VolumeKey,
			"cover_concurrency": strconv.Itoa(policy.IOPolicy.CoverConcurrency),
			"execution_mode":    "low_impact",
		},
		Limits:       c.taskLimitsForPath("", true),
		CompleteCode: "task.msg.rebuild_thumbnails.complete",
		CancelCode:   "task.msg.rebuild_thumbnails.cancelled",
		FailCode:     "task.msg.rebuild_thumbnails.failed",
	}

	if err := c.taskEngine.Run(spec, func(ctx context.Context, tp *TaskProgress) (TaskResult, error) {
		c.initRebuildThumbAggregator(tp, 0)
		defer c.releaseRebuildThumbAggregator()

		tp.Report(TaskFrame{Phase: "clearing_cache", Item: thumbDir, Code: "task.msg.rebuild_thumbnails.clearing_cache"})
		if err := c.clearThumbnailDir(thumbDir); err != nil {
			return rebuildThumbFailure("task.msg.rebuild_thumbnails.clear_cache_failed", err), err
		}
		if err := taskcontrol.Wait(ctx); err != nil {
			return TaskResult{}, err
		}
		if err := os.MkdirAll(thumbDir, 0o755); err != nil {
			return rebuildThumbFailure("task.msg.rebuild_thumbnails.mkdir_failed", err), err
		}
		tp.Phase("clearing_cache", "task.msg.rebuild_thumbnails.clearing_cover_index", nil)
		if err := c.clearAllCoverPaths(ctx); err != nil {
			return rebuildThumbFailure("task.msg.rebuild_thumbnails.clear_cover_index_failed", err), err
		}
		tp.Phase("reading_metadata", "task.msg.rebuild_thumbnails.rebuilding_low_impact", nil)
		if err := c.runGlobalScan(ctx, true, true /* 重建缩略图必须看得见全部已入库的书 */, func(current, total int, lib database.Library) {
			c.trackRebuildThumbLibraryProgress(current, total, lib)
			c.refreshRebuildThumbTaskFromAggregator(lib)
		}); err != nil {
			return TaskResult{}, err
		}
		c.refreshRebuildThumbTaskMessage("task.msg.rebuild_thumbnails.waiting_cover_queue", nil, "queueing_covers")
		if err := c.scanner.WaitForCoverQueue(ctx); err != nil {
			return rebuildThumbFailure("task.msg.rebuild_thumbnails.wait_queue_failed", err), err
		}
		c.warmDashboardStatsCacheAsync("rebuild_thumbnails_completed")
		return TaskResult{}, nil
	}); err != nil {
		return err
	}
	c.PublishEvent("refresh_thumbnails")
	return nil
}

// rebuildThumbFailure 给一个失败原因配上它专属的文案码，取消除外。
//
// 重建的几个工序各有各的失败文案，而取消同样以 ctx.Err() 的形式从这些调用里返回；
// TaskResult 的文案覆盖对 settleTask 裁决出的每条分支一视同仁，无条件带上码的话，
// 用户按下取消看到的会是「清空封面索引失败」而不是「已取消」。
func rebuildThumbFailure(code string, err error) TaskResult {
	if errors.Is(err, context.Canceled) {
		return TaskResult{}
	}
	return TaskResult{Code: code}
}

func (c *Controller) rebuildThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildThumbnailsTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A thumbnail rebuild is already running"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.thumbnails_rebuilding")})
}

func (c *Controller) launchCleanupThumbnailsTask() error {
	if !c.taskEngine.startPausableCancelableTaskMsg("cleanup_thumbnails", "cleanup_thumbnails", "task.msg.cleanup_thumbnails.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	taskCtx, cleanupCancel := c.taskEngine.newTaskContext("cleanup_thumbnails")
	c.taskEngine.setTaskMetadata("cleanup_thumbnails", nil, "")

	// 这里曾写成 `go c.runBackground(...)`：多套一层 goroutine 会让 runBackground 的
	// closed 检查与 backgroundWG.Add 都发生在另一个调度点上，停机竞态下任务会被静默丢弃，
	// 而 newTaskContext 已登记的 runtime 记录留在内存里泄漏。
	c.runBackgroundTask("cleanup_thumbnails", func() {
		defer cleanupCancel()

		c.taskEngine.updateTaskDetailsMsg("cleanup_thumbnails", 0, -1, "task.msg.cleanup_thumbnails.scanning", nil, "cleanup", "", nil, nil)

		err := c.scanner.CleanupThumbnails(taskCtx, func(deleted, scanned int, msg string) {
			c.taskEngine.updateTaskDetails("cleanup_thumbnails", deleted, scanned, msg, "cleanup", "", nil, nil)
		})

		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg("cleanup_thumbnails", "cancelled", "task.msg.cleanup_thumbnails.cancelled", nil)
			return
		}
		if err != nil {
			c.taskEngine.failTaskErrMsg("cleanup_thumbnails", "task.msg.cleanup_thumbnails.failed", nil, err.Error())
			return
		}
		c.taskEngine.finishTaskMsg("cleanup_thumbnails", "task.msg.cleanup_thumbnails.complete", nil)
	})
	return nil
}

func (c *Controller) cleanupThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchCleanupThumbnailsTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A thumbnail cleanup is already running"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.cover_cleanup_started")})
}

func (c *Controller) launchRebuildFileIdentitiesTask() error {
	if !c.taskEngine.startPausableCancelableTaskMsg("rebuild_file_identities", "rebuild_file_identities", "task.msg.rebuild_file_identities.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	c.taskEngine.setTaskMetadata("rebuild_file_identities", map[string]string{"profile": "quick_hash"}, "")
	c.taskEngine.setTaskEffectiveLimit("rebuild_file_identities", c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.taskEngine.newTaskContext("rebuild_file_identities")

	c.runBackgroundTask("rebuild_file_identities", func() {
		defer cleanupCancel()
		updated, total, err := c.runRebuildFileIdentities(taskCtx, 500, func(current, total int, _ string, metrics taskIOMetrics) {
			c.taskEngine.updateTaskDetailsMsg("rebuild_file_identities", current, total, "task.msg.rebuild_file_identities.progress", map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)}, "hashing", "", map[string]int64{
				"hashed_files": metrics.HashedFiles,
				"io_wait_ms":   metrics.IOWaitMillis,
				"paused_ms":    metrics.PausedMillis,
			}, map[string]string{
				"storage_profile": metrics.StorageProfile,
				"volume_key":      metrics.VolumeKey,
			})
			c.taskEngine.mergeTaskParams("rebuild_file_identities", taskIOMetricsParams(metrics))
		})
		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg("rebuild_file_identities", "cancelled", "task.msg.rebuild_file_identities.cancelled", nil)
			return
		}
		if err != nil {
			c.taskEngine.failTaskErrMsg("rebuild_file_identities", "task.msg.rebuild_file_identities.failed", nil, err.Error())
			return
		}
		c.taskEngine.finishTaskMsg("rebuild_file_identities", "task.msg.rebuild_file_identities.complete", map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)})
	})
	return nil
}

func (c *Controller) runRebuildFileIdentities(ctx context.Context, limit int, progress func(current, total int, message string, metrics taskIOMetrics)) (int, int, error) {
	if limit <= 0 {
		limit = 500
	}
	missingCount, err := c.store.CountBooksMissingQuickHash(ctx)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	metrics := taskIOMetrics{}
	var afterID int64
	for {
		if err := taskcontrol.Wait(ctx); err != nil {
			return updated, total, err
		}
		books, err := c.store.ListBooksMissingQuickHashBatch(ctx, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			policy, releaseToken, waited, paused, tokenErr := c.acquireTaskStorageToken(ctx, book.LibraryPath, storageio.WorkKindIdentityHash)
			if tokenErr != nil {
				return updated, total, tokenErr
			}
			if waited > 0 {
				metrics.IOWaitMillis += waited.Milliseconds()
			}
			if paused > 0 {
				metrics.PausedMillis += paused.Milliseconds()
			}
			metrics.StorageProfile = policy.StorageProfile
			metrics.VolumeKey = policy.VolumeKey
			quickHash, err := koreader.FingerprintQuickFile(book.Path)
			releaseToken()
			metrics.HashedFiles++
			if err != nil {
				slog.Warn("Failed to quick-fingerprint book", "book_id", book.ID, "path", book.Path, "error", err)
				afterID = book.ID
				continue
			}
			if err := c.store.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
				ID:        book.ID,
				QuickHash: quickHash,
			}); err != nil {
				return updated, total, err
			}

			updated++
			afterID = book.ID
			if progress != nil {
				// 展示文案由上层按 message code 本地化渲染，这里只上报进度值与指标。
				progress(updated, total, "", metrics)
			}
		}
	}
	return updated, total, nil
}

func (c *Controller) launchLowPriorityBookHashBackfillTask(reason string) bool {
	cfg := c.currentConfig()
	if !cfg.KOReader.Enabled || cfg.KOReader.MatchMode != config.KOReaderMatchModeBinaryHash {
		return false
	}

	missingCount, err := c.store.CountBooksMissingIdentity(context.Background(), config.KOReaderMatchModeBinaryHash)
	if err != nil {
		slog.Warn("Failed to count missing full hashes for background backfill", "error", err)
		return false
	}
	if missingCount == 0 {
		return false
	}

	if !c.taskEngine.startPausableCancelableTaskMsg(lowPriorityBookHashTaskKey, "rebuild_book_hashes", "task.msg.book_hash_backfill.start", nil, int(missingCount)) {
		return false
	}
	c.taskEngine.setTaskMetadata(lowPriorityBookHashTaskKey, map[string]string{
		"match_mode": config.KOReaderMatchModeBinaryHash,
		"profile":    "full_hash_low_priority",
		"reason":     reason,
	}, "")
	c.taskEngine.setTaskEffectiveLimit(lowPriorityBookHashTaskKey, c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.taskEngine.newTaskContext(lowPriorityBookHashTaskKey)

	c.runBackgroundTask(lowPriorityBookHashTaskKey, func() {
		// 必须 defer：这里曾是裸调用，写在任务体跑完之后。任务体一旦 panic 就走不到那一行，
		// newTaskContext 登记的运行时句柄连同它持有的 cancel 一起泄漏在内存里。
		defer cleanupCancel()
		updated, total, err := c.runBackfillFullHashesLowPriority(taskCtx, lowPriorityBookHashBatchSize, lowPriorityBookHashBatchGap, func(current, total int, _ string, metrics taskIOMetrics) {
			c.taskEngine.updateTaskDetailsMsg(lowPriorityBookHashTaskKey, current, total, "task.msg.book_hash_backfill.progress", map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)}, "hashing", "", map[string]int64{
				"hashed_files": metrics.HashedFiles,
				"io_wait_ms":   metrics.IOWaitMillis,
				"paused_ms":    metrics.PausedMillis,
			}, map[string]string{
				"storage_profile": metrics.StorageProfile,
				"volume_key":      metrics.VolumeKey,
			})
			c.taskEngine.mergeTaskParams(lowPriorityBookHashTaskKey, taskIOMetricsParams(metrics))
		})
		if errors.Is(err, context.Canceled) {
			c.taskEngine.completeTaskMsg(lowPriorityBookHashTaskKey, "cancelled", "task.msg.book_hash_backfill.cancelled", nil)
			return
		}
		if err != nil {
			c.taskEngine.failTaskErrMsg(lowPriorityBookHashTaskKey, "task.msg.book_hash_backfill.failed", nil, err.Error())
			return
		}
		c.taskEngine.finishTaskMsg(lowPriorityBookHashTaskKey, "task.msg.book_hash_backfill.complete", map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)})
	})
	return true
}

func (c *Controller) runBackfillFullHashesLowPriority(ctx context.Context, limit int, batchGap time.Duration, progress func(current, total int, message string, metrics taskIOMetrics)) (int, int, error) {
	if limit <= 0 {
		limit = lowPriorityBookHashBatchSize
	}
	missingCount, err := c.store.CountBooksMissingIdentity(ctx, config.KOReaderMatchModeBinaryHash)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	metrics := taskIOMetrics{}
	var afterID int64
	for {
		if err := taskcontrol.Wait(ctx); err != nil {
			return updated, total, err
		}
		books, err := c.store.ListBooksMissingIdentityBatch(ctx, config.KOReaderMatchModeBinaryHash, afterID, limit)
		if err != nil {
			return updated, total, err
		}
		if len(books) == 0 {
			break
		}

		for _, book := range books {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			policy, releaseToken, waited, paused, tokenErr := c.acquireTaskStorageToken(ctx, book.LibraryPath, storageio.WorkKindIdentityHash)
			if tokenErr != nil {
				return updated, total, tokenErr
			}
			if waited > 0 {
				metrics.IOWaitMillis += waited.Milliseconds()
			}
			if paused > 0 {
				metrics.PausedMillis += paused.Milliseconds()
			}
			metrics.StorageProfile = policy.StorageProfile
			metrics.VolumeKey = policy.VolumeKey
			fileHash, err := koreader.FingerprintFileContext(ctx, book.Path)
			releaseToken()
			metrics.HashedFiles++
			if err != nil {
				slog.Warn("Failed to backfill full book hash", "book_id", book.ID, "path", book.Path, "error", err)
				afterID = book.ID
				continue
			}
			if err := c.store.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
				ID:       book.ID,
				FileHash: fileHash,
			}); err != nil {
				return updated, total, err
			}

			updated++
			afterID = book.ID
			if progress != nil {
				// 展示文案由上层按 message code 本地化渲染，这里只上报进度值与指标。
				progress(updated, total, "", metrics)
			}
		}

		if batchGap > 0 {
			if err := taskcontrol.Wait(ctx); err != nil {
				return updated, total, err
			}
			timer := time.NewTimer(batchGap)
			select {
			case <-timer.C:
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return updated, total, ctx.Err()
			}
		}
	}
	return updated, total, nil
}

func (c *Controller) rebuildFileIdentities(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildFileIdentitiesTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A file identity rebuild is already running"})
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "maintenance.file_identity_rebuild_started")})
}
