// 业务说明：本文件是业务实现，属于漫画库扫描链路，负责发现文件、建立书籍和系列记录、提取封面、同步索引并维护任务进度。
// 它决定本地文件系统如何变成前端资料库、搜索结果和系列聚合视图。
// 维护时应重点关注增量扫描、重命名/删除处理、元数据回填、SQLite FTS5 搜索索引同步和长任务取消。

package scanner

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"manga-manager/internal/booksort"
	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/images"
	"manga-manager/internal/koreader"
	"manga-manager/internal/parser"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

type Scanner struct {
	store       database.Store
	config      *config.Manager
	openArchive func(string) (parser.Archive, error)
	coverOnce   sync.Once
	coverQueue  chan coverJob
	coverWG     sync.WaitGroup
	mu          sync.Mutex
	active      struct {
		libraries map[int64]struct{}
		series    map[int64]struct{}
	}
	// 批量插入结束后的回调播送机制
	onBatchIngested func(action string)
	onScanMetrics   func(ScanMetricsReport)
	onScanProgress  func(ScanProgressReport)
}

func NewScanner(store database.Store, cfg *config.Manager) *Scanner {
	s := &Scanner{
		store:       store,
		config:      cfg,
		openArchive: parser.OpenArchive,
	}
	s.active.libraries = make(map[int64]struct{})
	s.active.series = make(map[int64]struct{})
	return s
}

// SetBatchCallback 允许外部注册事件通知钩子
func (s *Scanner) SetBatchCallback(cb func(string)) {
	s.onBatchIngested = cb
}

func (s *Scanner) SetScanMetricsCallback(cb func(ScanMetricsReport)) {
	s.onScanMetrics = cb
}

func (s *Scanner) SetScanProgressCallback(cb func(ScanProgressReport)) {
	s.onScanProgress = cb
}

func (s *Scanner) currentConfig() config.Config {
	if s.config == nil {
		return config.Config{}
	}
	return s.config.Snapshot()
}

func (s *Scanner) scanOptions(force bool) ScanOptions {
	cfg := s.currentConfig()
	profile := NormalizeScanProfile(cfg.Scanner.ScanProfile)
	if profile == ScanProfileRepair {
		force = true
	}
	return ScanOptions{Force: force, Profile: profile}
}

func (s *Scanner) beginLibraryScan(libraryID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.active.libraries[libraryID]; exists {
		return false
	}
	s.active.libraries[libraryID] = struct{}{}
	return true
}

func (s *Scanner) endLibraryScan(libraryID int64) {
	s.mu.Lock()
	delete(s.active.libraries, libraryID)
	s.mu.Unlock()
}

func (s *Scanner) beginSeriesScan(seriesID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.active.series[seriesID]; exists {
		return false
	}
	s.active.series[seriesID] = struct{}{}
	return true
}

func (s *Scanner) endSeriesScan(seriesID int64) {
	s.mu.Lock()
	delete(s.active.series, seriesID)
	s.mu.Unlock()
}

type scanJob struct {
	path     string
	info     os.FileInfo
	existing *bookScanSnapshot // 已入库快照（增量扫描时非 nil），用于 fast 档位保留旧的 page_count/cover_path
}

type scanMetrics struct {
	discoveredArchives   atomic.Int64
	skippedArchives      atomic.Int64
	processedArchives    atomic.Int64
	openedArchives       atomic.Int64
	hashedFiles          atomic.Int64
	queuedCovers         atomic.Int64
	generatedCovers      atomic.Int64
	failedArchives       atomic.Int64
	ioWaitMillis         atomic.Int64
	pausedMillis         atomic.Int64
	thumbnailWriteMillis atomic.Int64
}

type scanMetricsSnapshot struct {
	discoveredArchives   int64
	skippedArchives      int64
	processedArchives    int64
	openedArchives       int64
	hashedFiles          int64
	queuedCovers         int64
	generatedCovers      int64
	failedArchives       int64
	ioWaitMillis         int64
	pausedMillis         int64
	thumbnailWriteMillis int64
}

type ScanMetricsReport struct {
	Scope                  string
	ID                     int64
	StorageProfile         string
	VolumeKey              string
	ArchiveOpenConcurrency int
	CoverConcurrency       int
	DiscoveredArchives     int64
	SkippedArchives        int64
	ProcessedArchives      int64
	OpenedArchives         int64
	HashedFiles            int64
	QueuedCovers           int64
	GeneratedCovers        int64
	FailedArchives         int64
	IOWaitMillis           int64
	PausedMillis           int64
	ThumbnailWriteMillis   int64
	DurationMillis         int64
}

type ScanProgressReport struct {
	Scope       string
	ID          int64
	Phase       string
	CurrentItem string
	Current     int64
	Total       int64
	Metrics     map[string]int64
}

type scanProgressReporter struct {
	scope   string
	id      int64
	metrics *scanMetrics
	cb      func(ScanProgressReport)

	mu       sync.Mutex
	lastSent time.Time
}

func newScanProgressReporter(scope string, id int64, metrics *scanMetrics, cb func(ScanProgressReport)) *scanProgressReporter {
	return &scanProgressReporter{scope: scope, id: id, metrics: metrics, cb: cb}
}

func (r *scanProgressReporter) publish(phase, currentItem string, force bool) {
	if r == nil || r.cb == nil {
		return
	}
	now := time.Now()
	r.mu.Lock()
	if !force && now.Sub(r.lastSent) < 250*time.Millisecond {
		r.mu.Unlock()
		return
	}
	r.lastSent = now
	r.mu.Unlock()

	snapshot := r.metrics.snapshot()
	current := snapshot.skippedArchives + snapshot.processedArchives
	total := snapshot.discoveredArchives
	if phase == "discovering" {
		current = snapshot.discoveredArchives
		total = 0
	}
	r.cb(ScanProgressReport{
		Scope:       r.scope,
		ID:          r.id,
		Phase:       phase,
		CurrentItem: currentItem,
		Current:     current,
		Total:       total,
		Metrics: map[string]int64{
			"discovered_archives": snapshot.discoveredArchives,
			"skipped_archives":    snapshot.skippedArchives,
			"processed_archives":  snapshot.processedArchives,
			"opened_archives":     snapshot.openedArchives,
			"hashed_files":        snapshot.hashedFiles,
			"queued_covers":       snapshot.queuedCovers,
			"generated_covers":    snapshot.generatedCovers,
			"failed_archives":     snapshot.failedArchives,
			"io_wait_ms":          snapshot.ioWaitMillis,
			"paused_ms":           snapshot.pausedMillis,
			"thumbnail_write_ms":  snapshot.thumbnailWriteMillis,
		},
	})
}

