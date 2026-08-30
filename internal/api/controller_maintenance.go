// 本文件由 controller.go 拆分而来，属于后端 API 层的维护任务子域，负责全库扫描、索引重建、缩略图重建/清理、文件指纹重建与低优先级全量哈希回填等运维任务的编排与接口。

package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/koreader"
	"manga-manager/internal/scanner"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskrun"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
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
			if err := c.scanner.ScanLibrary(ctx, lib.ID, lib.Path, true, nil); err != nil {
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
// observerFor 为每个资料库要一个**扫描观察者**；返回 nil 即该库这次扫描不属于任何任务。
// 调用它本身就是库切换的边界——没有第二条「现在换库了」的通知，两者一旦分家就会有
// 「观察者已经在收报文、界面上还是上一个库名」的窗口。
// tp 是发起这次扫描的任务的**任务句柄**，库与库之间的可中断点经它过**暂停闸门**；不得为 nil。
func (c *Controller) runGlobalScan(ctx context.Context, tp *taskrun.Handle, force bool, ignoreFormatFilter bool, observerFor func(lib database.Library, total int) scanner.ScanObserver) error {
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	total := len(libs)
	for _, lib := range libs {
		if err := tp.Checkpoint(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var observer scanner.ScanObserver
		if observerFor != nil {
			observer = observerFor(lib, total)
		}
		if err := c.scanner.ScanLibraryWithOptions(ctx, lib.ID, lib.Path, scanner.LibraryScanOptions{
			Force:              force,
			IgnoreFormatFilter: ignoreFormatFilter,
		}, observer); err != nil {
			return err
		}
		c.purgeReadingPathCaches()
	}
	return nil
}

// launchRebuildIndexTask 是索引重建任务的启动点，走引擎的启动入口：重灌两份 FTS 索引，
// 随后同步等一次全量扫描（等待的理由见 triggerGlobalScan）。
//
// 两步重灌各有专属失败文案码：技术错误串两步长得一样，只报一句「重建索引失败」的话，
// 用户不知道该去查系列索引还是书籍索引。
func (c *Controller) launchRebuildIndexTask() error {
	spec := TaskSpec{
		Key:          "rebuild_index",
		Type:         "rebuild_index",
		StartCode:    "task.msg.rebuild_index.start",
		Total:        1,
		CompleteCode: "task.msg.rebuild_index.complete",
		CancelCode:   "task.msg.rebuild_index.cancelled",
		FailCode:     "task.msg.rebuild_index.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, _ *taskrun.Handle) (TaskResult, error) {
		if err := c.store.RebuildSeriesSearchIndex(ctx); err != nil {
			return taskFailure("task.msg.rebuild_index.series_failed", err), err
		}
		if err := c.store.RebuildBookSearchIndex(ctx); err != nil {
			return taskFailure("task.msg.rebuild_index.book_failed", err), err
		}
		if err := ctx.Err(); err != nil {
			return TaskResult{}, err
		}
		c.triggerGlobalScan(ctx)
		return TaskResult{}, nil
	})
}

func (c *Controller) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildIndexTask(); err != nil {
		writeTaskLaunchError(w, err, "A search index rebuild is already running", "Failed to rebuild search index")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.search_index_rebuilt")})
}

// launchRebuildThumbnailsTask 是缩略图重建任务的启动点，走引擎的启动入口。
//
// 任务体开工第一件事是把任务句柄交给 rebuildThumbAggregator：这个任务的进度由任务体
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

	if err := c.taskEngine.Run(spec, func(ctx context.Context, tp *taskrun.Handle) (TaskResult, error) {
		c.initRebuildThumbAggregator(tp, 0)
		defer c.releaseRebuildThumbAggregator()

		tp.Report(taskrun.Frame{Phase: "clearing_cache", Item: thumbDir, Code: "task.msg.rebuild_thumbnails.clearing_cache"})
		if err := c.clearThumbnailDir(thumbDir); err != nil {
			return taskFailure("task.msg.rebuild_thumbnails.clear_cache_failed", err), err
		}
		if err := tp.Checkpoint(ctx); err != nil {
			return TaskResult{}, err
		}
		if err := os.MkdirAll(thumbDir, 0o755); err != nil {
			return taskFailure("task.msg.rebuild_thumbnails.mkdir_failed", err), err
		}
		tp.Phase("clearing_cache", "task.msg.rebuild_thumbnails.clearing_cover_index", nil)
		if err := c.clearAllCoverPaths(ctx); err != nil {
			return taskFailure("task.msg.rebuild_thumbnails.clear_cover_index_failed", err), err
		}
		tp.Phase("reading_metadata", "task.msg.rebuild_thumbnails.rebuilding_low_impact", nil)
		if err := c.runGlobalScan(ctx, tp, true, true, /* 重建缩略图必须看得见全部已入库的书 */
			c.beginRebuildThumbLibrary); err != nil {
			return TaskResult{}, err
		}
		c.refreshRebuildThumbTaskMessage("task.msg.rebuild_thumbnails.waiting_cover_queue", nil, "queueing_covers")
		if err := c.scanner.WaitForCoverQueue(ctx); err != nil {
			return taskFailure("task.msg.rebuild_thumbnails.wait_queue_failed", err), err
		}
		c.warmDashboardStatsCacheAsync("rebuild_thumbnails_completed")
		return TaskResult{}, nil
	}); err != nil {
		return err
	}
	c.PublishEvent("refresh_thumbnails")
	return nil
}

