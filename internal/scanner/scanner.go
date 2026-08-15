// 扫描主体：Scanner 的构造与配置快照、单库与单系列扫描的编排、同库并发拒绝、档位与
// worker 数决策、指标与进度上报。目录遍历、改名重连各在同包的 walk.go 与 rehome.go。

package scanner

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
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
	// dirtyRefreshInterval 是系列读模型的节流刷新间隔。做成字段而不是常量，是为了让
	// 回归测试能在毫秒级触发 ticker 分支——否则「刷新失败保留脏标记、下一次 tick 重试」
	// 这条路径需要一次跑满 10 秒的扫描才能覆盖到。
	dirtyRefreshInterval time.Duration
}

// defaultDirtyRefreshInterval 是 dirtyRefreshInterval 的默认值。
const defaultDirtyRefreshInterval = 10 * time.Second

func NewScanner(store database.Store, cfg *config.Manager) *Scanner {
	s := &Scanner{
		store:                store,
		config:               cfg,
		openArchive:          parser.OpenArchive,
		dirtyRefreshInterval: defaultDirtyRefreshInterval,
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

// libraryScanFormats 取该库生效的归档格式集。
//
// 取不到库行时返回零值（fail-open，等价于全部支持格式）而不是中止扫描：
// 紧随其后的 ListBooksByLibrary 本来就会在真正的库故障时返回错误，这里再 abort 一次
// 只会把一次偶发的 DB 抖动变成「新文件静默不可见」。
func (s *Scanner) libraryScanFormats(ctx context.Context, libraryID int64) config.ScanFormatSet {
	lib, err := s.store.GetLibrary(ctx, libraryID)
	if err != nil {
		slog.Warn("Failed to load library scan formats, falling back to all supported formats",
			"library_id", libraryID, "error", err)
		return config.ScanFormatSet{}
	}
	return config.NewScanFormatSet(lib.ScanFormats)
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
	discoveredArchives atomic.Int64
	skippedArchives    atomic.Int64
	processedArchives  atomic.Int64
	openedArchives     atomic.Int64
	hashedFiles        atomic.Int64
	queuedCovers       atomic.Int64
	generatedCovers    atomic.Int64
	failedArchives     atomic.Int64
	// rehomedBooks 统计本次扫描把多少条已入库记录按改名重连到了新路径。
	// 这是会改动用户数据的行为，必须可观测——否则「书怎么没变多」只能靠翻日志猜。
	rehomedBooks atomic.Int64
	// staleSeriesStats 记录扫描收尾时仍未刷新成功的系列数：这些系列的统计会一直不准，
	// 直到它们再次被扫描到。是「静默不一致」变可见的唯一途径。
	staleSeriesStats atomic.Int64
	// formatFilteredArchives 统计「本可入库但被库级 scan_formats 排除」的文件数。
	// 格式过滤是用户看不见的静默少扫，不给数字的话「我的书怎么少了 800 本」只能靠翻配置猜。
	formatFilteredArchives atomic.Int64
	ioWaitMillis           atomic.Int64
	pausedMillis           atomic.Int64
	thumbnailWriteMillis   atomic.Int64
}

type scanMetricsSnapshot struct {
	discoveredArchives     int64
	skippedArchives        int64
	processedArchives      int64
	openedArchives         int64
	hashedFiles            int64
	queuedCovers           int64
	generatedCovers        int64
	failedArchives         int64
	rehomedBooks           int64
	staleSeriesStats       int64
	formatFilteredArchives int64
	ioWaitMillis           int64
	pausedMillis           int64
	thumbnailWriteMillis   int64
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
	RehomedBooks           int64
	StaleSeriesStats       int64
	FormatFilteredArchives int64
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
			"discovered_archives":      snapshot.discoveredArchives,
			"skipped_archives":         snapshot.skippedArchives,
			"processed_archives":       snapshot.processedArchives,
			"opened_archives":          snapshot.openedArchives,
			"hashed_files":             snapshot.hashedFiles,
			"queued_covers":            snapshot.queuedCovers,
			"generated_covers":         snapshot.generatedCovers,
			"failed_archives":          snapshot.failedArchives,
			"rehomed_books":            snapshot.rehomedBooks,
			"stale_series_stats":       snapshot.staleSeriesStats,
			"format_filtered_archives": snapshot.formatFilteredArchives,
			"io_wait_ms":               snapshot.ioWaitMillis,
			"paused_ms":                snapshot.pausedMillis,
			"thumbnail_write_ms":       snapshot.thumbnailWriteMillis,
		},
	})
}