func (m *scanMetrics) snapshot() scanMetricsSnapshot {
	if m == nil {
		return scanMetricsSnapshot{}
	}
	return scanMetricsSnapshot{
		discoveredArchives:   m.discoveredArchives.Load(),
		skippedArchives:      m.skippedArchives.Load(),
		processedArchives:    m.processedArchives.Load(),
		openedArchives:       m.openedArchives.Load(),
		hashedFiles:          m.hashedFiles.Load(),
		queuedCovers:         m.queuedCovers.Load(),
		generatedCovers:      m.generatedCovers.Load(),
		failedArchives:       m.failedArchives.Load(),
		ioWaitMillis:         m.ioWaitMillis.Load(),
		pausedMillis:         m.pausedMillis.Load(),
		thumbnailWriteMillis: m.thumbnailWriteMillis.Load(),
	}
}

type bookScanSnapshot struct {
	modTime   time.Time
	size      int64
	pageCount int64
	coverPath sql.NullString
}

type ScanProfile string

const (
	ScanProfileFast     ScanProfile = "fast_scan"
	ScanProfileMetadata ScanProfile = "metadata_scan"
	ScanProfileIdentity ScanProfile = "identity_scan"
	ScanProfileRepair   ScanProfile = "repair_scan"
)

type ScanOptions struct {
	Force   bool
	Profile ScanProfile
}

func (s *Scanner) scanWorkerCount(cfg config.Config, rootPath string, opts ScanOptions) int {
	workers := cfg.Scanner.Workers
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}
	policy := config.ResolveStoragePolicy(cfg, rootPath)
	limit := policy.IOPolicy.ScanConcurrency
	if opts.Profile.opensArchive() {
		limit = storageIOLimit(limit, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if opts.Profile.computesQuickHash() || opts.Profile.computesFullHash(cfg) {
		limit = storageIOLimit(limit, policy.IOPolicy.HashConcurrency)
	}
	if limit > 0 && workers > limit {
		workers = limit
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

func storageIOLimit(values ...int) int {
	limit := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if limit == 0 || value < limit {
			limit = value
		}
	}
	return limit
}

func (s *Scanner) acquireStorageToken(ctx context.Context, policy config.ResolvedStoragePolicy, limit int, kind storageio.WorkKind) (func(), time.Duration, time.Duration, error) {
	if limit <= 0 || strings.TrimSpace(policy.VolumeKey) == "" {
		return func() {}, 0, 0, nil
	}
	lease, err := storageio.Default.Acquire(ctx, storageio.Request{
		VolumeKey:        policy.VolumeKey,
		Limit:            limit,
		Kind:             kind,
		PauseWhenReading: policy.IOPolicy.PauseBackgroundWhenReading,
		IdleOnly:         policy.IOPolicy.IdleOnlyHeavyTasks,
	})
	if err != nil {
		return nil, lease.Wait, lease.PausedWait, err
	}
	return lease.Release, lease.Wait, lease.PausedWait, nil
}

func NormalizeScanProfile(raw string) ScanProfile {
	switch ScanProfile(strings.ToLower(strings.TrimSpace(raw))) {
	case ScanProfileFast:
		return ScanProfileFast
	case ScanProfileIdentity:
		return ScanProfileIdentity
	case ScanProfileRepair:
		return ScanProfileRepair
	default:
		return ScanProfileMetadata
	}
}

func (p ScanProfile) opensArchive() bool {
	return p != ScanProfileFast
}

func (p ScanProfile) extractsMetadata() bool {
	return p == ScanProfileMetadata || p == ScanProfileIdentity || p == ScanProfileRepair
}

func (p ScanProfile) computesQuickHash() bool {
	return p == ScanProfileIdentity || p == ScanProfileRepair
}

func (p ScanProfile) computesFullHash(cfg config.Config) bool {
	return p == ScanProfileIdentity || p == ScanProfileRepair
}

type scanResult struct {
	seriesName           string
	seriesPath           string
	book                 database.UpsertBookByPathParams
	coverCandidate       *coverCandidate
	comicInfo            *parser.ComicInfo
	fileHash             string
	quickHash            string
	pathFingerprint      string
	pathFingerprintNoExt string
}

type coverCandidate struct {
	path      string
	pageName  string
	mediaType string
	bookHash  string
}

type coverJob struct {
	ctx       context.Context
	bookID    int64
	seriesID  int64
	libraryID int64
	candidate coverCandidate
	metrics   *scanMetrics
	progress  *scanProgressReporter
}

// ScanLibrary 递归扫描库目录查找漫画包，采用“发现文件 -> 解析归档 -> 批量入库”的三阶段流水线。
// 业务上它需要同时保证增量扫描够快、强制修复能重建封面和索引、任务进度能实时反馈给前端。
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID int64, rootPath string, force bool) error {
	if !s.beginLibraryScan(libraryID) {
		slog.Info("Library scan skipped because another scan is already running", "library_id", libraryID)
		return nil
	}
	defer s.endLibraryScan(libraryID)

	opts := s.scanOptions(force)
	started := time.Now()
	metrics := &scanMetrics{}
	progress := newScanProgressReporter("library", libraryID, metrics, s.onScanProgress)
	progress.publish("loading_existing_books", "", true)

	// 增量扫描先加载已入库文件的修改时间和大小，未变化的归档可以跳过重读，降低大库重复扫描成本。
	bookCache := make(map[string]bookScanSnapshot)

	if !opts.Force {
		existingBooks, err := s.store.ListBooksByLibrary(ctx, libraryID)
		if err != nil {
			slog.Warn("Failed to load existing books cache", "library_id", libraryID, "error", err)
			return err
		}

		for _, b := range existingBooks {
			bookCache[b.Path] = bookScanSnapshot{modTime: b.FileModifiedAt, size: b.Size, pageCount: b.PageCount, coverPath: b.CoverPath}
		}
	}

	jobs := make(chan scanJob, 1000)
	results := make(chan scanResult, 1000)

	var wg sync.WaitGroup

	// 第 2 阶段：解析工作池。
	// 并发数受全局 worker 配置与存储 IO 策略共同约束，避免网络盘、机械盘或大归档场景下拖慢阅读器。
	cfg := s.currentConfig()
	numWorkers := s.scanWorkerCount(cfg, rootPath, opts)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				s.workerProcess(ctx, libraryID, rootPath, job, opts, metrics, progress, results)
			}
		}()
	}

	// 第 3 阶段：数据库写入器。
	// 解析结果统一进入单个写入协程，减少 SQLite 并发写导致的锁冲突，同时便于批量刷新系列统计和搜索索引。
	ingestWg := sync.WaitGroup{}
	ingestWg.Add(1)
	go func() {
		defer ingestWg.Done()
		s.ingestResults(ctx, libraryID, results, metrics, progress)
	}()

	// 第 1 阶段：文件发现。
	// WalkDir 只负责识别候选漫画归档并投递任务，不打开归档内容，确保发现阶段可以快速响应暂停和取消。
	var walkErr error
	progress.publish("discovering", rootPath, true)
	walkErr = filepath.WalkDir(rootPath, func(path string, d os.DirEntry, err error) error {
		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			slog.Warn("Error accessing path", "path", path, "error", err)
			return nil
		}

		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if config.IsSupportedArchiveExtension(ext) {
			metrics.discoveredArchives.Add(1)
			progress.publish("discovering", path, false)
			info, err := d.Info()
			if err != nil {
				return nil
			}

			// 增量拦截：非强制扫描下检查修改时间
			if !opts.Force {
				if existing, exists := bookCache[path]; exists {
					// 若存在同名记录且时间与大小精确吻合，跳过这本卷的解析派发
					if existing.modTime.Equal(info.ModTime()) && existing.size == info.Size() {
						metrics.skippedArchives.Add(1)
						progress.publish("comparing", path, false)
						return nil
					}
				}
			}

			var existing *bookScanSnapshot
			if snap, ok := bookCache[path]; ok {
				existing = &snap
			}
			select {
			case jobs <- scanJob{path: path, info: info, existing: existing}:
				metrics.processedArchives.Add(1)
				progress.publish("reading_metadata", path, false)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	close(jobs) // 通知 Workers 没活儿了
	progress.publish("reading_metadata", "", true)
	wg.Wait()       // 等待所有 Worker 的解析收尾
	close(results)  // 通知 Ingester 没数据投递了
	ingestWg.Wait() // 等待 Ingester 将批次强刷入磁盘

	if walkErr == nil {
		walkErr = ctx.Err()
	}
	s.logScanCompleted("library", libraryID, rootPath, opts, metrics, time.Since(started), walkErr)
	progress.publish("completed", "", true)
	return walkErr
}

// ScanSeries 扫描单一系列目录，将新的卷添加到数据库中
func (s *Scanner) ScanSeries(ctx context.Context, seriesID int64, force bool) error {
	if !s.beginSeriesScan(seriesID) {
		slog.Info("Series scan skipped because another scan is already running", "series_id", seriesID)
		return nil
	}
	defer s.endSeriesScan(seriesID)

	series, err := s.store.GetSeries(ctx, seriesID)
	if err != nil {
		return fmt.Errorf("failed to get series: %w", err)
	}

	library, err := s.store.GetLibrary(ctx, series.LibraryID)
	if err != nil {
		return fmt.Errorf("failed to get library: %w", err)
	}

	opts := s.scanOptions(force)
	started := time.Now()
	metrics := &scanMetrics{}
	progress := newScanProgressReporter("series", seriesID, metrics, s.onScanProgress)
	progress.publish("loading_existing_books", "", true)
	bookCache := make(map[string]bookScanSnapshot)
	if !opts.Force {
		existingBooks, err := s.store.ListBooksBySeries(ctx, seriesID)
		if err == nil {
			for _, b := range existingBooks {
				bookCache[b.Path] = bookScanSnapshot{modTime: b.FileModifiedAt, size: b.Size, pageCount: b.PageCount, coverPath: b.CoverPath}
			}
		}
	}

	jobs := make(chan scanJob, 100)
	results := make(chan scanResult, 100)

	var wg sync.WaitGroup
	cfg := s.currentConfig()
	numWorkers := s.scanWorkerCount(cfg, library.Path, opts)
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				s.workerProcess(ctx, series.LibraryID, library.Path, job, opts, metrics, progress, results)
			}
		}()
	}

	ingestWg := sync.WaitGroup{}
	ingestWg.Add(1)
	go func() {
		defer ingestWg.Done()
		s.ingestResults(ctx, series.LibraryID, results, metrics, progress)
	}()

	var walkErr error
	progress.publish("discovering", series.Path, true)
	walkErr = filepath.WalkDir(series.Path, func(path string, d os.DirEntry, err error) error {
		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			slog.Warn("Error accessing path", "path", path, "error", err)
			return nil
		}
		if d.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if config.IsSupportedArchiveExtension(ext) {
			metrics.discoveredArchives.Add(1)
			progress.publish("discovering", path, false)
			info, err := d.Info()
			if err != nil {
				return nil
			}

			if !opts.Force {
				if existing, exists := bookCache[path]; exists {
					if existing.modTime.Equal(info.ModTime()) && existing.size == info.Size() {
						metrics.skippedArchives.Add(1)
						progress.publish("comparing", path, false)
						return nil
					}
				}
			}

			var existing *bookScanSnapshot
			if snap, ok := bookCache[path]; ok {
				existing = &snap
			}
			select {
			case jobs <- scanJob{path: path, info: info, existing: existing}:
				metrics.processedArchives.Add(1)
				progress.publish("reading_metadata", path, false)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	})

	close(jobs)
	progress.publish("reading_metadata", "", true)
	wg.Wait()
	close(results)
	ingestWg.Wait()

	if walkErr == nil {
		walkErr = ctx.Err()
	}
	s.logScanCompleted("series", seriesID, library.Path, opts, metrics, time.Since(started), walkErr)
	progress.publish("completed", "", true)
	return walkErr
}