func (c *Controller) rebuildThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildThumbnailsTask(); err != nil {
		writeTaskLaunchError(w, err, "A thumbnail rebuild is already running", "Failed to start thumbnail rebuild")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.thumbnails_rebuilding")})
}

// launchCleanupThumbnailsTask 是缩略图清理任务的启动点，走引擎的启动入口。
func (c *Controller) launchCleanupThumbnailsTask() error {
	spec := TaskSpec{
		Key:          "cleanup_thumbnails",
		Type:         "cleanup_thumbnails",
		StartCode:    "task.msg.cleanup_thumbnails.start",
		CanCancel:    true,
		CanPause:     true,
		CompleteCode: "task.msg.cleanup_thumbnails.complete",
		CancelCode:   "task.msg.cleanup_thumbnails.cancelled",
		FailCode:     "task.msg.cleanup_thumbnails.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *taskrun.Handle) (TaskResult, error) {
		// 开工这一帧只播**阶段**：此时一个文件都还没数过，报计数只能编一个凑数的值。
		tp.Phase("cleanup", "task.msg.cleanup_thumbnails.scanning", nil)
		err := c.scanner.CleanupThumbnails(ctx, func(deleted, scanned int) {
			tp.Advance(deleted, scanned, "task.msg.cleanup_thumbnails.progress", map[string]string{
				"deleted": strconv.Itoa(deleted),
				"scanned": strconv.Itoa(scanned),
			})
		})
		return TaskResult{}, err
	})
}

func (c *Controller) cleanupThumbnails(w http.ResponseWriter, r *http.Request) {
	if err := c.launchCleanupThumbnailsTask(); err != nil {
		writeTaskLaunchError(w, err, "A thumbnail cleanup is already running", "Failed to start thumbnail cleanup")
		return
	}
	jsonResponse(w, http.StatusOK, map[string]string{"message": apiText(requestLocale(r), "maintenance.cover_cleanup_started")})
}