func (m *scanMetrics) snapshot() scanMetricsSnapshot {
	if m == nil {
		return scanMetricsSnapshot{}
	}
	return scanMetricsSnapshot{
		discoveredArchives:     m.discoveredArchives.Load(),
		skippedArchives:        m.skippedArchives.Load(),
		processedArchives:      m.processedArchives.Load(),
		openedArchives:         m.openedArchives.Load(),
		hashedFiles:            m.hashedFiles.Load(),
		queuedCovers:           m.queuedCovers.Load(),
		generatedCovers:        m.generatedCovers.Load(),
		failedArchives:         m.failedArchives.Load(),
		rehomedBooks:           m.rehomedBooks.Load(),
		staleSeriesStats:       m.staleSeriesStats.Load(),
		formatFilteredArchives: m.formatFilteredArchives.Load(),
		ioWaitMillis:           m.ioWaitMillis.Load(),
		pausedMillis:           m.pausedMillis.Load(),
		thumbnailWriteMillis:   m.thumbnailWriteMillis.Load(),
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
	// rehome 非 nil 表示这个文件被判定为某条已入库记录的改名/移动：入库时先把那条记录
	// 改挂到新路径（保留 id 从而保留阅读进度与所有 CASCADE 关联），再走正常的 upsert。
	rehome *bookRehome
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

// ErrScanAlreadyRunning 表示同一资料库/系列上已有扫描在跑，本次调用被跳过。
//
// 调用方必须显式判定此错误：等待重试，或让任务以失败收尾并提示「该库正被其它扫描占用」。
// 把它当成功处理的代价——任务面板会在零点几秒内谎报「扫描完成」；更糟的是重建缩略图任务
// 已经 RemoveAll 了整个缩略图目录并清空 cover_path，却把被跳过的库当作成功，而增量扫描只比对
// mtime+size、不检查封面是否缺失，那批封面从此不会自愈，必须人工再跑一次 force 扫描。
var ErrScanAlreadyRunning = errors.New("scanner: a scan is already running for this target")

// ScanLibrary 递归扫描库目录查找漫画包，采用“发现文件 -> 解析归档 -> 批量入库”的三阶段流水线。
// 业务上它需要同时保证增量扫描够快、强制修复能重建封面和索引、任务进度能实时反馈给前端。
// LibraryScanOptions 是整库扫描的可选行为。
type LibraryScanOptions struct {
	// Force 跳过 mtime+size 的增量拦截，重读每个归档。
	Force bool
	// IgnoreFormatFilter 让本次扫描无视库的 scan_formats。
	//
	// 只有「重建缩略图」这类**对已入库内容的维护**才该置位：它会先删光缩略图文件、
	// 清空所有 cover_path，再靠一次强制扫描重建。若这次扫描仍按 scan_formats 过滤，
	// 被排除格式的书就再也不会被访问到——缩略图已删、cover_path 已清、且永不重生。
	// 格式过滤的语义是「导入哪些文件」，不是「已入库的书哪些还算数」。
	IgnoreFormatFilter bool
}

// ScanLibrary 按默认选项扫描整库（尊重库的 scan_formats）。
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID int64, rootPath string, force bool) error {
	return s.ScanLibraryWithOptions(ctx, libraryID, rootPath, LibraryScanOptions{Force: force})
}

func (s *Scanner) ScanLibraryWithOptions(ctx context.Context, libraryID int64, rootPath string, scanOpts LibraryScanOptions) error {
	force := scanOpts.Force
	if !s.beginLibraryScan(libraryID) {
		slog.Info("Library scan skipped because another scan is already running", "library_id", libraryID)
		return ErrScanAlreadyRunning
	}
	defer s.endLibraryScan(libraryID)

	opts := s.scanOptions(force)
	started := time.Now()
	metrics := &scanMetrics{}
	progress := newScanProgressReporter("library", libraryID, metrics, s.onScanProgress)
	progress.publish("loading_existing_books", "", true)

	// 库级格式过滤是**发现阶段**的过滤，只决定导入哪些文件；已入库的书不受影响——
	// CleanupLibrary 只按「文件是否还在磁盘上」删行，与格式无关。
	formats := config.ScanFormatSet{} // 零值 = 全部支持格式
	if !scanOpts.IgnoreFormatFilter {
		formats = s.libraryScanFormats(ctx, libraryID)
	}

	// 增量扫描先加载已入库文件的修改时间和大小，未变化的归档可以跳过重读，降低大库重复扫描成本。
	// 同一份清单还用于构建改名重连索引——强制扫描不吃增量跳过，但一样需要识别改名，
	// 因此这条查询无论 opts.Force 与否都执行（每次扫描一条查询，相对随后要读的 N 个归档可忽略）。
	bookCache := make(map[string]bookScanSnapshot)

	existingBooks, err := s.store.ListBooksByLibrary(ctx, libraryID)
	if err != nil {
		slog.Warn("Failed to load existing books cache", "library_id", libraryID, "error", err)
		return err
	}
	if !opts.Force {
		for _, b := range existingBooks {
			bookCache[b.Path] = bookScanSnapshot{modTime: b.FileModifiedAt, size: b.Size, pageCount: b.PageCount, coverPath: b.CoverPath}
		}
	}
	renames := newRenameIndex(rehomeCandidatesFromLibraryRows(existingBooks))

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
		s.ingestResults(ctx, libraryID, results, metrics, progress, renames)
	}()

	// 第 1 阶段：文件发现。
	// WalkDir 只负责识别候选漫画归档并投递任务，不打开归档内容，确保发现阶段可以快速响应暂停和取消。
	var walkErr error
	progress.publish("discovering", rootPath, true)
	walkErr = walkDirFollowingSymlinks(rootPath, func(path string, d fs.DirEntry, err error) error {
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
		if config.IsSupportedArchiveExtension(ext) && !formats.Matches(path) {
			// 本可入库、但被用户的 scan_formats 排除。只统计这一类，
			// 别把 .txt/.jpg 之类的噪音也算进去，否则这个数字没有诊断价值。
			metrics.formatFilteredArchives.Add(1)
			return nil
		}
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
		return ErrScanAlreadyRunning
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
	// 与 ScanLibrary 同口径的格式过滤；library 行在上面已经取过，零额外查询。
	// 系列扫描与库扫描必须同口径，否则「单系列重扫」会把库扫描刚过滤掉的文件重新灌进来。
	formats := config.NewScanFormatSet(library.ScanFormats)

	bookCache := make(map[string]bookScanSnapshot)
	// 与 ScanLibrary 同理：这份清单同时供增量跳过与改名重连使用，强制扫描也需要后者。
	var renames *renameIndex
	if existingBooks, err := s.store.ListBooksBySeries(ctx, seriesID); err == nil {
		if !opts.Force {
			for _, b := range existingBooks {
				bookCache[b.Path] = bookScanSnapshot{modTime: b.FileModifiedAt, size: b.Size, pageCount: b.PageCount, coverPath: b.CoverPath}
			}
		}
		renames = newRenameIndex(rehomeCandidatesFromBooks(existingBooks))
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
		s.ingestResults(ctx, series.LibraryID, results, metrics, progress, renames)
	}()

	var walkErr error
	progress.publish("discovering", series.Path, true)
	walkErr = walkDirFollowingSymlinks(series.Path, func(path string, d fs.DirEntry, err error) error {
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
		if config.IsSupportedArchiveExtension(ext) && !formats.Matches(path) {
			// 本可入库、但被用户的 scan_formats 排除。只统计这一类，
			// 别把 .txt/.jpg 之类的噪音也算进去，否则这个数字没有诊断价值。
			metrics.formatFilteredArchives.Add(1)
			return nil
		}
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

	seriesList, err := s.store.ListSeriesByLibraryLite(ctx, libraryID)
	if err != nil {
		return fmt.Errorf("failed to list series: %w", err)
	}

	// 第一遍：仅收集“确证缺失”（os.IsNotExist）的系列；权限、超时、网络等不确定错误
	// 一律跳过而非删除，避免瞬时 IO 故障被误判为文件消失。
	//
	// 注意 series.Path 并不总对应磁盘上真实存在的目录：库根目录直放的散装归档
	// （<root>/OnePiece.cbz）会被 workerProcess 归到一个合成路径 <root>/OnePiece 下，
	// 该目录从来就不存在。只按目录是否存在判定，会把这类“虚拟系列”连同其书籍与
	// 每用户阅读进度一起 CASCADE 删掉，且每次扫描重建、下次清理再删，进度反复丢失。
	// 因此改为二次确认：目录不存在时再看它的书还在不在磁盘上，只要还有一本在就不删。
	//
	// 三处 ctx 早退（本循环 / 删系列 / 删书）都是安全的：取消只会造成**少删**。
	// seriesHasSurvivingBook 出错时返回 true（保守留下），missingSeries 只会被低估，
	// DeleteSeries/DeleteBook 拿到已取消的 ctx 直接失败，所以半途停下不会级联删除、
	// 更不会丢阅读进度；剩下的幽灵记录留到下一次清理。
	var missingSeries []database.Series
	for _, series := range seriesList {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, statErr := os.Stat(series.Path); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			slog.Warn("Skipping series with ambiguous stat error during cleanup",
				"series_id", series.ID, "path", series.Path, "error", statErr)
			continue
		}
		if s.seriesHasSurvivingBook(ctx, series.ID) {
			slog.Debug("Series directory missing but books still on disk; treating as virtual series",
				"series_id", series.ID, "path", series.Path)
			continue
		}
		missingSeries = append(missingSeries, series)
	}

	// 熔断：待删系列占比过高，极可能是存储异常而非真实删除。
	if total := len(seriesList); total > 0 && float64(len(missingSeries))/float64(total) > cleanupDeleteRatioGuard {
		return fmt.Errorf("cleanup aborted: %d/%d series appear missing (> %.0f%%), likely a storage issue rather than real deletions",
			len(missingSeries), total, cleanupDeleteRatioGuard*100)
	}

	removedSeries := make(map[int64]bool, len(missingSeries))
	for _, series := range missingSeries {
		if err := ctx.Err(); err != nil {
			return err
		}
		slog.Info("Removing missing series", "series_id", series.ID, "path", series.Path)
		if err := s.store.DeleteSeries(ctx, series.ID); err != nil {
			slog.Error("Failed to delete series", "series_id", series.ID, "error", err)
			continue
		}
		removedSeries[series.ID] = true
	}

	// 存活的系列再逐卷清理缺失书籍（同样只在确证缺失时删除）。
	for _, series := range seriesList {
		if err := ctx.Err(); err != nil {
			return err
		}
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

// seriesHasSurvivingBook 报告该系列名下是否还有至少一本书的文件真实存在于磁盘。
// 用于在删除“目录已不存在”的系列之前做二次确认：库根散装文件构成的虚拟系列没有对应目录，
// 但它们的书是真实存在的，不能因为目录探测失败就整串删掉。
// 查询失败时返回 true（fail-safe：宁可留下一个幽灵记录，也不要误删阅读进度）。
func (s *Scanner) seriesHasSurvivingBook(ctx context.Context, seriesID int64) bool {
	books, err := s.store.ListBooksBySeries(ctx, seriesID)
	if err != nil {
		slog.Warn("Failed to list books while confirming series removal; keeping series",
			"series_id", seriesID, "error", err)
		return true
	}
	for _, book := range books {
		if _, statErr := os.Stat(book.Path); statErr == nil {
			return true
		} else if !os.IsNotExist(statErr) {
			// 权限/超时等不确定错误同样按“可能还在”处理，与上方系列级判定口径一致。
			return true
		}
	}
	return false
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
		"rehomed_books", snapshot.rehomedBooks,
		"stale_series_stats", snapshot.staleSeriesStats,
		"format_filtered_archives", snapshot.formatFilteredArchives,
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
		RehomedBooks:           snapshot.rehomedBooks,
		StaleSeriesStats:       snapshot.staleSeriesStats,
		FormatFilteredArchives: snapshot.formatFilteredArchives,
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
		fileHash, err = koreader.FingerprintFileContext(ctx, job.path)
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

// ingestResults 是唯一的写入协程。renames 为本次扫描的改名重连索引（可为 nil，表示不做重连）：
// 认领在这里而不是解析 worker 里做，因为「一条旧记录只能被认领一次」在单写入方下天然成立。
func (s *Scanner) ingestResults(ctx context.Context, libIDInt int64, results <-chan scanResult, metrics *scanMetrics, progress *scanProgressReporter, renames *renameIndex) {
	// 系列缓存：路径 -> 原系列对象 (保留原属性能防止 Upsert 被 NULL 覆盖)
	seriesCache := make(map[string]database.Series)
	// 锁定字段缓存：ID -> 锁定字段列表 (用 map 提高查找速度)
	lockedFieldsCache := make(map[int64]map[string]bool)

	// 预加载已有的 Series
	existingSeries, _ := s.store.ListSeriesByLibraryLite(ctx, libIDInt)
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

	// dirtySeries 累积整个扫描过程中被触及、待刷新读模型的系列。刷新按 10s ticker 节流 + 扫描末尾兜底：
	// 不能改成每批都对每个 touched 系列全量 UpdateSeriesStatistics + RefreshSeriesStats——那样跨多批的大
	// 系列每批都要重扫它已入库的全部书，退化成 O(K²/batch)。节流同时保留扫描中每 ~10s 的增量 UX。
	dirtySeries := make(map[int64]bool)

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
					seriesCache[res.seriesPath] = database.Series{ID: seriesID, Path: res.seriesPath}
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

				// 改名重连：先把那条「文件已消失」的旧记录改挂到新路径上，紧接着的 UpsertBookByPath
				// 就会经 ON CONFLICT(path) 命中同一行——id 不变，user_book_progress / 书签 / 合集归属
				// 因而全部保留。若更新落空（并发扫描已把该行改走），退回按新书插入即可。
				if res.rehome != nil {
					affected, err := q.RehomeBookPath(ctx, database.RehomeBookPathParams{
						Path:   res.book.Path,
						ID:     res.rehome.bookID,
						Path_2: res.rehome.oldPath,
					})
					switch {
					case err != nil:
						slog.Warn("Failed to rehome renamed book", "book_id", res.rehome.bookID,
							"old_path", res.rehome.oldPath, "new_path", res.book.Path, "error", err)
					case affected == 0:
						slog.Info("Skipped rehoming renamed book because the row moved concurrently",
							"book_id", res.rehome.bookID, "old_path", res.rehome.oldPath)
					default:
						slog.Info("Rehomed renamed book", "book_id", res.rehome.bookID,
							"old_path", res.rehome.oldPath, "new_path", res.book.Path, "match", res.rehome.reason)
						metrics.rehomedBooks.Add(1)
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
			// 读模型刷新不放在批事务内：touched 系列累积到 dirtySeries，交由 refreshDirtySeries 在 10s ticker
			// 与扫描末尾节流刷新。放进事务里逐系列全量重算会让大系列被每批重扫成 O(K²)。
			return nil
		})

		if err != nil {
			// 整批写事务失败会丢弃最多 batchSize 本书。丢弃数必须计入 failedArchives，
			// 否则任务会静默报成功，扫描完成日志与指标里看不出任何异常。
			slog.Error("Batch ingest transaction failed, dropping batch", "book_count", len(batch), "error", err)
			metrics.failedArchives.Add(int64(len(batch)))
		} else {
			slog.Info("Successfully ingested batch", "book_count", len(batch))
			// 累积本批 touched 系列，待 refreshDirtySeries 节流刷新（不在批事务内逐系列全量重算）。
			for sid := range updatedSeriesIDs {
				dirtySeries[sid] = true
			}
			if s.onBatchIngested != nil {
				s.onBatchIngested("batch_inserted")
			}
			progress.publish("queueing_covers", "", true)
			s.enqueueCoverJobs(ctx, coverJobs)
		}

		batch = batch[:0]
	}

	// refreshDirtySeries 节流刷新累积的 touched 系列读模型（series 冗余统计列 + series_stats +
	// 每用户聚合，见 RefreshSeriesDerivedData）。由 10s ticker 与扫描末尾调用，使任一系列在两次刷新
	// 之间至少间隔一个 tick。
	//
	// **刷新失败时保留脏标记**，不能改成无论成败都 delete：扫描被取消或 DB 瞬时出错时，
	// 那些系列的 book_count/total_pages 与读模型会永久停在旧值——没有任何后台自愈路径
	// （启动回填被 user_version 门控，api 侧也没有重算系列统计的维护任务），
	// 只有该系列今后再次发生文件变动、或用户手动强制扫描，才会被纠正回来。
	//
	// warnedSeries 让同一个系列只在首次失败时告警：长扫描下每 10s 刷一屏重复告警会淹没真正的信息。
	warnedSeries := make(map[int64]bool)
	refreshDirtySeries := func(refreshCtx context.Context) {
		if len(dirtySeries) == 0 {
			return
		}
		for sid := range dirtySeries {
			if err := s.store.RefreshSeriesDerivedData(refreshCtx, sid); err != nil {
				if !warnedSeries[sid] {
					warnedSeries[sid] = true
					slog.Warn("Failed to refresh series derived data, keeping it dirty for a later retry",
						"series_id", sid, "err", err)
				}
				continue
			}
			delete(dirtySeries, sid)
			delete(warnedSeries, sid)
		}
	}

	// drainDirtySeries 是扫描收尾时的最后一次刷新。它刻意用 WithoutCancel 派生的 ctx：
	// 扫描被取消时，ctx 已经 Done，用它去刷新只会立刻失败，把整批脏标记原样留下——
	// 而这之后再没有人会来刷它们了。取消要停的是「继续扫描」，不是「把已经写进去的东西算对」。
	//
	// 每个系列单独给 1 秒超时、总预算 5 秒：单个系列被写锁卡住时不至于吃光全部预算，
	// 其余系列仍有机会刷成功。5s 明显小于 SQLite 的 busy_timeout，重度写竞争下会走告警分支
	// 并保留脏标记——这是刻意的取舍，不能让停机被一个卡住的刷新无限期拖住。
	drainDirtySeries := func() {
		if len(dirtySeries) == 0 {
			return
		}
		drainCtx, cancelDrain := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelDrain()
		for sid := range dirtySeries {
			perSeries, cancelOne := context.WithTimeout(drainCtx, time.Second)
			err := s.store.RefreshSeriesDerivedData(perSeries, sid)
			cancelOne()
			if err != nil {
				continue
			}
			delete(dirtySeries, sid)
		}
		if remaining := len(dirtySeries); remaining > 0 {
			// 这些系列的统计会一直不准，直到它们再次被扫描到。必须让它可见。
			metrics.staleSeriesStats.Add(int64(remaining))
			slog.Error("Scan finished with stale series statistics",
				"stale_series", remaining,
				"hint", "these series keep outdated book_count/total_pages until they are scanned again")
		}
	}

	refreshInterval := s.dirtyRefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = defaultDirtyRefreshInterval
	}
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case res, ok := <-results:
			if !ok {
				flush()            // 通道被收尾，最后一次刷盘
				drainDirtySeries() // 扫描结束：兜底刷新剩余 touched 系列（取消时也要刷，见其注释）
				if s.onBatchIngested != nil {
					s.onBatchIngested("scan_completed")
				}
				return
			}
			// 改名认领必须在事务外完成：它要对旧路径做一次 os.Stat。
			res.rehome = renames.claim(rehomeRequest{
				path:      res.book.Path,
				size:      res.book.Size,
				modTime:   res.book.FileModifiedAt,
				quickHash: res.quickHash,
				fileHash:  res.fileHash,
			})
			batch = append(batch, res)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()                 // 按时间自然聚合，避免低频挂起锁
			refreshDirtySeries(ctx) // 每 ~10s 节流刷新一次 touched 系列（取代每批全量重算）
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
	// 只刷新封面那两列，而不是跑完整的 RefreshSeriesStats。
	//
	// 后者含 9 个相关子查询（已读页数求和、已读/读完计数、最近阅读、标签与作者串……），
	// 其中多个是对该系列全部书的聚合。而封面 worker 是**每生成一张封面就调一次**：
	// 一个 K 卷系列首扫就是 K 次全系列聚合，退化成 O(K²) 行扫描 + K 次独立写事务。
	// 这里真正变了的只有 books.cover_path 一列，其余统计与它无关。
	//
	// 其余统计由入库侧的 dirtySeries 节流刷新负责（见 ingestResults），职责不重叠。
	if err := s.store.RefreshSeriesCover(ctx, job.seriesID); err != nil {
		slog.Warn("Failed to refresh series cover after queued thumbnail", "series_id", job.seriesID, "error", err)
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

// SetBookCoverFromPage 用书内指定页(1-based)重建封面并无条件更新 cover_path，随后刷新系列统计。
// GetPages() 已按自然阅读顺序排序，故第 N 页即 pages[N-1]。返回新的相对封面路径。
func (s *Scanner) SetBookCoverFromPage(ctx context.Context, book database.Book, pageNumber int) (string, error) {
	arc, err := s.openArchive(book.Path)
	if err != nil {
		return "", err
	}
	defer arc.Close()
	pages, err := arc.GetPages()
	if err != nil {
		return "", err
	}
	if pageNumber < 1 || pageNumber > len(pages) {
		return "", fmt.Errorf("page %d out of range (1..%d)", pageNumber, len(pages))
	}
	page := pages[pageNumber-1]
	data, err := arc.ReadPage(page.Name)
	if err != nil {
		return "", err
	}
	return s.applyCustomCover(ctx, book, data, page.MediaType)
}

// SetBookCoverFromImage 用外部上传的图片字节重建封面（上传封面）。
func (s *Scanner) SetBookCoverFromImage(ctx context.Context, book database.Book, imageData []byte, mediaType string) (string, error) {
	return s.applyCustomCover(ctx, book, imageData, mediaType)
}

// applyCustomCover 把给定图片处理成内容寻址的缩略图写盘，无条件更新 cover_path 并刷新系列统计。
func (s *Scanner) applyCustomCover(ctx context.Context, book database.Book, imageData []byte, mediaType string) (string, error) {
	cfg := s.currentConfig()
	relPath, err := s.writeCoverThumbnail(cfg, imageData, mediaType)
	if err != nil {
		return "", err
	}
	if err := s.store.SetBookCover(ctx, book.ID, relPath); err != nil {
		removeGeneratedThumbnail(cfg, relPath)
		return "", err
	}
	if err := s.store.RefreshSeriesStats(ctx, book.SeriesID); err != nil {
		slog.Warn("refresh series stats after custom cover failed", "series_id", book.SeriesID, "error", err)
	}
	return relPath, nil
}

// writeCoverThumbnail 把原始图片处理成 400px 缩略图，用内容 SHA1 命名（与扫描封面同一目录方案），
// 内容寻址天然去重且能刷新浏览器缓存。返回相对路径（<2字符子目录>/<hash>.<ext>）。
func (s *Scanner) writeCoverThumbnail(cfg config.Config, imageData []byte, mediaType string) (string, error) {
	targetFormat := cfg.Scanner.ThumbnailFormat
	if targetFormat == "" {
		targetFormat = "webp"
	}
	processed, contentType, err := images.ProcessImage(imageData, mediaType, images.ProcessOptions{Width: 400, Quality: 82, Format: targetFormat})
	if err != nil || len(processed) == 0 {
		processed, contentType, err = images.ProcessImage(imageData, mediaType, images.ProcessOptions{Width: 400, Quality: 82, Format: "jpeg"})
		if err != nil {
			return "", err
		}
	}
	if len(processed) == 0 {
		return "", fmt.Errorf("no processed cover data generated")
	}
	sum := sha1.Sum(processed)
	hash := hex.EncodeToString(sum[:])
	subDir := thumbnailSubDir(hash)
	thumbDir := filepath.Join(thumbnailBaseDir(cfg), subDir)
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return "", err
	}
	fileName := hash + extensionFromContentType(contentType, targetFormat)
	if err := os.WriteFile(filepath.Join(thumbDir, fileName), processed, 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(subDir, fileName)), nil
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
//
// progressCb 只收计数：展示文案是调用方的事，扫描器不渲染用户可见文字。
func (s *Scanner) CleanupThumbnails(ctx context.Context, progressCb func(deleted, scanned int)) error {
	cfg := s.currentConfig()
	thumbDir := thumbnailBaseDir(cfg)

	// 流式收集被引用的封面路径，不要换回 :many 查询把整库路径先读进切片再折进 map：
	// 那两份切片纯属中转（10 万本书要多分配一遍字符串与底层数组），DISTINCT 的去重也是
	// 白付的——map 本来就去重。
	//
	// 注意 taskcontrol.Wait 必须放在遍历**之后**：回调期间数据库游标是开着的，
	// 在里面等待暂停闸会把一个连接和一个 WAL 读快照一起挂住。
	usedPaths := make(map[string]bool)
	if err := s.store.ForEachReferencedCoverPath(ctx, func(path string) error {
		usedPaths[path] = true
		return nil
	}); err != nil {
		return fmt.Errorf("failed to fetch referenced cover paths: %w", err)
	}

	if err := taskcontrol.Wait(ctx); err != nil {
		return err
	}

	// Walk the directory structure
	var dirsToDelete []string
	var deletedFiles int
	var scannedFiles int

	err := filepath.WalkDir(thumbDir, func(path string, d os.DirEntry, err error) error {
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
			progressCb(deletedFiles, scannedFiles)
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
		progressCb(deletedFiles, scannedFiles)
	}

	return nil
}