// CleanupLibrary 验证并清理指定资料库中的失效资源记录
// cleanupDeleteRatioGuard 熔断阈值：当一次清理判定为“缺失”的系列占全库比例超过该值时，
// 极可能是存储离线、盘符漂移或 UNC 断连造成的整库误判，而非用户真的删了这么多文件。
// 此时中止清理，保护系列、书籍及其阅读进度不被级联删除。
const cleanupDeleteRatioGuard = 0.5

func (s *Scanner) CleanupLibrary(ctx context.Context, libraryID int64) error {
	// 先探测资料库根目录：存储离线时库内所有系列路径都会“看起来不存在”，
	// 若继续清理会把整库系列连同书籍、阅读进度一并级联删除。任何 Stat 错误
	// （不存在 / 权限 / 超时 / 网络）或非目录都视为不可达，直接中止。
	library, err := s.store.GetLibrary(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("failed to load library %d: %w", libraryID, err)
	}
	if info, statErr := os.Stat(library.Path); statErr != nil || !info.IsDir() {
		return fmt.Errorf("library root %q unreachable, aborting cleanup to avoid mass deletion: %w", library.Path, statErr)
	}

	seriesList, err := s.store.ListSeriesByLibrary(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("failed to list series: %w", err)
	}

	// 第一遍：仅收集“确证缺失”（os.IsNotExist）的系列；权限、超时、网络等不确定错误
	// 一律跳过而非删除，避免瞬时 IO 故障被误判为文件消失。
	var missingSeries []database.ListSeriesByLibraryRow
	for _, series := range seriesList {
		if _, statErr := os.Stat(series.Path); statErr != nil {
			if os.IsNotExist(statErr) {
				missingSeries = append(missingSeries, series)
			} else {
				slog.Warn("Skipping series with ambiguous stat error during cleanup",
					"series_id", series.ID, "path", series.Path, "error", statErr)
			}
		}
	}

	// 熔断：待删系列占比过高，极可能是存储异常而非真实删除。
	if total := len(seriesList); total > 0 && float64(len(missingSeries))/float64(total) > cleanupDeleteRatioGuard {
		return fmt.Errorf("cleanup aborted: %d/%d series appear missing (> %.0f%%), likely a storage issue rather than real deletions",
			len(missingSeries), total, cleanupDeleteRatioGuard*100)
	}

	removedSeries := make(map[int64]bool, len(missingSeries))
	for _, series := range missingSeries {
		slog.Info("Removing missing series", "series_id", series.ID, "path", series.Path)
		if err := s.store.DeleteSeries(ctx, series.ID); err != nil {
			slog.Error("Failed to delete series", "series_id", series.ID, "error", err)
			continue
		}
		removedSeries[series.ID] = true
	}

	// 存活的系列再逐卷清理缺失书籍（同样只在确证缺失时删除）。
	for _, series := range seriesList {
		if removedSeries[series.ID] {
			continue
		}
		books, err := s.store.ListBooksBySeries(ctx, series.ID)
		if err != nil {
			continue
		}
		booksChanged := false
		for _, book := range books {
			if _, statErr := os.Stat(book.Path); statErr != nil {
				if os.IsNotExist(statErr) {
					slog.Info("Removing missing book", "book_id", book.ID, "path", book.Path)
					if err := s.store.DeleteBook(ctx, book.ID); err != nil {
						slog.Error("Failed to delete book", "book_id", book.ID, "error", err)
					}
					booksChanged = true
				} else {
					slog.Warn("Skipping book with ambiguous stat error during cleanup",
						"book_id", book.ID, "path", book.Path, "error", statErr)
				}
			}
		}
		// 如果有卷被删除，更新系列的统计信息
		if booksChanged {
			_ = s.store.UpdateSeriesStatistics(ctx, database.UpdateSeriesStatisticsParams{
				SeriesID:   series.ID,
				SeriesID_2: series.ID,
				SeriesID_3: series.ID,
				ID:         series.ID,
			})
		}
	}

	slog.Info("Library cleanup completed", "library_id", libraryID, "removed_series", len(removedSeries))
	return nil
}

