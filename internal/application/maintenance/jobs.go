package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/koreader"
	"manga-manager/internal/runhandle"
	"manga-manager/internal/storageio"
	"manga-manager/internal/task"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	rebuildBookHashesTaskKey     = "rebuild_book_hashes"
	lowPriorityBookHashTaskKey   = "background_book_hash_backfill"
	lowPriorityBookHashBatchSize = 32
	lowPriorityBookHashBatchGap  = 100 * time.Millisecond
)

func systemIdentity(t string, v task.Variant) task.Identity {
	return task.Identity{Type: task.Type(t), Scope: task.ScopeSystem, Variant: v}
}
func (c *Service) run(id task.Identity, trigger task.Trigger, spec task.RunSpec, body task.Body) error {
	spec.Identity = id
	spec.Trigger = trigger
	spec.LibraryIDs = []int64{task.AllLibraries}
	_, err := c.runs.Start(context.Background(), spec, body)
	return err
}
func (c *Service) rebuildThumbnailLibraries(ctx context.Context, h *runhandle.Handle) error {
	libs, err := c.store.ListLibraries(ctx)
	if err != nil {
		return err
	}
	return c.scans.RebuildThumbnails(ctx, h, libs, c.thumbnails.BeginLibrary)
}
func (c *Service) clearThumbnailDir(thumbDir string) error {
	keep := make(map[string]bool)
	for _, dir := range config.NonThumbnailDirs(c.config()) {
		keep[dir] = true
	}

	entries, err := os.ReadDir(thumbDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		full := filepath.Join(thumbDir, entry.Name())
		if entry.IsDir() && keep[filepath.Clean(full)] {
			continue
		}
		if err := os.RemoveAll(full); err != nil {
			return err
		}
	}
	return nil
}

func (c *Service) clearAllCoverPaths(ctx context.Context) error {
	if err := c.store.ClearAllBookCoverPaths(ctx); err != nil {
		return fmt.Errorf("clear book cover paths: %w", err)
	}
	if err := c.store.ClearAllSeriesStatsCoverPaths(ctx); err != nil {
		return fmt.Errorf("clear series cover paths: %w", err)
	}
	return nil
}

// RebuildThumbnails clears generated cover state, scans all libraries, and queues each library cover run.
func (c *Service) RebuildThumbnails(trigger task.Trigger) error {
	cfg := c.config()
	thumbDir := config.ThumbnailDir(cfg)

	spec := task.RunSpec{
		Key:       "rebuild_thumbnails",
		StartCode: "task.msg.rebuild_thumbnails.start",
		CanCancel: true,
		CanPause:  true,
		Args: map[string]string{
			"execution_mode": "low_impact",
		},
		CompleteCode: "task.msg.rebuild_thumbnails.complete",
		CancelCode:   "task.msg.rebuild_thumbnails.cancelled",
		FailCode:     "task.msg.rebuild_thumbnails.failed",
	}

	if err := c.run(systemIdentity("rebuild_thumbnails", ""), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		c.thumbnails.Begin(handle, 0)
		defer c.thumbnails.End()

		handle.Report(runhandle.Frame{Phase: "clearing_cache", Item: thumbDir, Code: "task.msg.rebuild_thumbnails.clearing_cache"})
		if err := c.clearThumbnailDir(thumbDir); err != nil {
			return failure("task.msg.rebuild_thumbnails.clear_cache_failed", err), err
		}
		if err := handle.Checkpoint(ctx); err != nil {
			return task.Result{}, err
		}
		if err := os.MkdirAll(thumbDir, 0o755); err != nil {
			return failure("task.msg.rebuild_thumbnails.mkdir_failed", err), err
		}
		handle.Phase("clearing_cache", "task.msg.rebuild_thumbnails.clearing_cover_index", nil)
		if err := c.clearAllCoverPaths(ctx); err != nil {
			return failure("task.msg.rebuild_thumbnails.clear_cover_index_failed", err), err
		}
		handle.Phase("reading_metadata", "task.msg.rebuild_thumbnails.rebuilding_low_impact", nil)
		if err := c.rebuildThumbnailLibraries(ctx, handle); err != nil {
			return task.Result{}, err
		}
		c.effects.WarmStats("rebuild_thumbnails_completed")
		return task.Result{}, nil
	}); err != nil {
		return err
	}
	c.effects.Publish("refresh_thumbnails")
	return nil
}