// launchRebuildFileIdentitiesTask 是文件身份重建任务的启动点，走引擎的启动入口。
func (c *Controller) launchRebuildFileIdentitiesTask() error {
	spec := TaskSpec{
		Key:          "rebuild_file_identities",
		Type:         "rebuild_file_identities",
		StartCode:    "task.msg.rebuild_file_identities.start",
		CanCancel:    true,
		CanPause:     true,
		Metadata:     map[string]string{"profile": "quick_hash"},
		Limits:       c.taskLimitsForPath("", true),
		CompleteCode: "task.msg.rebuild_file_identities.complete",
		CancelCode:   "task.msg.rebuild_file_identities.cancelled",
		FailCode:     "task.msg.rebuild_file_identities.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *taskrun.Handle) (TaskResult, error) {
		updated, total, err := c.runRebuildFileIdentities(ctx, 500,
			hashingFrameHandle{Handle: tp, code: "task.msg.rebuild_file_identities.progress"})
		if err != nil {
			return TaskResult{}, err
		}
		return TaskResult{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// reportHashProgress 把一次哈希进度上报成**一帧**，外加**任务参数**那一条通道。
//
// 文件身份重建与低优先级哈希回填除文案码外完全同形，共用一份实现，免得两处各自漂移。
// 计数、阶段、指标与标签同属一次事件，必须整帧报出（拆开报会撕成什么样见 taskrun.Handle.Report）。
// IO 参数走的是另一条通道（存储 IO 面板按参数名读，见 taskArchiveOpenRate），只能单独一次。
func reportHashProgress(tp *taskrun.Handle, current, total int, code string, handleIO taskrun.IOMetrics) {
	metrics := taskIOMetricsFrom(handleIO)
	tp.Report(taskrun.Frame{
		Current: &current,
		Total:   &total,
		Phase:   "hashing",
		Code:    code,
		Params:  map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)},
		Metrics: metrics.frameMetrics(),
		Labels: map[string]string{
			"storage_profile": metrics.StorageProfile,
			"volume_key":      metrics.VolumeKey,
		},
	})
	tp.MergeParams(taskIOMetricsParams(handleIO))
}

// hashingFrameHandle 是两处哈希回填的批循环共同收下的**任务句柄**，分工同
// koreaderFingerprintHandle：只有**计数推进**换成 reportHashProgress 那一帧，
// 其余能力由内嵌的句柄原样白拿。
type hashingFrameHandle struct {
	*taskrun.Handle
	// code 是这个任务的文案码。两处的帧除它之外同形，因此它是构造期唯一要填的差异。
	code string
}

func (h hashingFrameHandle) Advance(current, total int) {
	reportHashProgress(h.Handle, current, total, h.code, h.IOMetrics())
}

// runRebuildFileIdentities 逐批补齐缺 quick_hash 的书。它与 RebuildBookIdentities 同形，
// 干活所需的三样资格因此共用那一份窄接口：过**暂停闸门**、发起**磁盘作业**、报**计数推进**。
//
// task 不得为 nil：闸门与磁盘作业都经它，没有兜底的空实现可用。
func (c *Controller) runRebuildFileIdentities(ctx context.Context, limit int, task koreader.TaskHandle) (int, int, error) {
	if limit <= 0 {
		limit = 500
	}
	missingCount, err := c.store.CountBooksMissingQuickHash(ctx)
	if err != nil {
		return 0, 0, err
	}

	total := int(missingCount)
	updated := 0
	var afterID int64
	for {
		if err := task.Checkpoint(ctx); err != nil {
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
			// 最干净的**磁盘作业**形状：取令牌 → 读一遍文件 → 立刻归还。闭包只捕获自己的错误，
			// 因为算不出指纹不该中止整个回填；中止只由句柄返回的闸门与令牌错误决定。
			// 这次作业的实况由句柄自己吸收，「已哈希文件数」也由它按「闭包真的跑了」计数。
			var quickHash string
			var hashErr error
			if err := task.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindIdentityHash, Path: book.Path}, func() error {
				quickHash, hashErr = koreader.FingerprintQuickFile(book.Path)
				return nil
			}); err != nil {
				return updated, total, err
			}
			if hashErr != nil {
				slog.Warn("Failed to quick-fingerprint book", "book_id", book.ID, "path", book.Path, "error", hashErr)
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
			task.Advance(updated, total)
		}
	}
	return updated, total, nil
}