func (s *Scanner) logScanCompleted(scope string, id int64, rootPath string, opts ScanOptions, metrics *scanMetrics, duration time.Duration, err error) {
	snapshot := metrics.snapshot()
	policy := config.ResolveStoragePolicy(s.currentConfig(), rootPath)
	attrs := []any{
		"scope", scope,
		"scan_profile", opts.Profile,
		"force", opts.Force,
		"storage_profile", policy.StorageProfile,
		"volume_key", policy.VolumeKey,
		"archive_open_concurrency", policy.IOPolicy.ArchiveOpenConcurrency,
		"cover_concurrency", policy.IOPolicy.CoverConcurrency,
		"discovered_archives", snapshot.discoveredArchives,
		"skipped_archives", snapshot.skippedArchives,
		"processed_archives", snapshot.processedArchives,
		"opened_archives", snapshot.openedArchives,
		"hashed_files", snapshot.hashedFiles,
		"queued_covers", snapshot.queuedCovers,
		"generated_covers", snapshot.generatedCovers,
		"failed_archives", snapshot.failedArchives,
		"io_wait_ms", snapshot.ioWaitMillis,
		"paused_ms", snapshot.pausedMillis,
		"thumbnail_write_ms", snapshot.thumbnailWriteMillis,
		"duration_ms", duration.Milliseconds(),
	}
	switch scope {
	case "series":
		attrs = append(attrs, "series_id", id)
	default:
		attrs = append(attrs, "library_id", id)
	}
	if err != nil {
		attrs = append(attrs, "error", err)
		slog.Warn("Scan completed with errors", attrs...)
		s.publishScanMetrics(scope, id, policy, snapshot, duration)
		return
	}
	slog.Info("Scan completed", attrs...)
	s.publishScanMetrics(scope, id, policy, snapshot, duration)
}

func (s *Scanner) publishScanMetrics(scope string, id int64, policy config.ResolvedStoragePolicy, snapshot scanMetricsSnapshot, duration time.Duration) {
	if s.onScanMetrics == nil {
		return
	}
	s.onScanMetrics(ScanMetricsReport{
		Scope:                  scope,
		ID:                     id,
		StorageProfile:         policy.StorageProfile,
		VolumeKey:              policy.VolumeKey,
		ArchiveOpenConcurrency: policy.IOPolicy.ArchiveOpenConcurrency,
		CoverConcurrency:       policy.IOPolicy.CoverConcurrency,
		DiscoveredArchives:     snapshot.discoveredArchives,
		SkippedArchives:        snapshot.skippedArchives,
		ProcessedArchives:      snapshot.processedArchives,
		OpenedArchives:         snapshot.openedArchives,
		HashedFiles:            snapshot.hashedFiles,
		QueuedCovers:           snapshot.queuedCovers,
		GeneratedCovers:        snapshot.generatedCovers,
		FailedArchives:         snapshot.failedArchives,
		IOWaitMillis:           snapshot.ioWaitMillis,
		PausedMillis:           snapshot.pausedMillis,
		ThumbnailWriteMillis:   snapshot.thumbnailWriteMillis,
		DurationMillis:         duration.Milliseconds(),
	})
}