// CleanupThumbnails starts removal of generated thumbnails without retained references.
func (c *Service) CleanupThumbnails(trigger task.Trigger) error {
	spec := task.RunSpec{
		Key:          "cleanup_thumbnails",
		StartCode:    "task.msg.cleanup_thumbnails.start",
		CanCancel:    true,
		CanPause:     true,
		CompleteCode: "task.msg.cleanup_thumbnails.complete",
		CancelCode:   "task.msg.cleanup_thumbnails.cancelled",
		FailCode:     "task.msg.cleanup_thumbnails.failed",
	}

	return c.run(systemIdentity("cleanup_thumbnails", ""), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		// 开工这一帧只播**阶段**：此时一个文件都还没数过，报计数只能编一个凑数的值。
		handle.Phase("cleanup", "task.msg.cleanup_thumbnails.scanning", nil)
		err := c.scans.CleanupThumbnails(ctx, func(deleted, scanned int) {
			handle.Advance(deleted, scanned, "task.msg.cleanup_thumbnails.progress", map[string]string{
				"deleted": strconv.Itoa(deleted),
				"scanned": strconv.Itoa(scanned),
			})
		})
		return task.Result{}, err
	})
}

// RebuildFileIdentities starts repair of missing quick identities with pause and disk admission.
func (c *Service) RebuildFileIdentities(trigger task.Trigger) error {
	spec := task.RunSpec{
		Key:          "rebuild_file_identities",
		StartCode:    "task.msg.rebuild_file_identities.start",
		CanCancel:    true,
		CanPause:     true,
		Args:         map[string]string{"profile": "quick_hash"},
		CompleteCode: "task.msg.rebuild_file_identities.complete",
		CancelCode:   "task.msg.rebuild_file_identities.cancelled",
		FailCode:     "task.msg.rebuild_file_identities.failed",
	}

	return c.run(systemIdentity("rebuild_file_identities", ""), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		updated, total, err := c.RebuildFileIdentityRecords(ctx, 500,
			hashingFrameHandle{Handle: handle, code: "task.msg.rebuild_file_identities.progress"})
		if err != nil {
			return task.Result{}, err
		}
		return task.Result{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// RebuildFileIdentityRecords repairs missing quick identities in bounded database batches.
func (c *Service) RebuildFileIdentityRecords(ctx context.Context, limit int, task koreader.TaskHandle) (int, int, error) {
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
				slog.WarnContext(ctx, "Failed to quick-fingerprint book", "book_id", book.ID, "path", book.Path, "error", hashErr)
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

// BackfillBookHashes starts binary identity repair only when the enabled matching mode needs it.
func (c *Service) BackfillBookHashes(reason string, trigger task.Trigger) error {
	cfg := c.config()
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

	spec := task.RunSpec{
		Key:       lowPriorityBookHashTaskKey,
		StartCode: "task.msg.book_hash_backfill.start",
		Total:     int(missingCount),
		CanCancel: true,
		CanPause:  true,
		Args: map[string]string{
			"match_mode": config.KOReaderMatchModeBinaryHash,
			"profile":    "full_hash_low_priority",
			"reason":     reason,
		},
		CompleteCode: "task.msg.book_hash_backfill.complete",
		CancelCode:   "task.msg.book_hash_backfill.cancelled",
		FailCode:     "task.msg.book_hash_backfill.failed",
	}

	return c.run(systemIdentity("rebuild_book_hashes", "low_priority_backfill"), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		updated, total, err := c.BackfillFullHashRecords(ctx, lowPriorityBookHashBatchSize, lowPriorityBookHashBatchGap,
			hashingFrameHandle{Handle: handle, code: "task.msg.book_hash_backfill.progress"})
		if err != nil {
			return task.Result{}, err
		}
		return task.Result{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// ChainBookHashBackfill schedules the scan follow-up and keeps launch warnings on the parent run context.
func (c *Service) ChainBookHashBackfill(ctx context.Context, reason string) {
	err := c.BackfillBookHashes(reason, task.TriggerChained)
	if err != nil && !errors.Is(err, task.ErrRunAlreadyActive) {
		slog.WarnContext(ctx, "Background book hash backfill not started", "reason", reason, "error", err)
	}
}

// BackfillFullHashRecords repairs binary identities with bounded batches and cancellable gaps.
func (c *Service) BackfillFullHashRecords(ctx context.Context, limit int, batchGap time.Duration, task koreader.TaskHandle) (int, int, error) {
	if limit <= 0 {
		limit = lowPriorityBookHashBatchSize
	}
	return c.koreader.RebuildBookIdentities(ctx, koreader.RebuildOptions{
		BatchSize: limit,
		BatchGap:  batchGap,
		MatchMode: config.KOReaderMatchModeBinaryHash,
	}, task)
}

// ReportHashProgress publishes identity counts and IO metrics together, retaining replay parameters separately.
func ReportHashProgress(handle *runhandle.Handle, current, total int, code string, handleIO runhandle.IOMetrics) {
	handle.Report(runhandle.Frame{
		Current: &current,
		Total:   &total,
		Phase:   "hashing",
		Code:    code,
		Params:  map[string]string{"current": strconv.Itoa(current), "total": strconv.Itoa(total)},
		Metrics: taskIOFrameMetrics(handleIO),
		Labels: map[string]string{
			"storage_profile": handleIO.StorageProfile,
			"volume_key":      handleIO.VolumeKey,
		},
	})
	handle.MergeParams(taskIOMetricsParams(handleIO))
}

// hashingFrameHandle 是两处哈希回填的批循环共同收下的**运行句柄**，分工同
// koreaderFingerprintHandle：只有**计数推进**换成 ReportHashProgress 那一帧，
// 其余能力由内嵌的句柄原样白拿。
type hashingFrameHandle struct {
	*runhandle.Handle
	// code 是这个任务的文案码。两处的帧除它之外同形，因此它是构造期唯一要填的差异。
	code string
}

func (h hashingFrameHandle) Advance(current, total int) {
	ReportHashProgress(h.Handle, current, total, h.code, h.IOMetrics())
}

const koreaderTaskBatchSize = 500

// koreaderMatchMetadata 是三个 KOReader 任务共同的**任务参数**：任务中心按 match_mode 与
// path_ignore_extension 渲染「路径索引 / 二进制哈希索引」那块标签（前端 formatKOReaderIndexLabel）。
// 三处必须写同一份，缺一个键界面就回落到默认标签，用户看到的索引类型是错的。
func koreaderMatchMetadata(cfg config.Config) map[string]string {
	return map[string]string{
		"match_mode":            cfg.KOReader.MatchMode,
		"path_ignore_extension": strconv.FormatBool(cfg.KOReader.PathIgnoreExtension),
	}
}

// koreaderFingerprintFrame 把一次**指纹**重建进度翻成**一帧**：**计数推进**、**阶段**、文案与
// 指标同属一次事件，拆开报会被投递水位撕断（撕开的样子见 runhandle.Handle.Report）。
// 书籍指纹重建与匹配刷新的第一阶段共用它，帧的内容因此只有一份。
//
// 档位与卷键不进帧的**标签**，只走**任务参数**那条通道（`taskIOMetricsParams` 会把空值滤掉）：
// **路径**匹配模式下这个任务一次盘都不读，两项恒为空，而标签是有一个显示一个——写进去等于
// 把「没有这回事」显示成「实况为空」。IO 那几项指标同样恒为零，但面板只显示大于零的指标。
func koreaderFingerprintFrame(current, total int, handleIO runhandle.IOMetrics) runhandle.Frame {
	frameMetrics := taskIOFrameMetrics(handleIO)
	frameMetrics["processed_books"] = int64(current)
	return runhandle.Frame{
		Current: &current,
		Total:   &total,
		Phase:   "hashing",
		Code:    "task.msg.koreader_rebuild_hashes.progress",
		Params:  map[string]string{"updated": strconv.Itoa(current), "total": strconv.Itoa(total)},
		Metrics: frameMetrics,
	}
}

// koreaderReconcileFrame 把一次进度对账翻成一帧，整帧报出的理由同 koreaderFingerprintFrame。
// 进度对账与匹配刷新的第二阶段共用它。
func koreaderReconcileFrame(current, total int) runhandle.Frame {
	return runhandle.Frame{
		Current: &current,
		Total:   &total,
		Phase:   "reconciling_progress",
		Code:    "task.msg.reconcile_koreader_progress.progress",
		Params:  map[string]string{"processed": strconv.Itoa(current), "total": strconv.Itoa(total)},
		Metrics: map[string]int64{"processed_progress": int64(current)},
	}
}

// holdingStepCount 摘掉一帧的计数推进，把进度条留在原处，其余照报。
//
// 只有匹配刷新用它：那个任务的进度条数的是它自己的两个阶段（0/2 → 1/2），而两个阶段内部复用的
// 正是上面两个帧——原样报进去，用户会看到「40 / 共 2」这种读数。逐条目计数在那里只进占位参数
// 与指标，两处都还在帧里。
func holdingStepCount(frame runhandle.Frame) runhandle.Frame {
	frame.Current, frame.Total = nil, nil
	return frame
}

// koreaderFingerprintHandle 是**指纹**重建批循环收下的**运行句柄**：过**暂停闸门**与发起
// **磁盘作业**由内嵌的句柄原样白拿，只有**计数推进**换成本包的帧构造——批循环报两个数字，
// i18n 码与**阶段**留在这一侧（遮蔽内嵌方法的写法同 proposalDB.ExecTx）。
type koreaderFingerprintHandle struct {
	*runhandle.Handle
	// holdStepCount 为真时摘掉这一帧的计数推进，理由见 holdingStepCount。
	holdStepCount bool
}

// Advance 遮蔽内嵌句柄的同名方法：批循环只认收窄后的这个形状。
// 读 IO 实况与批循环写它同在任务体这一条 goroutine 上，而句柄本身已整体加锁。
func (h koreaderFingerprintHandle) Advance(current, total int) {
	handleIO := h.IOMetrics()
	frame := koreaderFingerprintFrame(current, total, handleIO)
	if h.holdStepCount {
		frame = holdingStepCount(frame)
	}
	h.Report(frame)
	// IO 参数走的是另一条通道（存储 IO 面板按参数名读），只能单独报一次。
	h.MergeParams(taskIOMetricsParams(handleIO))
}

// koreaderReconcileHandle 是进度对账批循环收下的运行句柄，分工同 koreaderFingerprintHandle。
// 对账一次盘都不读，因此它的帧里没有 IO 实况。
type koreaderReconcileHandle struct {
	*runhandle.Handle
	// holdStepCount 为真时摘掉这一帧的计数推进，理由见 holdingStepCount。
	holdStepCount bool
}

func (h koreaderReconcileHandle) Advance(current, total int) {
	frame := koreaderReconcileFrame(current, total)
	if h.holdStepCount {
		frame = holdingStepCount(frame)
	}
	h.Report(frame)
}

// RebuildBookHashes 是书籍**指纹**重建任务的启动点，走引擎的启动入口。
func (c *Service) RebuildBookHashes(trigger task.Trigger) error {
	return c.rebuildBookHashes(trigger, c.config())
}
func (c *Service) rebuildBookHashes(trigger task.Trigger, cfg config.Config) error {
	spec := task.RunSpec{
		Key:          rebuildBookHashesTaskKey,
		StartCode:    "task.msg.koreader_rebuild_hashes.start",
		CanCancel:    true,
		CanPause:     true,
		Args:         koreaderMatchMetadata(cfg),
		CompleteCode: "task.msg.koreader_rebuild_hashes.complete",
		CancelCode:   "task.msg.koreader_rebuild_hashes.cancelled",
		FailCode:     "task.msg.koreader_rebuild_hashes.failed",
	}

	return c.run(systemIdentity("rebuild_book_hashes", "foreground"), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		opts := koreader.RebuildOptions{BatchSize: koreaderTaskBatchSize, MatchMode: cfg.KOReader.MatchMode}
		updated, total, err := c.koreader.RebuildBookIdentities(ctx, opts, koreaderFingerprintHandle{Handle: handle})
		if err != nil {
			return task.Result{}, err
		}
		return task.Result{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// ReconcileKOReaderProgress 是 KOReader 进度对账任务的启动点，走引擎的启动入口。
//
// 它只重算已落库记录的归属，不读书文件，没有哪个并发上限管得住它，因此 Limits 留零值
// （零值的语义见 RunSpec.Limits）。
func (c *Service) ReconcileKOReaderProgress(trigger task.Trigger) error {
	return c.reconcileKOReaderProgress(trigger, c.config())
}
func (c *Service) reconcileKOReaderProgress(trigger task.Trigger, cfg config.Config) error {
	spec := task.RunSpec{
		Key:          "reconcile_koreader_progress",
		StartCode:    "task.msg.reconcile_koreader_progress.start",
		CanCancel:    true,
		CanPause:     true,
		Args:         koreaderMatchMetadata(cfg),
		CompleteCode: "task.msg.reconcile_koreader_progress.complete",
		CancelCode:   "task.msg.reconcile_koreader_progress.cancelled",
		FailCode:     "task.msg.reconcile_koreader_progress.failed",
	}

	return c.run(systemIdentity("reconcile_koreader_progress", ""), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		updated, total, err := c.koreader.ReconcileProgressWithMatching(ctx, koreaderTaskBatchSize, koreaderReconcileHandle{Handle: handle}, koreader.MatchConfig{MatchMode: cfg.KOReader.MatchMode, PathIgnoreExtension: cfg.KOReader.PathIgnoreExtension})
		if err != nil {
			return task.Result{}, err
		}
		return task.Result{Params: map[string]string{"updated": strconv.Itoa(updated), "total": strconv.Itoa(total)}}, nil
	})
}

// RefreshKOReaderMatching 是匹配刷新任务的启动点，走引擎的启动入口。
//
// 它是**两阶段**任务：先重建**指纹**，再对账进度。进度条数的因此是阶段而不是条目——总数在
// 任务声明里就是 2，两个阶段的逐条目回调一次都不动它，逐条目计数只进占位参数与指标。
//
// 两步各有专属失败文案码，取消则无论停在哪一步都落同一条取消码：taskFailure 把取消挡在专属码
// 之外，否则用户按下取消看到的会是「索引重建失败」。
//
// 任务声明里那条默认失败码今天走不到（两步的失败各被覆盖、取消走取消码）。留着是因为
// FailCode 留空会让将来任何一条未覆盖的失败路径把**起始**文案原样渲染成失败态的文案。
func (c *Service) RefreshKOReaderMatching(trigger task.Trigger) error {
	return c.refreshKOReaderMatching(trigger, c.config())
}
func (c *Service) refreshKOReaderMatching(trigger task.Trigger, cfg config.Config) error {
	spec := task.RunSpec{
		Key:          "refresh_koreader_matching",
		StartCode:    "task.msg.refresh_koreader_matching.start",
		Total:        2,
		CanCancel:    true,
		CanPause:     true,
		Args:         koreaderMatchMetadata(cfg),
		CompleteCode: "task.msg.refresh_koreader_matching.complete",
		CancelCode:   "task.msg.refresh_koreader_matching.cancelled",
		FailCode:     "task.msg.refresh_koreader_matching.failed",
	}

	return c.run(systemIdentity("refresh_koreader_matching", ""), trigger, spec, func(ctx context.Context, handle *runhandle.Handle) (task.Result, error) {
		handle.Phase("hashing", "task.msg.refresh_koreader_matching.rebuild_start", nil)
		opts := koreader.RebuildOptions{BatchSize: koreaderTaskBatchSize, MatchMode: cfg.KOReader.MatchMode}
		updatedBooks, totalBooks, err := c.koreader.RebuildBookIdentities(ctx, opts,
			koreaderFingerprintHandle{Handle: handle, holdStepCount: true})
		if err != nil {
			return failure("task.msg.refresh_koreader_matching.rebuild_failed", err), err
		}

		// 阶段跃迁与阶段计数是同一件事，必须一帧报出：分成两次的话，先出去的那条载荷带着
		// 新计数与旧阶段名，而补齐的那条会被水位吞掉（阶段与文案码此时已经一字未变）。
		reconcileStep := 1
		handle.Report(runhandle.Frame{
			Current: &reconcileStep,
			Phase:   "reconciling_progress",
			Code:    "task.msg.refresh_koreader_matching.reconcile_start",
			Params:  map[string]string{"updated": strconv.Itoa(updatedBooks), "total": strconv.Itoa(totalBooks)},
		})
		updatedProgress, totalProgress, err := c.koreader.ReconcileProgressWithMatching(ctx, koreaderTaskBatchSize,
			koreaderReconcileHandle{Handle: handle, holdStepCount: true}, koreader.MatchConfig{MatchMode: cfg.KOReader.MatchMode, PathIgnoreExtension: cfg.KOReader.PathIgnoreExtension})
		if err != nil {
			return failure("task.msg.refresh_koreader_matching.reconcile_failed", err), err
		}

		return task.Result{Params: map[string]string{
			"updatedBooks":    strconv.Itoa(updatedBooks),
			"totalBooks":      strconv.Itoa(totalBooks),
			"updatedProgress": strconv.Itoa(updatedProgress),
			"totalProgress":   strconv.Itoa(totalProgress),
		}}, nil
	})
}

func taskIOFrameMetrics(handleIO runhandle.IOMetrics) map[string]int64 {
	return map[string]int64{
		"hashed_files": handleIO.HashedFiles,
		"io_wait_ms":   handleIO.IOWaitMillis,
		"paused_ms":    handleIO.PausedMillis,
	}
}
func taskIOMetricsParams(handleIO runhandle.IOMetrics) map[string]string {
	params := map[string]string{
		"io_wait_ms":   strconv.FormatInt(handleIO.IOWaitMillis, 10),
		"paused_ms":    strconv.FormatInt(handleIO.PausedMillis, 10),
		"hashed_files": strconv.FormatInt(handleIO.HashedFiles, 10),
	}
	if handleIO.StorageProfile != "" {
		params["storage_profile"] = handleIO.StorageProfile
	}
	if handleIO.VolumeKey != "" {
		params["volume_key"] = handleIO.VolumeKey
	}
	return params
}
