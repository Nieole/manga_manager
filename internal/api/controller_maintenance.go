// 业务说明：本文件由 controller.go 拆分而来，属于后端 API 层的维护任务子域，负责全库扫描、索引重建、缩略图重建/清理、文件指纹重建与低优先级全量哈希回填等运维任务的编排与接口。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/koreader"
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

func (c *Controller) runGlobalScan(ctx context.Context, force bool, progress func(current, total int, lib database.Library)) error {
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
		if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, force); err != nil {
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
// 此前这三步全在 HTTP 请求 goroutine 里同步跑：FTS 是 DELETE + INSERT...SELECT 全表重灌，
// 大库上会把请求挂到结束（反代通常先 504），期间任务显示 running、进度恒为 0 且不可取消；
// 随后的 `go c.triggerGlobalScan(context.Background())` 又派生出一批完全脱离
// backgroundWG 的扫描 goroutine，Close() 不会等它们，main 返回后 store.Close() 关掉
// sql.DB 而扫描仍在写库，产生 "sql: database is closed" 与半截写入。
func (c *Controller) launchRebuildIndexTask() error {
	if !c.startTaskMsg("rebuild_index", "rebuild_index", "task.msg.rebuild_index.start", nil, 1) {
		return errTaskAlreadyRunning
	}
	c.setTaskMetadata("rebuild_index", nil, "")

	taskCtx, release := c.newTaskContext("rebuild_index")
	c.runBackground(func() {
		defer release()

		if err := c.store.RebuildSeriesSearchIndex(taskCtx); err != nil {
			c.failTaskWithError("rebuild_index", fmt.Sprintf("SQLite series search index rebuild failed: %v", err), err.Error())
			return
		}
		if err := c.store.RebuildBookSearchIndex(taskCtx); err != nil {
			c.failTaskWithError("rebuild_index", fmt.Sprintf("SQLite book search index rebuild failed: %v", err), err.Error())
			return
		}
		if err := taskCtx.Err(); err != nil {
			c.failTaskWithError("rebuild_index", "Search index rebuild cancelled", err.Error())
			return
		}

		c.triggerGlobalScan(taskCtx)
		c.finishTaskMsg("rebuild_index", "task.msg.rebuild_index.complete", nil)
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

func (c *Controller) launchRebuildThumbnailsTask() error {
	if !c.startPausableCancelableTaskMsg("rebuild_thumbnails", "rebuild_thumbnails", "task.msg.rebuild_thumbnails.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	policy := config.ResolveStoragePolicy(c.currentConfig(), "")
	c.setTaskMetadata("rebuild_thumbnails", map[string]string{
		"storage_profile":   policy.StorageProfile,
		"volume_key":        policy.VolumeKey,
		"cover_concurrency": strconv.Itoa(policy.IOPolicy.CoverConcurrency),
		"execution_mode":    "low_impact",
	}, "")
	c.setTaskEffectiveLimit("rebuild_thumbnails", c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext("rebuild_thumbnails")

	thumbDir := filepath.Join(".", "data", "thumbnails")
	cfg := c.currentConfig()
	if cfg.Cache.Dir != "" {
		thumbDir = cfg.Cache.Dir
	}

	c.runBackground(func() {
		defer cleanupCancel()
		defer c.releaseRebuildThumbAggregator()
		c.initRebuildThumbAggregator(0)
		c.updateTaskDetailsMsg("rebuild_thumbnails", 0, 0, "task.msg.rebuild_thumbnails.clearing_cache", nil, "clearing_cache", thumbDir, nil, nil)
		if err := os.RemoveAll(thumbDir); err != nil {
			c.failTaskErrMsg("rebuild_thumbnails", "task.msg.rebuild_thumbnails.clear_cache_failed", nil, err.Error())
			return
		}
		if err := taskcontrol.Wait(taskCtx); errors.Is(err, context.Canceled) {
			c.completeTaskMsg("rebuild_thumbnails", "cancelled", "task.msg.rebuild_thumbnails.cancelled", nil)
			return
		}
		if err := os.MkdirAll(thumbDir, 0o755); err != nil {
			c.failTaskErrMsg("rebuild_thumbnails", "task.msg.rebuild_thumbnails.mkdir_failed", nil, err.Error())
			return
		}
		c.updateTaskDetailsMsg("rebuild_thumbnails", 0, -1, "task.msg.rebuild_thumbnails.clearing_cover_index", nil, "clearing_cache", "", nil, nil)
		if err := c.clearAllCoverPaths(taskCtx); err != nil {
			c.failTaskErrMsg("rebuild_thumbnails", "task.msg.rebuild_thumbnails.clear_cover_index_failed", nil, err.Error())
			return
		}
		c.updateTaskDetailsMsg("rebuild_thumbnails", 0, -1, "task.msg.rebuild_thumbnails.rebuilding_low_impact", nil, "reading_metadata", "", nil, nil)
		err := c.runGlobalScan(taskCtx, true, func(current, total int, lib database.Library) {
			c.trackRebuildThumbLibraryProgress(current, total, lib)
			c.refreshRebuildThumbTaskFromAggregator(lib)
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg("rebuild_thumbnails", "cancelled", "task.msg.rebuild_thumbnails.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg("rebuild_thumbnails", "task.msg.rebuild_thumbnails.failed", nil, err.Error())
			return
		}
		c.refreshRebuildThumbTaskMessage("task.msg.rebuild_thumbnails.waiting_cover_queue", nil, "queueing_covers")
		if err := c.scanner.WaitForCoverQueue(taskCtx); errors.Is(err, context.Canceled) {
			c.completeTaskMsg("rebuild_thumbnails", "cancelled", "task.msg.rebuild_thumbnails.cancelled", nil)
			return
		} else if err != nil {
			c.failTaskErrMsg("rebuild_thumbnails", "task.msg.rebuild_thumbnails.wait_queue_failed", nil, err.Error())
			return
		}
		c.finishTaskMsg("rebuild_thumbnails", "task.msg.rebuild_thumbnails.complete", nil)
		c.warmDashboardStatsCacheAsync("rebuild_thumbnails_completed")
	})
	c.PublishEvent("refresh_thumbnails")
	return nil
}

func (c *Controller) rebuildThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildThumbnailsTask(); err != nil {
		jsonResponse(w, http.StatusConflict, map[string]string{"error": "A thumbnail rebuild is already running"})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.thumbnails_rebuilding")})
}

func (c *Controller) launchCleanupThumbnailsTask() error {
	if !c.startPausableCancelableTaskMsg("cleanup_thumbnails", "cleanup_thumbnails", "task.msg.cleanup_thumbnails.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	taskCtx, cleanupCancel := c.newTaskContext("cleanup_thumbnails")
	c.setTaskMetadata("cleanup_thumbnails", nil, "")

	// 这里曾写成 `go c.runBackground(...)`：多套一层 goroutine 会让 runBackground 的
	// closed 检查与 backgroundWG.Add 都发生在另一个调度点上，停机竞态下任务会被静默丢弃，
	// 而 newTaskContext 已登记的 runtime 记录留在内存里泄漏。与其余 34 个调用点保持一致。
	c.runBackground(func() {
		defer cleanupCancel()

		c.updateTaskDetailsMsg("cleanup_thumbnails", 0, -1, "task.msg.cleanup_thumbnails.scanning", nil, "cleanup", "", nil, nil)

		err := c.scanner.CleanupThumbnails(taskCtx, func(deleted, scanned int, msg string) {
			c.updateTaskDetails("cleanup_thumbnails", deleted, scanned, msg, "cleanup", "", nil, nil)
		})

		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg("cleanup_thumbnails", "cancelled", "task.msg.cleanup_thumbnails.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg("cleanup_thumbnails", "task.msg.cleanup_thumbnails.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg("cleanup_thumbnails", "task.msg.cleanup_thumbnails.complete", nil)
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
	if !c.startPausableCancelableTaskMsg("rebuild_file_identities", "rebuild_file_identities", "task.msg.rebuild_file_identities.start", nil, 0) {
		return errTaskAlreadyRunning
	}
	c.setTaskMetadata("rebuild_file_identities", map[string]string{"profile": "quick_hash"}, "")
	c.setTaskEffectiveLimit("rebuild_file_identities", c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext("rebuild_file_identities")

	c.runBackground(func() {
		defer cleanupCancel()
		updated, total, err := c.runRebuildFileIdentities(taskCtx, 500, func(current, total int, _ string, metrics taskIOMetrics) {
			c.updateTaskDetailsMsg("rebuild_file_identities", current, total, "task.msg.rebuild_file_identities.progress", map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)}, "hashing", "", map[string]int64{
				"hashed_files": metrics.HashedFiles,
				"io_wait_ms":   metrics.IOWaitMillis,
				"paused_ms":    metrics.PausedMillis,
			}, map[string]string{
				"storage_profile": metrics.StorageProfile,
				"volume_key":      metrics.VolumeKey,
			})
			c.mergeTaskParams("rebuild_file_identities", taskIOMetricsParams(metrics))
		})
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg("rebuild_file_identities", "cancelled", "task.msg.rebuild_file_identities.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg("rebuild_file_identities", "task.msg.rebuild_file_identities.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg("rebuild_file_identities", "task.msg.rebuild_file_identities.complete", map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)})
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

	if !c.startPausableCancelableTaskMsg(lowPriorityBookHashTaskKey, "rebuild_book_hashes", "task.msg.book_hash_backfill.start", nil, int(missingCount)) {
		return false
	}
	c.setTaskMetadata(lowPriorityBookHashTaskKey, map[string]string{
		"match_mode": config.KOReaderMatchModeBinaryHash,
		"profile":    "full_hash_low_priority",
		"reason":     reason,
	}, "")
	c.setTaskEffectiveLimit(lowPriorityBookHashTaskKey, c.taskLimitsForPath("", true))
	taskCtx, cleanupCancel := c.newTaskContext(lowPriorityBookHashTaskKey)

	c.runBackground(func() {
		updated, total, err := c.runBackfillFullHashesLowPriority(taskCtx, lowPriorityBookHashBatchSize, lowPriorityBookHashBatchGap, func(current, total int, _ string, metrics taskIOMetrics) {
			c.updateTaskDetailsMsg(lowPriorityBookHashTaskKey, current, total, "task.msg.book_hash_backfill.progress", map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)}, "hashing", "", map[string]int64{
				"hashed_files": metrics.HashedFiles,
				"io_wait_ms":   metrics.IOWaitMillis,
				"paused_ms":    metrics.PausedMillis,
			}, map[string]string{
				"storage_profile": metrics.StorageProfile,
				"volume_key":      metrics.VolumeKey,
			})
			c.mergeTaskParams(lowPriorityBookHashTaskKey, taskIOMetricsParams(metrics))
		})
		cleanupCancel()
		if errors.Is(err, context.Canceled) {
			c.completeTaskMsg(lowPriorityBookHashTaskKey, "cancelled", "task.msg.book_hash_backfill.cancelled", nil)
			return
		}
		if err != nil {
			c.failTaskErrMsg(lowPriorityBookHashTaskKey, "task.msg.book_hash_backfill.failed", nil, err.Error())
			return
		}
		c.finishTaskMsg(lowPriorityBookHashTaskKey, "task.msg.book_hash_backfill.complete", map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)})
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
			fileHash, err := koreader.FingerprintFile(book.Path)
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