func (s *Scanner) workerProcess(ctx context.Context, libIDInt int64, rootPath string, job scanJob, opts ScanOptions, metrics *scanMetrics, progress *scanProgressReporter, results chan<- scanResult) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	if err := taskcontrol.Wait(ctx); err != nil {
		return
	}

	cfg := s.currentConfig()
	storagePolicy := config.ResolveStoragePolicy(cfg, rootPath)
	var arc parser.Archive
	var pages []parser.PageMetadata
	closeArchive := func() {}
	if opts.Profile.opensArchive() {
		var err error
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		progress.publish("reading_metadata", job.path, false)
		releaseToken, waited, paused, err := s.acquireStorageToken(ctx, storagePolicy, storageIOLimit(storagePolicy.IOPolicy.ScanConcurrency, storagePolicy.IOPolicy.ArchiveOpenConcurrency), storageio.WorkKindMetadataScan)
		if err != nil {
			return
		}
		if metrics != nil && waited > 0 {
			metrics.ioWaitMillis.Add(waited.Milliseconds())
		}
		if metrics != nil && paused > 0 {
			metrics.pausedMillis.Add(paused.Milliseconds())
		}
		arc, err = s.openArchive(job.path)
		if err != nil {
			releaseToken()
			if metrics != nil {
				metrics.failedArchives.Add(1)
			}
			slog.Warn("Failed to open archive (may be corrupted)", "path", job.path, "error", err)
			return
		}
		if metrics != nil {
			metrics.openedArchives.Add(1)
		}
		progress.publish("reading_metadata", job.path, false)
		closed := false
		closeArchive = func() {
			if closed {
				return
			}
			closed = true
			arc.Close()
			releaseToken()
		}
		defer closeArchive()

		pages, err = arc.GetPages()
		if err != nil {
			if metrics != nil {
				metrics.failedArchives.Add(1)
			}
			slog.Warn("Failed to scan pages inside archive", "path", job.path, "error", err)
			return
		}
	}

	// 基于路径、修改时间和大小生成复合哈希，确保文件内容变动时缩略图强制刷新
	hashSource := fmt.Sprintf("%s|%d|%d", job.path, job.info.ModTime().Unix(), job.info.Size())
	bookHash := fmt.Sprintf("%x", sha1.Sum([]byte(hashSource)))
	baseName := filepath.Base(job.path)
	bookTitle := sql.NullString{
		String: strings.TrimSuffix(baseName, filepath.Ext(baseName)),
		Valid:  true,
	}

	var seriesName, seriesPath string
	var volumeName string
	relPath, err := filepath.Rel(rootPath, job.path)
	if err == nil {
		parts := strings.Split(relPath, string(filepath.Separator))
		if len(parts) > 2 {
			// 第一级目录作为 Series，第二级目录作为 Volume
			seriesName = parts[0]
			seriesPath = filepath.Join(rootPath, seriesName)
			volumeName = parts[1]
		} else if len(parts) > 1 {
			// 第一级目录作为 Series，无 Volume
			seriesName = parts[0]
			seriesPath = filepath.Join(rootPath, seriesName)
		} else {
			// 如果直接放在资源库根目录，则以去后缀的文件名作为 Series
			seriesName = strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
			seriesPath = filepath.Join(rootPath, seriesName)
		}
	} else {
		// Fallback
		seriesPath = filepath.Dir(job.path)
		seriesName = filepath.Base(seriesPath)
	}

	// 尝试解析文件名中的第一个可能代表话数的数字作为自然排序依据，支持 01、第十话 等格式。
	var sortNumber float64 = 0
	if val, ok := booksort.ExtractSortNumber(bookTitle.String); ok {
		sortNumber = val
	}

	// 封面缓存只在扫描 worker 内做轻量命中检查；缺失时交给后台封面队列生成。
	var coverPath sql.NullString
	var coverHint *coverCandidate
	if opts.Profile.extractsMetadata() && len(pages) > 0 {
		if existing := existingThumbnailPath(cfg, bookHash); existing.Valid {
			coverPath = existing
		} else {
			coverHint = &coverCandidate{
				path:      job.path,
				pageName:  pages[0].Name,
				mediaType: pages[0].MediaType,
				bookHash:  bookHash,
			}
		}
	} else if opts.Profile.extractsMetadata() {
		slog.Warn("No pages found in archive to extract cover", "path", job.path)
	}

	// 尝试提取 ComicInfo.xml；归档读取完成后立即释放 IO token，避免后续 hash 再申请同盘 token 时自我等待。
	var cInfo *parser.ComicInfo
	if opts.Profile.extractsMetadata() && arc != nil {
		xmlData, err := arc.ReadMetadataFile("ComicInfo.xml")
		if err == nil {
			if parsed, err := parser.ParseComicInfo(xmlData); err == nil {
				cInfo = parsed
			}
		}
	}
	closeArchive()

	book := database.UpsertBookByPathParams{
		LibraryID:      libIDInt,
		Name:           baseName,
		Path:           job.path,
		Size:           job.info.Size(),
		FileModifiedAt: job.info.ModTime(),
		Volume:         volumeName,
		Title:          bookTitle,
		PageCount:      int64(len(pages)),
		SortNumber:     sql.NullFloat64{Float64: sortNumber, Valid: true},
		CoverPath:      coverPath,
	}
	// fast 档位不开归档，pages 与 coverPath 恒空；增量扫描（有旧快照）时保留已入库的
	// page_count/cover_path，避免 upsert 把变动书籍的页数/封面清零、封面被永久抹掉。
	if !opts.Profile.opensArchive() && job.existing != nil {
		if book.PageCount == 0 && job.existing.pageCount > 0 {
			book.PageCount = job.existing.pageCount
		}
		if (!book.CoverPath.Valid || book.CoverPath.String == "") && job.existing.coverPath.Valid && job.existing.coverPath.String != "" {
			book.CoverPath = job.existing.coverPath
		}
	}
	var fileHash string
	if opts.Profile.computesFullHash(cfg) {
		var err error
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		progress.publish("hashing", job.path, false)
		releaseToken, waited, paused, tokenErr := s.acquireStorageToken(ctx, storagePolicy, storageIOLimit(storagePolicy.IOPolicy.ScanConcurrency, storagePolicy.IOPolicy.HashConcurrency), storageio.WorkKindIdentityHash)
		if tokenErr != nil {
			return
		}
		if metrics != nil && waited > 0 {
			metrics.ioWaitMillis.Add(waited.Milliseconds())
		}
		if metrics != nil && paused > 0 {
			metrics.pausedMillis.Add(paused.Milliseconds())
		}
		fileHash, err = koreader.FingerprintFile(job.path)
		releaseToken()
		if metrics != nil {
			metrics.hashedFiles.Add(1)
		}
		progress.publish("hashing", job.path, false)
		if err != nil {
			slog.Warn("Failed to compute book binary fingerprint", "path", job.path, "error", err, "scan_profile", opts.Profile)
		}
	}

	var quickHash string
	if opts.Profile.computesQuickHash() {
		var err error
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		progress.publish("hashing", job.path, false)
		releaseToken, waited, paused, tokenErr := s.acquireStorageToken(ctx, storagePolicy, storageIOLimit(storagePolicy.IOPolicy.ScanConcurrency, storagePolicy.IOPolicy.HashConcurrency), storageio.WorkKindIdentityHash)
		if tokenErr != nil {
			return
		}
		if metrics != nil && waited > 0 {
			metrics.ioWaitMillis.Add(waited.Milliseconds())
		}
		if metrics != nil && paused > 0 {
			metrics.pausedMillis.Add(paused.Milliseconds())
		}
		quickHash, err = koreader.FingerprintQuickFile(job.path)
		releaseToken()
		if metrics != nil {
			metrics.hashedFiles.Add(1)
		}
		progress.publish("hashing", job.path, false)
		if err != nil {
			slog.Warn("Failed to compute quick book fingerprint", "path", job.path, "error", err, "scan_profile", opts.Profile)
		}
	}

	res := scanResult{
		seriesName:           seriesName,
		seriesPath:           seriesPath,
		book:                 book,
		coverCandidate:       coverHint,
		comicInfo:            cInfo,
		fileHash:             fileHash,
		quickHash:            quickHash,
		pathFingerprint:      koreader.FingerprintRelativePath(rootPath, job.path, false),
		pathFingerprintNoExt: koreader.FingerprintRelativePath(rootPath, job.path, true),
	}

	select {
	case results <- res:
	case <-ctx.Done():
	}
}