// launchLowPriorityBookHashBackfillTask 是低优先级哈希回填任务的启动点，走引擎的启动入口。
// 它由**资料库扫描**与**系列扫描**的任务体在收尾前发起，是唯一一个由另一个任务的任务体启动的任务。
//
// 返回 nil 表示没有该做而没做的事：任务已发起，或者按前置条件本就不该发起——KOReader 未启用、
// 匹配模式不是二进制哈希、根本没有书缺哈希。被**任务键**闸门挡下时返回 errTaskAlreadyRunning，
// 这在本路径上是常态而非异常：回填跑得久，后一次扫描收尾时撞上它很常见，调用方据此自行取舍。
func (c *Controller) launchLowPriorityBookHashBackfillTask(reason string) error {
	cfg := c.currentConfig()
	if !cfg.KOReader.Enabled || cfg.KOReader.MatchMode != config.KOReaderMatchModeBinaryHash {
		return nil
	}

	missingCount, err := c.store.CountBooksMissingIdentity(context.Background(), config.KOReaderMatchModeBinaryHash)
	if err != nil {
		return fmt.Errorf("count books missing full hash: %w", err)
	}
	if missingCount == 0 {
		return nil
	}

	spec := TaskSpec{
		Key:       lowPriorityBookHashTaskKey,
		Type:      "rebuild_book_hashes",
		StartCode: "task.msg.book_hash_backfill.start",
		Total:     int(missingCount),
		CanCancel: true,
		CanPause:  true,
		Metadata: map[string]string{
			"match_mode": config.KOReaderMatchModeBinaryHash,
			"profile":    "full_hash_low_priority",
			"reason":     reason,
		},
		Limits:       c.taskLimitsForPath("", true),
		CompleteCode: "task.msg.book_hash_backfill.complete",
		CancelCode:   "task.msg.book_hash_backfill.cancelled",
		FailCode:     "task.msg.book_hash_backfill.failed",
	}

	return c.taskEngine.Run(spec, func(ctx context.Context, tp *taskrun.Handle) (TaskResult, error) {
		updated, total, err := c.runBackfillFullHashesLowPriority(ctx, lowPriorityBookHashBatchSize, lowPriorityBookHashBatchGap,
			hashingFrameHandle{Handle: tp, code: "task.msg.book_hash_backfill.progress"})
		if err != nil {
			return TaskResult{}, err
		}
		return TaskResult{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// chainBookHashBackfill 是扫描任务体收尾时串联哈希回填的那一下：尽力而为，起不来不影响这次扫描。
//
// 「同类任务已在运行」在这里不记日志——回填跑得久，连着扫两个资料库时后一次必然撞上它，
// 记下来只是噪音。其余错误要记：数不清缺口是真出了问题，而调用方是任务体，没有别的地方能报。
func (c *Controller) chainBookHashBackfill(reason string) {
	err := c.launchLowPriorityBookHashBackfillTask(reason)
	if err != nil && !errors.Is(err, errTaskAlreadyRunning) {
		slog.Warn("Background book hash backfill not started", "reason", reason, "error", err)
	}
}

// runBackfillFullHashesLowPriority 走的就是 KOReader 那套**指纹**重建，只是把节奏压低：小批次、
// 批间停顿，好让它在扫描收尾后长时间跑着也不与别的活抢盘。
//
// 匹配模式钉死在二进制哈希，不跟着配置走：这个任务由**资料库扫描**收尾时按「缺全量哈希的书有
// 几本」发起，声明里报的也是这一档，跑到一半配置被改不该让它改口径去补路径指纹。
func (c *Controller) runBackfillFullHashesLowPriority(ctx context.Context, limit int, batchGap time.Duration, task koreader.TaskHandle) (int, int, error) {
	if limit <= 0 {
		limit = lowPriorityBookHashBatchSize
	}
	return c.koreader.RebuildBookIdentities(ctx, koreader.RebuildOptions{
		BatchSize: limit,
		BatchGap:  batchGap,
		MatchMode: config.KOReaderMatchModeBinaryHash,
	}, task)
}

func (c *Controller) rebuildFileIdentities(w http.ResponseWriter, r *http.Request) {
	if err := c.launchRebuildFileIdentitiesTask(); err != nil {
		writeTaskLaunchError(w, err, "A file identity rebuild is already running", "Failed to start file identity rebuild")
		return
	}
	jsonResponse(w, http.StatusAccepted, map[string]string{"message": apiText(requestLocale(r), "maintenance.file_identity_rebuild_started")})
}