func (s *Scanner) ingestResults(ctx context.Context, libIDInt int64, results <-chan scanResult, metrics *scanMetrics, progress *scanProgressReporter) {
	// 系列缓存：路径 -> 原系列对象 (保留原属性能防止 Upsert 被 NULL 覆盖)
	seriesCache := make(map[string]database.ListSeriesByLibraryRow)
	// 锁定字段缓存：ID -> 锁定字段列表 (用 map 提高查找速度)
	lockedFieldsCache := make(map[int64]map[string]bool)

	// 预加载已有的 Series
	existingSeries, _ := s.store.ListSeriesByLibrary(ctx, libIDInt)
	for _, series := range existingSeries {
		seriesCache[series.Path] = series

		lfMap := make(map[string]bool)
		if series.LockedFields.Valid && series.LockedFields.String != "" {
			for _, f := range strings.Split(series.LockedFields.String, ",") {
				lfMap[strings.TrimSpace(f)] = true
			}
		}
		lockedFieldsCache[series.ID] = lfMap
	}

	var batch []scanResult
	const batchSize = 100 // 每蓄满 100 卷漫画就开启一次写事务

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		progress.publish("writing_database", "", true)

		var coverJobs []coverJob
		updatedSeriesIDs := make(map[int64]bool)

		err := s.store.ExecTx(ctx, func(q *database.Queries) error {
			for _, res := range batch {
				// 获取或创建/更新归属系列
				var seriesID int64
				existingS, ok := seriesCache[res.seriesPath]
				if ok {
					seriesID = existingS.ID
				}

				// 提取元数据准备
				var rSummary, rPublisher, rStatus, rLang string
				if res.comicInfo != nil {
					rSummary = res.comicInfo.Summary
					rPublisher = res.comicInfo.Publisher
					rLang = res.comicInfo.LanguageISO
				}
				var rating float64
				if res.comicInfo != nil && res.comicInfo.CommunityRating > 0 {
					rating = float64(res.comicInfo.CommunityRating)
				}

				if !ok {
					// 初次创建
					createdSeries, err := q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
						LibraryID:    libIDInt,
						Name:         res.seriesName,
						Path:         res.seriesPath,
						Title:        sql.NullString{String: res.seriesName, Valid: true},
						Summary:      sql.NullString{String: rSummary, Valid: rSummary != ""},
						Publisher:    sql.NullString{String: rPublisher, Valid: rPublisher != ""},
						Status:       sql.NullString{String: rStatus, Valid: rStatus != ""},
						Rating:       sql.NullFloat64{Float64: rating, Valid: rating > 0},
						Language:     sql.NullString{String: rLang, Valid: rLang != ""},
						LockedFields: sql.NullString{String: "title", Valid: true},
						VolumeCount:  0,
						BookCount:    0,
						TotalPages:   0,
						NameInitial:  database.SeriesInitial(res.seriesName, res.seriesName),
					})
					if err != nil {
						slog.Error("Failed to create/upsert series", "series_name", res.seriesName, "error", err)
						continue
					}
					seriesID = createdSeries.ID
					// 为了保持下文逻辑，我们塞一个临时的进去
					seriesCache[res.seriesPath] = database.ListSeriesByLibraryRow{ID: seriesID, Path: res.seriesPath}
				} else {
					// 已存在的系列，利用 UpsertSeriesByPath 去更新其累积统计和元数据（仅当有新元数据时增补）
					if res.comicInfo != nil {
						// 检查字段锁定机制
						locks := lockedFieldsCache[seriesID]
						if locks == nil {
							locks = make(map[string]bool)
						}
						// 系列名默认始终锁定，防止被外部刮削覆盖
						locks["title"] = true

						// 若被锁定则沿用旧有库中的数据，不被更新的 NULL 覆盖掉
						getStr := func(field string, newVal string) sql.NullString {
							if locks[field] {
								// 从缓存的老对象中读
								switch field {
								case "summary":
									return existingS.Summary
								case "publisher":
									return existingS.Publisher
								case "status":
									return existingS.Status
								case "language":
									return existingS.Language
								}
							}
							return sql.NullString{String: newVal, Valid: newVal != ""}
						}

						getRating := func() sql.NullFloat64 {
							if locks["rating"] {
								return existingS.Rating
							}
							return sql.NullFloat64{Float64: rating, Valid: rating > 0}
						}

						_, _ = q.UpsertSeriesByPath(ctx, database.UpsertSeriesByPathParams{
							LibraryID: libIDInt,
							Name:      res.seriesName,
							Path:      res.seriesPath,
							Title:     sql.NullString{String: res.seriesName, Valid: true},
							Summary:   getStr("summary", rSummary),
							Publisher: getStr("publisher", rPublisher),
							Status:    getStr("status", rStatus),
							Rating:    getRating(),
							Language:  getStr("language", rLang),
							// LockedFields 这里应该保持原样，所以 Valid 设为 false 让 Upsert 判定或传旧值
							// 因为我们的 Upsert 里会用 excluded.locked_fields 覆盖，为了不丢掉我们传回现有的锁。
							LockedFields: sql.NullString{String: getKeys(locks), Valid: true},
							VolumeCount:  existingS.VolumeCount,
							BookCount:    existingS.BookCount,
							TotalPages:   existingS.TotalPages,
							NameInitial:  database.SeriesInitial(res.seriesName, res.seriesName),
						})
					}
				}
				res.book.SeriesID = seriesID
				updatedSeriesIDs[seriesID] = true

				// 维护系列与标签、作者的多对多关系 (在单卷有新元数据时重刷)
				if res.comicInfo != nil {
					// 为每个卷提取补充，由于事务中，且中间表用 INSERT OR IGNORE, 不会报错。
					tags := res.comicInfo.GetTags()
					for _, t := range tags {
						if inserted, err := q.UpsertTag(ctx, t); err == nil {
							_ = q.LinkSeriesTag(ctx, database.LinkSeriesTagParams{SeriesID: seriesID, TagID: inserted.ID})
						}
					}

					authors := res.comicInfo.GetAuthors()
					for _, a := range authors {
						if inserted, err := q.UpsertAuthor(ctx, database.UpsertAuthorParams{Name: a.Name, Role: a.Role}); err == nil {
							_ = q.LinkSeriesAuthor(ctx, database.LinkSeriesAuthorParams{SeriesID: seriesID, AuthorID: inserted.ID})
						}
					}
				}

				// 使用 Upsert 模式：同路径书籍只更新元数据，保留 last_read_page / last_read_at，返回带主键的对象
				actualBook, err := q.UpsertBookByPath(ctx, res.book)
				if err != nil {
					slog.Error("Failed to upsert book", "path", res.book.Path, "error", err)
					continue
				}
				if err := q.UpdateBookIdentity(ctx, database.UpdateBookIdentityParams{
					ID:                   actualBook.ID,
					FileHash:             res.fileHash,
					QuickHash:            res.quickHash,
					PathFingerprint:      res.pathFingerprint,
					PathFingerprintNoExt: res.pathFingerprintNoExt,
				}); err != nil {
					slog.Warn("Failed to update book identity", "book_id", actualBook.ID, "path", actualBook.Path, "error", err)
				}
				if res.coverCandidate != nil && (!actualBook.CoverPath.Valid || actualBook.CoverPath.String == "") {
					coverJobs = append(coverJobs, coverJob{
						ctx:       ctx,
						bookID:    actualBook.ID,
						seriesID:  actualBook.SeriesID,
						libraryID: libIDInt,
						candidate: *res.coverCandidate,
						metrics:   metrics,
						progress:  progress,
					})
				}

			}
			// 强力补丁：在批处理提交后，对该批次涉及的所有系列进行统计重算，确保数据最终一致性。
			// 虽然这样多了一些 SQL，但在扫描性能层面由于 SQLite WAL + PageCache 与 SSD 极速 IO 相对微乎其微。
			for sid := range updatedSeriesIDs {
				if err := q.UpdateSeriesStatistics(ctx, database.UpdateSeriesStatisticsParams{
					SeriesID:   sid,
					SeriesID_2: sid,
					SeriesID_3: sid,
					ID:         sid,
				}); err != nil {
					slog.Warn("Failed to update series statistics", "series_id", sid, "err", err)
				}
				if err := q.RefreshSeriesStats(ctx, sid); err != nil {
					slog.Warn("Failed to refresh series stats", "series_id", sid, "err", err)
				}
			}

			return nil
		})

		if err != nil {
			// 整批写事务失败会丢弃最多 batchSize 本书。此前静默丢弃、任务仍报成功；
			// 现把丢弃数计入 failedArchives，使其在扫描完成日志与指标中可见。
			slog.Error("Batch ingest transaction failed, dropping batch", "book_count", len(batch), "error", err)
			metrics.failedArchives.Add(int64(len(batch)))
		} else {
			slog.Info("Successfully ingested batch", "book_count", len(batch))
			if s.onBatchIngested != nil {
				s.onBatchIngested("batch_inserted")
			}
			progress.publish("queueing_covers", "", true)
			s.enqueueCoverJobs(ctx, coverJobs)
		}

		batch = batch[:0]
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-results:
			if !ok {
				flush() // 通道被收尾，最后一次刷盘
				if s.onBatchIngested != nil {
					s.onBatchIngested("scan_completed")
				}
				return
			}
			batch = append(batch, res)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush() // 按时间自然聚合，避免低频挂起锁
		}
	}
}

// 提取 locks 字典的所有 key 重组成字符串
func getKeys(m map[string]bool) string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}

func thumbnailBaseDir(cfg config.Config) string {
	if cfg.Cache.Dir != "" {
		return cfg.Cache.Dir
	}
	return filepath.Join(".", "data", "thumbnails")
}

func thumbnailSubDir(bookHash string) string {
	if len(bookHash) >= 2 {
		return bookHash[:2]
	}
	return ""
}

func existingThumbnailPath(cfg config.Config, bookHash string) sql.NullString {
	subDir := thumbnailSubDir(bookHash)
	thumbDir := filepath.Join(thumbnailBaseDir(cfg), subDir)
	for _, ext := range []string{".webp", ".jpg", ".jpeg", ".png", ".avif"} {
		fileName := bookHash + ext
		if _, err := os.Stat(filepath.Join(thumbDir, fileName)); err == nil {
			return sql.NullString{String: filepath.ToSlash(filepath.Join(subDir, fileName)), Valid: true}
		}
	}
	return sql.NullString{}
}

func (s *Scanner) enqueueCoverJobs(ctx context.Context, jobs []coverJob) {
	if len(jobs) == 0 {
		return
	}
	s.startCoverWorkers()
	for _, job := range jobs {
		if err := taskcontrol.Wait(ctx); err != nil {
			return
		}
		s.coverWG.Add(1)
		select {
		case s.coverQueue <- job:
			if job.metrics != nil {
				job.metrics.queuedCovers.Add(1)
			}
		case <-ctx.Done():
			s.coverWG.Done()
			return
		}
	}
}

func (s *Scanner) startCoverWorkers() {
	s.coverOnce.Do(func() {
		s.coverQueue = make(chan coverJob, 1024)
		workers := s.currentConfig().Scanner.Workers
		if workers <= 0 {
			workers = runtime.NumCPU()
		}
		workers = workers / 2
		if workers < 1 {
			workers = 1
		}
		if workers > 4 {
			workers = 4
		}
		policy := config.ResolveStoragePolicy(s.currentConfig(), "")
		if policy.IOPolicy.CoverConcurrency > 0 && workers > policy.IOPolicy.CoverConcurrency {
			workers = policy.IOPolicy.CoverConcurrency
		}
		if workers < 1 {
			workers = 1
		}
		for i := 0; i < workers; i++ {
			go func() {
				for job := range s.coverQueue {
					s.runCoverJob(job)
					s.coverWG.Done()
				}
			}()
		}
	})
}

func (s *Scanner) runCoverJob(job coverJob) {
	ctx := job.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := taskcontrol.Wait(ctx); err != nil {
		return
	}

	cfg := s.currentConfig()
	coverPath, err := s.generateBookThumbnail(ctx, job.candidate, cfg, job.metrics)
	if err != nil {
		slog.Warn("Failed to generate queued thumbnail", "book_id", job.bookID, "path", job.candidate.path, "error", err)
		return
	}
	if !coverPath.Valid || coverPath.String == "" {
		return
	}

	rowsAffected, err := s.store.SetBookCoverIfMissing(ctx, database.SetBookCoverIfMissingParams{
		CoverPath: coverPath,
		ID:        job.bookID,
	})
	if err != nil {
		removeGeneratedThumbnail(cfg, coverPath.String)
		slog.Warn("Failed to update queued thumbnail cover path", "book_id", job.bookID, "error", err)
		return
	}
	if rowsAffected == 0 {
		return
	}
	if job.metrics != nil {
		job.metrics.generatedCovers.Add(1)
	}
	if job.progress != nil {
		job.progress.publish("queueing_covers", job.candidate.path, false)
	}
	if err := s.store.RefreshSeriesStats(ctx, job.seriesID); err != nil {
		slog.Warn("Failed to refresh series stats after queued thumbnail", "series_id", job.seriesID, "error", err)
	}
	if s.onBatchIngested != nil {
		s.onBatchIngested("thumbnail_updated")
	}
}

func (s *Scanner) generateBookThumbnail(ctx context.Context, candidate coverCandidate, cfg config.Config, metrics *scanMetrics) (sql.NullString, error) {
	if existing := existingThumbnailPath(cfg, candidate.bookHash); existing.Valid {
		return existing, nil
	}

	storagePolicy := config.ResolveStoragePolicy(cfg, candidate.path)
	releaseToken, waited, paused, err := s.acquireStorageToken(ctx, storagePolicy, storageIOLimit(storagePolicy.IOPolicy.ArchiveOpenConcurrency, storagePolicy.IOPolicy.CoverConcurrency), storageio.WorkKindCoverBuild)
	if err != nil {
		return sql.NullString{}, err
	}
	tokenReleased := false
	releaseSourceToken := func() {
		if tokenReleased {
			return
		}
		tokenReleased = true
		releaseToken()
	}
	defer releaseSourceToken()
	if metrics != nil && waited > 0 {
		metrics.ioWaitMillis.Add(waited.Milliseconds())
	}
	if metrics != nil && paused > 0 {
		metrics.pausedMillis.Add(paused.Milliseconds())
	}
	if waited >= 250*time.Millisecond {
		slog.Info("Queued thumbnail waited for storage IO token",
			"path", candidate.path,
			"storage_profile", storagePolicy.StorageProfile,
			"volume_key", storagePolicy.VolumeKey,
			"io_wait_ms", waited.Milliseconds(),
		)
	}

	arc, err := s.openArchive(candidate.path)
	if err != nil {
		return sql.NullString{}, err
	}

	select {
	case <-ctx.Done():
		arc.Close()
		return sql.NullString{}, ctx.Err()
	default:
	}

	pageData, err := arc.ReadPage(candidate.pageName)
	arc.Close()
	releaseSourceToken()
	if err != nil {
		return sql.NullString{}, err
	}

	targetFormat := cfg.Scanner.ThumbnailFormat
	if targetFormat == "" {
		targetFormat = "webp"
	}

	processed, contentType, err := images.ProcessImage(pageData, candidate.mediaType, images.ProcessOptions{
		Width: 400, Quality: 82, Format: targetFormat,
	})
	if err != nil || len(processed) == 0 {
		slog.Warn("Primary thumbnail format generation failed, falling back to jpeg", "format", targetFormat, "path", candidate.path, "error", err)
		processed, contentType, err = images.ProcessImage(pageData, candidate.mediaType, images.ProcessOptions{
			Width: 400, Quality: 82, Format: "jpeg",
		})
		if err != nil {
			return sql.NullString{}, err
		}
	}
	if len(processed) == 0 {
		return sql.NullString{}, fmt.Errorf("no processed thumbnail data generated")
	}

	subDir := thumbnailSubDir(candidate.bookHash)
	thumbDir := filepath.Join(thumbnailBaseDir(cfg), subDir)
	fileName := candidate.bookHash + extensionFromContentType(contentType, targetFormat)
	fullPath := filepath.Join(thumbDir, fileName)
	writeWait, writePaused, writeDuration, err := s.writeThumbnailFile(ctx, cfg, storagePolicy, candidate.path, thumbDir, fullPath, processed)
	if metrics != nil {
		if writeWait > 0 {
			metrics.ioWaitMillis.Add(writeWait.Milliseconds())
		}
		if writePaused > 0 {
			metrics.pausedMillis.Add(writePaused.Milliseconds())
		}
		if writeDuration > 0 {
			metrics.thumbnailWriteMillis.Add(writeDuration.Milliseconds())
		}
	}
	if err != nil {
		return sql.NullString{}, err
	}
	if writeDuration >= 250*time.Millisecond || writeWait >= 250*time.Millisecond {
		slog.Info("Queued thumbnail cache write completed",
			"path", candidate.path,
			"thumbnail_path", fullPath,
			"storage_profile", storagePolicy.StorageProfile,
			"volume_key", config.VolumeKey(fullPath),
			"io_wait_ms", writeWait.Milliseconds(),
			"paused_ms", writePaused.Milliseconds(),
			"thumbnail_write_ms", writeDuration.Milliseconds(),
		)
	}
	return sql.NullString{String: filepath.ToSlash(filepath.Join(subDir, fileName)), Valid: true}, nil
}

func removeGeneratedThumbnail(cfg config.Config, relativePath string) {
	relativePath = strings.TrimSpace(relativePath)
	if relativePath == "" || filepath.IsAbs(relativePath) {
		return
	}
	fullPath := filepath.Join(thumbnailBaseDir(cfg), filepath.FromSlash(relativePath))
	_ = os.Remove(fullPath)
}

func (s *Scanner) writeThumbnailFile(ctx context.Context, cfg config.Config, sourcePolicy config.ResolvedStoragePolicy, sourcePath, thumbDir, fullPath string, data []byte) (time.Duration, time.Duration, time.Duration, error) {
	writePolicy := config.ResolveStoragePolicy(cfg, thumbDir)
	if config.SameVolume(sourcePath, thumbDir) {
		writePolicy = sourcePolicy
		writePolicy.VolumeKey = config.VolumeKey(thumbDir)
	}
	releaseToken, waited, paused, err := s.acquireStorageToken(ctx, writePolicy, writePolicy.IOPolicy.CoverConcurrency, storageio.WorkKindCacheWrite)
	if err != nil {
		return waited, paused, 0, err
	}
	defer releaseToken()

	started := time.Now()
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return waited, paused, time.Since(started), err
	}
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return waited, paused, time.Since(started), err
	}
	return waited, paused, time.Since(started), nil
}

func (s *Scanner) waitForCoverQueue(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.coverWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scanner) WaitForCoverQueue(ctx context.Context) error {
	return s.waitForCoverQueue(ctx)
}

func extensionFromContentType(contentType, fallbackFormat string) string {
	switch {
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "avif"):
		return ".avif"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	}

	switch strings.ToLower(strings.TrimSpace(fallbackFormat)) {
	case "jpeg", "jpg":
		return ".jpg"
	case "png":
		return ".png"
	case "avif":
		return ".avif"
	default:
		return ".webp"
	}
}

// CleanupThumbnails scans the thumbnails directory and removes any files
// that are not referenced in the database (by books or series_stats).
// It also cleans up empty subdirectories.
func (s *Scanner) CleanupThumbnails(ctx context.Context, progressCb func(current, total int, msg string)) error {
	cfg := s.currentConfig()
	thumbDir := thumbnailBaseDir(cfg)

	// Fetch all referenced cover paths from DB
	usedPaths := make(map[string]bool)

	// Books
	bookCovers, err := s.store.GetReferencedBookCoverPaths(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch book covers: %w", err)
	}
	for _, p := range bookCovers {
		if p.Valid && p.String != "" {
			usedPaths[filepath.ToSlash(p.String)] = true
		}
	}

	// Series Stats
	seriesCovers, err := s.store.GetReferencedSeriesCoverPaths(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch series covers: %w", err)
	}
	for _, p := range seriesCovers {
		if p != "" {
			usedPaths[filepath.ToSlash(p)] = true
		}
	}

	if err := taskcontrol.Wait(ctx); err != nil {
		return err
	}

	// Walk the directory structure
	var dirsToDelete []string
	var deletedFiles int
	var scannedFiles int

	err = filepath.WalkDir(thumbDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}

		if path == thumbDir {
			return nil
		}

		relPath, err := filepath.Rel(thumbDir, path)
		if err != nil {
			return nil // ignore
		}

		if d.IsDir() {
			dirsToDelete = append(dirsToDelete, path)
			return nil
		}

		scannedFiles++
		if scannedFiles%100 == 0 && progressCb != nil {
			progressCb(deletedFiles, scannedFiles, fmt.Sprintf("已扫描 %d 个文件，删除 %d 个", scannedFiles, deletedFiles))
		}

		slashRelPath := filepath.ToSlash(relPath)
		if !usedPaths[slashRelPath] {
			if removeErr := os.Remove(path); removeErr == nil {
				deletedFiles++
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk thumbnails directory: %w", err)
	}

	// Cleanup empty directories (bottom up since we traverse top down)
	for i := len(dirsToDelete) - 1; i >= 0; i-- {
		_ = os.Remove(dirsToDelete[i]) // os.Remove only deletes empty directories
	}

	if progressCb != nil {
		progressCb(deletedFiles, scannedFiles, fmt.Sprintf("清理完成，共删除 %d 个冗余缩略图", deletedFiles))
	}

	return nil
}
