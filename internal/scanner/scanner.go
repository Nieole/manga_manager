package scanner

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"fmt"

	"log/slog"

	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"manga-manager/internal/bookcontent"

	"manga-manager/internal/config"
	"manga-manager/internal/database"
	"manga-manager/internal/diskwork"
	"manga-manager/internal/images"

	"manga-manager/internal/parser"

	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

type Scanner struct {
	store  database.Store
	config *config.Manager
	// diskWork 执行扫描器的**磁盘作业**：闸门、策略、上限、令牌与观测都收在它里面。
	diskWork      *diskwork.Runner
	openArchive   func(string) (parser.Archive, error)
	contentReader *bookcontent.ArchiveReader
	coverOnce     sync.Once
	coverMu       sync.RWMutex
	coverClosed   bool
	coverWorkers  sync.WaitGroup
	coverQueue    chan coverJob
	// coverRuns 是**封面运行**的发起口，装配期注册（见 SetCoverRunLauncher）。
	// 留 nil 的进程里封面照样生成，只是不建运行、无处可报。
	coverRuns CoverRunLauncher
	// selfDrained 数的是没有发起方接手、由扫描器自己跑完的那些批，见 waitForCoverQueue。
	selfDrained sync.WaitGroup
	mu          sync.Mutex
	// scanningLibraries 是**正在扫描中**的资料库计数（库 id -> 在飞的整库扫描数）。
	//
	// 它不是准入闸门——「同一件事不会同时跑两遍」由任务那一处的准入回答（ADR 0005）。
	// 它只回答文件监听器那个问题：这个库此刻有没有扫描在跑。改名重连写在扫描**末尾**，
	// 扫描没结束就等于重连没落地，此刻清理会把旧行当成「文件已消失」删掉。
	// 计数而不是集合：同一个库上可能有两次扫描同时在跑（重建缩略图的逐库强扫挂在系统作用域上，
	// 与库扫描是两个身份，任务那一处的准入不会拦它们）。
	scanningLibraries map[int64]int
	// 批量插入结束后的回调播送机制。
	//
	// 它刻意仍是装配期注册的进程级回调，与**扫描观察者**分属两件事：这几条事件从入库批次
	// 和封面 worker 两处发出，消费方是缓存失效与 SSE 广播，与「哪一次扫描」无关。
	onBatchIngested func(action string)
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
	s.contentReader = bookcontent.NewArchiveReader(func(path string) (parser.Archive, func(), error) {
		arc, err := s.openArchive(path)
		if err != nil {
			return nil, func() {}, err
		}
		return arc, func() { _ = arc.Close() }, nil
	})
	s.scanningLibraries = make(map[int64]int)
	s.diskWork = diskwork.NewRunner(s.currentConfig, storageio.Default)
	return s
}

// SetBatchCallback 允许外部注册事件通知钩子
func (s *Scanner) SetBatchCallback(cb func(string)) {
	s.onBatchIngested = cb
}

// ScanObserver 是一次扫描把**计数推进**与收尾指标交给的对象，由发起方在调用扫描时交出。
//
// 「这份报文属于谁」因此由交出者回答，报文自己不带身份。传 nil 表示这次扫描的进度与指标无处可报
// （调用方自己另有进度口径，例如重建索引那趟逐库强扫），此时两个方法都不会被调用。
//
// 它只管扫描本身，寿命与那次调用一致：异步生成的封面归**封面运行**，报文经封面作业自带的
// CoverObserver 走（ADR 0005）。实现方仍须自行保证并发安全——四个方法会被多个扫描 worker
// 并发调用。
type ScanObserver interface {
	// Progress 报告一次扫描进行中的计数、**阶段**与当前条目。
	Progress(ScanProgressReport)
	// Metrics 报告一次扫描收尾时的全量指标，每次扫描恰好一次。
	Metrics(ScanMetricsReport)
	// ItemFailed 报告一个条目没处理成：哪个文件、为什么。它答的是详情页那个
	// 「`failed_archives: 7` 到底是哪 7 个文件」——只有计数的话，那个数没有任何去处可查。
	ItemFailed(ItemFailure)
	// Warn 报告一次运行级告警：整批被丢掉、某项能力降级、某道护栏拦下了后续的东西。
	// 它与 ItemFailed 的分界是「说的是某一个条目，还是整次扫描」。
	Warn(ScanWarning)
}

// ItemFailure 是一个条目没处理成：哪个文件、为什么。扫描与封面两条通道共用它。
//
// Reason 是给人看的一句话（多半是底层错误串），不是错误码：这一条落进**运行事件**之后
// 原样显示在详情页上，翻译它既没有稳定的取值集合，也会把真正的线索译丢。
type ItemFailure struct {
	Path   string
	Reason string
}

// ScanWarning 是一次运行级告警。Code 是一个稳定的短码（由渲染方翻译），
// Detail 是给排查用的补充说明，Count 是「这一条涉及多少个条目」，零表示不带计数。
type ScanWarning struct {
	Code   string
	Detail string
	Count  int64
}

// 扫描发出的运行级告警码。取值稳定：它们落进用户库里的历史事件行，改名等于把旧行读成一句空白。
const (
	// WarnBatchIngestDropped 是一整批入库事务失败、这一批书被丢掉。
	WarnBatchIngestDropped = "batch_ingest_dropped"
)

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
		slog.WarnContext(ctx, "Failed to load library scan formats, falling back to all supported formats",
			"library_id", libraryID, "error", err)
		return config.ScanFormatSet{}
	}
	return config.NewScanFormatSet(lib.ScanFormats)
}

// enterLibraryScan / leaveLibraryScan 记一次整库扫描的进出。它们**不拒绝**任何调用：
// 记账用，不是闸门。
func (s *Scanner) enterLibraryScan(libraryID int64) {
	s.mu.Lock()
	s.scanningLibraries[libraryID]++
	s.mu.Unlock()
}

func (s *Scanner) leaveLibraryScan(libraryID int64) {
	s.mu.Lock()
	if s.scanningLibraries[libraryID] <= 1 {
		delete(s.scanningLibraries, libraryID)
	} else {
		s.scanningLibraries[libraryID]--
	}
	s.mu.Unlock()
}

// LibraryScanActive 报告该库此刻是否有整库扫描在跑，**不区分发起方**。
//
// 文件监听器用它决定敢不敢清理：任何一次扫描的改名重连都写在末尾，扫描没结束就等于重连没落地。
// 它看得见的是**真的进到扫描里**的那些，含不经任务发起的（重建缩略图与重建索引的逐库强扫）。
func (s *Scanner) LibraryScanActive(libraryID int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scanningLibraries[libraryID] > 0
}

type archiveCandidate struct {
	path string
	info os.FileInfo
}

type scanMetrics struct {
	discoveredArchives atomic.Int64
	skippedArchives    atomic.Int64
	processedArchives  atomic.Int64
	openedArchives     atomic.Int64
	hashedFiles        atomic.Int64
	queuedCovers       atomic.Int64
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
}

// absorbDiskWork 把一次**磁盘作业**的等待与暂停耗时折进扫描指标。
// 接收者为 nil 时直接返回，于是取用点上不必再守一次——不属于任何任务的扫描没有指标。
func (m *scanMetrics) absorbDiskWork(stats diskwork.Stats) {
	if m == nil {
		return
	}
	if stats.Wait > 0 {
		m.ioWaitMillis.Add(stats.Wait.Milliseconds())
	}
	if stats.PausedWait > 0 {
		m.pausedMillis.Add(stats.PausedWait.Milliseconds())
	}
}

type scanMetricsSnapshot struct {
	discoveredArchives     int64
	skippedArchives        int64
	processedArchives      int64
	openedArchives         int64
	hashedFiles            int64
	queuedCovers           int64
	failedArchives         int64
	rehomedBooks           int64
	staleSeriesStats       int64
	formatFilteredArchives int64
	ioWaitMillis           int64
	pausedMillis           int64
}

// ScanMetricsReport 是一次扫描收尾时的全量指标。
//
// 它与 ScanProgressReport 都不带身份：一份报文属于哪次扫描，由收下它的**扫描观察者**回答。
type ScanMetricsReport struct {
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
	FailedArchives         int64
	RehomedBooks           int64
	StaleSeriesStats       int64
	FormatFilteredArchives int64
	IOWaitMillis           int64
	PausedMillis           int64
	DurationMillis         int64
}

type ScanProgressReport struct {
	Phase       string
	CurrentItem string
	Current     int64
	Total       int64
	Metrics     map[string]int64
}

type scanProgressReporter struct {
	metrics  *scanMetrics
	observer ScanObserver
	throttle reportThrottle
}

func newScanProgressReporter(metrics *scanMetrics, observer ScanObserver) *scanProgressReporter {
	return &scanProgressReporter{metrics: metrics, observer: observer, throttle: reportThrottle{interval: scanReportInterval}}
}

// scanReportInterval 是扫描进度的投递水位，与封面推进那一条同一个道理，见 coverReportInterval。
const scanReportInterval = 250 * time.Millisecond

// itemFailed 把一个条目的失败交给**扫描观察者**。它不受投递水位约束：水位管的是同一份展示态
// 别重复投，而每一条失败都是一件**不同**的事，被节流吞掉就再也补不回来。
//
// 条数上限不在这里判：那属于收下它的那一侧（见 task.MaxItemFailureEvents），
// 在这里也判一遍等于让「最多留多少条」有两个答案。
func (r *scanProgressReporter) itemFailed(path, reason string) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.ItemFailed(ItemFailure{Path: path, Reason: reason})
}

// warn 把一次运行级告警交给**扫描观察者**，同样不受投递水位约束。
func (r *scanProgressReporter) warn(warning ScanWarning) {
	if r == nil || r.observer == nil {
		return
	}
	r.observer.Warn(warning)
}

func (r *scanProgressReporter) publish(phase, currentItem string, force bool) {
	if r == nil || r.observer == nil || !r.throttle.allow(force) {
		return
	}

	snapshot := r.metrics.snapshot()
	current := snapshot.skippedArchives + snapshot.processedArchives
	total := snapshot.discoveredArchives
	if phase == "discovering" {
		current = snapshot.discoveredArchives
		total = 0
	}
	r.observer.Progress(ScanProgressReport{
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
			"failed_archives":          snapshot.failedArchives,
			"rehomed_books":            snapshot.rehomedBooks,
			"stale_series_stats":       snapshot.staleSeriesStats,
			"format_filtered_archives": snapshot.formatFilteredArchives,
			"io_wait_ms":               snapshot.ioWaitMillis,
			"paused_ms":                snapshot.pausedMillis,
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
		failedArchives:         m.failedArchives.Load(),
		rehomedBooks:           m.rehomedBooks.Load(),
		staleSeriesStats:       m.staleSeriesStats.Load(),
		formatFilteredArchives: m.formatFilteredArchives.Load(),
		ioWaitMillis:           m.ioWaitMillis.Load(),
		pausedMillis:           m.pausedMillis.Load(),
	}
}

type bookScanSnapshot struct {
	modTime   time.Time
	size      int64
	pageCount int64
	coverPath sql.NullString
}

// unchanged 判定磁盘上的这个文件与快照记下的完全一致。
func (snap bookScanSnapshot) unchanged(info os.FileInfo) bool {
	return snap.modTime.Equal(info.ModTime()) && snap.size == info.Size()
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

// WorkerCount 回答「用这份配置扫这条路径，解析工作池会起几个 worker」，是这个数的唯一出处。
//
// 导出是因为任务面板要报它：面板报的必须**就是**扫描器会起的那个数，因此调本函数而不是照着
// 复述一遍公式。复述的那份在收窄判据改口径时不会跟着变，而两边都不报错、没有用例会红。
func WorkerCount(cfg config.Config, rootPath string, opts ScanOptions) int {
	workers := cfg.Scanner.Workers
	if workers <= 0 {
		workers = runtime.NumCPU() * 2
	}
	policy := config.ResolveStoragePolicy(cfg, rootPath)
	limit := policy.IOPolicy.ScanConcurrency
	if opts.Profile.opensArchive() {
		limit = minPositive(limit, policy.IOPolicy.ArchiveOpenConcurrency)
	}
	if opts.Profile.computesQuickHash() || opts.Profile.computesFullHash(cfg) {
		limit = minPositive(limit, policy.IOPolicy.HashConcurrency)
	}
	if limit > 0 && workers > limit {
		workers = limit
	}
	if workers < 1 {
		workers = 1
	}
	return workers
}

// minPositive 取诸项里最小的正值；全非正时返回 0，即不设上限。
func minPositive(values ...int) int {
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

// backfills 判定这个档位能给这份已入库快照补上它缺的数据——增量跳过的第二个判据。
//
// 「文件没变」只说明重读读不出**新**东西，不说明库里那份已经**齐**了：fast 档位不开归档，
// 它首扫留下的书 page_count 为 0、cover_path 为空，而 mtime+size 与磁盘完全一致，
// 只按文件没变就跳过的话，用户改回 metadata 档跑普通扫描，那本书照样被跳过，
// 页数与封面只有强制扫描补得回来——进度百分比、读完判定、系列已读统计因此一直算不出来。
//
// 判据必须挂在**当前档位的能力**上，而不是「库里缺什么就重读」：fast 档位补不出页数与封面，
// 缺着也该继续跳过，否则它退化成每次全量重读，这个档位随之失去意义。
// 内容哈希不在此列——它由扫描收尾发起的低优先级哈希回填任务负责补。
func (p ScanProfile) backfills(snap bookScanSnapshot) bool {
	if p.opensArchive() && snap.pageCount <= 0 {
		return true
	}
	if p.extractsMetadata() && (!snap.coverPath.Valid || snap.coverPath.String == "") {
		return true
	}
	return false
}

// coverCandidate 是一张待生成封面的来源。pageName 为空表示「这一页由封面构建自己挑」——
// 续跑那条路只从库里读得到书行，读不到归档里的页目录。
type coverCandidate struct {
	path      string
	pageName  string
	mediaType string
	bookHash  string
}

// coverJob 是一本书的一次封面构建。它**自带去处**：batch 说的就是「这张封面属于哪一次
// 封面运行」，报文因此不必回头按身份反查（ADR 0005）。
//
// batch 与 ctx 由派发那一刻（CoverBatch.Drain）补齐，排进批里的时候还是空的——
// 一批要派给哪条运行，在扫描把它排进来时并不知道。
type coverJob struct {
	ctx       context.Context
	batch     *CoverBatch
	bookID    int64
	seriesID  int64
	libraryID int64
	candidate coverCandidate
}

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
//
// observer 为 nil 表示这次扫描的进度与指标无处可报，见 ScanObserver。
func (s *Scanner) ScanLibrary(ctx context.Context, libraryID int64, rootPath string, force bool, observer ScanObserver) error {
	return s.ScanLibraryWithOptions(ctx, libraryID, rootPath, LibraryScanOptions{Force: force}, observer)
}

// ScanLibraryWithOptions 是整库扫描的完整入口。
//
// observer 刻意是位置参数而不是 LibraryScanOptions 的一个字段：它是协作方不是开关，
// 而且结构体字段可以漏填——漏填与「故意不报进度」长得一模一样，都不会有编译错误。
func (s *Scanner) ScanLibraryWithOptions(ctx context.Context, libraryID int64, rootPath string, scanOpts LibraryScanOptions, observer ScanObserver) error {
	force := scanOpts.Force
	s.enterLibraryScan(libraryID)
	defer s.leaveLibraryScan(libraryID)

	opts := s.scanOptions(force)
	started := time.Now()
	metrics := &scanMetrics{}
	progress := newScanProgressReporter(metrics, observer)
	progress.publish("loading_existing_books", "", true)

	// 这一批封面的寿命超出本次调用：扫描收尾只关批，剩下的由那条**封面运行**接着跑。
	covers := s.newCoverBatch(libraryID)
	defer covers.close()

	// 库级格式过滤是**发现阶段**的过滤，只决定导入哪些文件；已入库的书不受影响——
	// CleanupLibrary 只按「文件是否还在磁盘上」删行，与格式无关。
	formats := config.ScanFormatSet{} // 零值 = 全部支持格式
	if !scanOpts.IgnoreFormatFilter {
		formats = s.libraryScanFormats(ctx, libraryID)
	}

	// 先加载已入库文件的快照，它供两件事：跳过既没变、本档位又补不出新数据的归档
	// （只在非强制扫描下，见下面遍历里那处独立守卫），以及构建改名重连索引。
	// 「fast 档位别把页数与封面清零」不在这里——那由入库侧挑哪个 upsert 决定，见 sql/query.sql。
	bookCache := make(map[string]bookScanSnapshot)

	existingBooks, err := s.store.ListBooksByLibrary(ctx, libraryID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to load existing books cache", "library_id", libraryID, "error", err)
		return err
	}
	for _, b := range existingBooks {
		bookCache[b.Path] = bookScanSnapshot{modTime: b.FileModifiedAt, size: b.Size, pageCount: b.PageCount, coverPath: b.CoverPath}
	}
	renames := newRenameIndex(rehomeCandidatesFromLibraryRows(existingBooks))

	walkErr := s.ingest(ctx, ingestionPlan{libraryID: libraryID, libraryRoot: rootPath, scanPath: rootPath, options: opts, formats: formats, existing: bookCache, renames: renames, queueSize: 1000}, metrics, progress, covers)
	s.logScanCompleted(ctx, "library", libraryID, rootPath, opts, metrics, time.Since(started), walkErr, observer)
	progress.publish("completed", "", true)
	return walkErr
}

// ScanSeries 扫描单一系列目录，将新的卷添加到数据库中
func (s *Scanner) ScanSeries(ctx context.Context, seriesID int64, force bool, observer ScanObserver) error {
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
	progress := newScanProgressReporter(metrics, observer)
	progress.publish("loading_existing_books", "", true)
	// 封面按库归属：系列扫描排出来的封面进的也是这个库那条**封面运行**。
	covers := s.newCoverBatch(series.LibraryID)
	defer covers.close()
	// 与 ScanLibrary 同口径的格式过滤；library 行在上面已经取过，零额外查询。
	// 系列扫描与库扫描必须同口径，否则「单系列重扫」会把库扫描刚过滤掉的文件重新灌进来。
	formats := config.NewScanFormatSet(library.ScanFormats)

	bookCache := make(map[string]bookScanSnapshot)
	// 与 ScanLibrary 同口径：这份快照供增量跳过与改名重连两件事。
	var renames *renameIndex
	if existingBooks, err := s.store.ListBooksBySeries(ctx, seriesID); err == nil {
		for _, b := range existingBooks {
			bookCache[b.Path] = bookScanSnapshot{modTime: b.FileModifiedAt, size: b.Size, pageCount: b.PageCount, coverPath: b.CoverPath}
		}
		renames = newRenameIndex(rehomeCandidatesFromBooks(existingBooks))
	}

	walkErr := s.ingest(ctx, ingestionPlan{libraryID: series.LibraryID, libraryRoot: library.Path, scanPath: series.Path, options: opts, formats: formats, existing: bookCache, renames: renames, queueSize: 100}, metrics, progress, covers)
	s.logScanCompleted(ctx, "series", seriesID, library.Path, opts, metrics, time.Since(started), walkErr, observer)
	progress.publish("completed", "", true)
	return walkErr
}

// CleanupLibrary removes proved-missing records only after conservative library checks.
func (s *Scanner) CleanupLibrary(ctx context.Context, libraryID int64) error {
	cleanup := libraryCleanup{store: s.store}
	return cleanup.run(ctx, libraryID)
}

// logScanCompleted 打一条收尾日志，并把全量指标交给本次扫描的**扫描观察者**。
//
// scope 与 id 只进日志属性：报文本身不带身份，观察者知道自己是谁。
func (s *Scanner) logScanCompleted(ctx context.Context, scope string, id int64, rootPath string, opts ScanOptions, metrics *scanMetrics, duration time.Duration, err error, observer ScanObserver) {
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
		"failed_archives", snapshot.failedArchives,
		"rehomed_books", snapshot.rehomedBooks,
		"stale_series_stats", snapshot.staleSeriesStats,
		"format_filtered_archives", snapshot.formatFilteredArchives,
		"io_wait_ms", snapshot.ioWaitMillis,
		"paused_ms", snapshot.pausedMillis,
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
		slog.WarnContext(ctx, "Scan completed with errors", attrs...)
		publishScanMetrics(observer, policy, snapshot, duration)
		return
	}
	slog.InfoContext(ctx, "Scan completed", attrs...)
	publishScanMetrics(observer, policy, snapshot, duration)
}

func publishScanMetrics(observer ScanObserver, policy config.ResolvedStoragePolicy, snapshot scanMetricsSnapshot, duration time.Duration) {
	if observer == nil {
		return
	}
	observer.Metrics(ScanMetricsReport{
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
		FailedArchives:         snapshot.failedArchives,
		RehomedBooks:           snapshot.rehomedBooks,
		StaleSeriesStats:       snapshot.staleSeriesStats,
		FormatFilteredArchives: snapshot.formatFilteredArchives,
		IOWaitMillis:           snapshot.ioWaitMillis,
		PausedMillis:           snapshot.pausedMillis,
		DurationMillis:         duration.Milliseconds(),
	})
}

func thumbnailBaseDir(cfg config.Config) string {
	return config.ThumbnailDir(cfg)
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

// enqueueCoverJobs 把一批封面作业排进这次扫描那一批里，并确保有一条封面运行去跑它。
//
// 排队本身不阻塞、不过闸门：这一批的寿命超出扫描调用，扫描不该被它按住——「扫描完成」
// 从此只说扫描本身（ADR 0005）。派发、暂停与取消都是那条封面运行的事，见 CoverBatch.Drain。
func (s *Scanner) enqueueCoverJobs(ctx context.Context, covers *CoverBatch, metrics *scanMetrics, jobs []coverJob) {
	if covers == nil || len(jobs) == 0 {
		return
	}
	covers.add(jobs)
	if metrics != nil {
		metrics.queuedCovers.Add(int64(len(jobs)))
	}
	covers.attach(ctx)
}

// dispatchCover 把一个封面作业派进共享的 worker 池，并把它记进那一批的在飞数。
//
// 池是共享的：两个库的封面运行会在里面交错推进，这是预期行为——限住并发的是池的大小与
// **存储令牌**，而不是运行的条数。
func (s *Scanner) dispatchCover(ctx context.Context, batch *CoverBatch, job coverJob) error {
	s.coverMu.RLock()
	defer s.coverMu.RUnlock()
	if s.coverClosed {
		return context.Canceled
	}
	s.startCoverWorkers()
	job.ctx = ctx
	job.batch = batch
	if err := batch.reserve(ctx); err != nil {
		return err
	}
	select {
	case s.coverQueue <- job:
		return nil
	case <-ctx.Done():
		batch.release()
		return ctx.Err()
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
			s.coverWorkers.Add(1)
			go func() {
				defer s.coverWorkers.Done()
				for job := range s.coverQueue {
					s.runCoverJob(job)
				}
			}()
		}
	})
}

// runCoverJob 生成一张封面，并把结果记进它自带的那一批。
//
// 每条出口都要经 batch 的三条结算之一：漏一条那一批的剩余量就永远归不了零，等它排空的那条
// 封面运行会一直挂着，而界面上写着「还剩 N 张」。
//
// 这里只看取消，不问**暂停闸门**：闸门在派发前问过了（见 CoverBatch.Drain），
// 而 worker 是共享的——在这里等下去，一条被按下的封面运行会把整个池子占住。
func (s *Scanner) runCoverJob(job coverJob) {
	batch := job.batch
	ctx := job.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defer batch.release()
	if ctx.Err() != nil {
		batch.settleSkipped()
		return
	}

	cfg := s.currentConfig()
	coverPath, err := s.generateBookThumbnail(ctx, job.candidate, cfg, batch)
	if err != nil {
		slog.WarnContext(ctx, "Failed to generate queued thumbnail", "book_id", job.bookID, "path", job.candidate.path, "error", err)
		batch.settleFailed(job.candidate.path, err.Error())
		return
	}
	if !coverPath.Valid || coverPath.String == "" {
		batch.settleFailed(job.candidate.path, "thumbnail generator produced no cover path")
		return
	}

	rowsAffected, err := s.store.SetBookCoverIfMissing(ctx, database.SetBookCoverIfMissingParams{
		CoverPath: coverPath,
		ID:        job.bookID,
	})
	if err != nil {
		removeGeneratedThumbnail(cfg, coverPath.String)
		slog.WarnContext(ctx, "Failed to update queued thumbnail cover path", "book_id", job.bookID, "error", err)
		batch.settleFailed(job.candidate.path, err.Error())
		return
	}
	if rowsAffected == 0 {
		// 这本书在封面生成期间已经有了封面（用户自己设了一张，或另一次扫描抢先写下）：
		// 不是故障，也没有新增一张。
		batch.settleSkipped()
		return
	}
	batch.settleGenerated()
	batch.report(job.candidate.path, false)
	// 只刷新封面那两列，而不是跑完整的 RefreshSeriesStats。
	//
	// 后者含 9 个相关子查询（已读页数求和、已读/读完计数、最近阅读、标签与作者串……），
	// 其中多个是对该系列全部书的聚合。而封面 worker 是**每生成一张封面就调一次**：
	// 一个 K 卷系列首扫就是 K 次全系列聚合，退化成 O(K²) 行扫描 + K 次独立写事务。
	// 这里真正变了的只有 books.cover_path 一列，其余统计与它无关。
	//
	// 其余统计由入库侧的 dirtySeries 节流刷新负责（见 derivedSeries），职责不重叠。
	if err := s.store.RefreshSeriesCover(ctx, job.seriesID); err != nil {
		slog.WarnContext(ctx, "Failed to refresh series cover after queued thumbnail", "series_id", job.seriesID, "error", err)
	}
	if s.onBatchIngested != nil {
		s.onBatchIngested("thumbnail_updated")
	}
}

// slowThumbnailWrite 是缩略图这一趟慢得该被看见的阈值：落盘或等令牌任一超过它就留一条记录。
const slowThumbnailWrite = 250 * time.Millisecond

// generateBookThumbnail 生成一张封面并落盘，交回它的相对路径。这一趟的等待与落盘耗时
// 折进 batch——封面归**封面运行**记账，与发起它的那次扫描无关。
func (s *Scanner) generateBookThumbnail(ctx context.Context, candidate coverCandidate, cfg config.Config, batch *CoverBatch) (sql.NullString, error) {
	if existing := existingThumbnailPath(cfg, candidate.bookHash); existing.Valid {
		return existing, nil
	}

	// 令牌只覆盖归档 IO：紧随其后的图片转码是纯 CPU 的，圈进持有区间只会虚占归档打开的并发额度。
	var pageData []byte
	mediaType := candidate.mediaType
	coverWork := diskwork.Work{Kind: storageio.WorkKindCoverBuild, Path: candidate.path}
	stats, err := s.diskWork.Do(ctx, coverWork, func() error {
		var materialized bookcontent.PageBytes
		var err error
		if candidate.pageName == "" {
			materialized, err = s.contentReader.Page(ctx, candidate.path, 1)
		} else {
			materialized, err = s.contentReader.NamedPage(ctx, candidate.path, parser.PageMetadata{Name: candidate.pageName, MediaType: mediaType})
		}
		if materialized.ArchiveOpened {
			batch.countOpenedArchive()
		}
		pageData, mediaType = materialized.Data, materialized.Page.MediaType
		return err
	})
	batch.absorbDiskWork(stats)
	if err != nil {
		return sql.NullString{}, err
	}

	targetFormat := cfg.Scanner.ThumbnailFormat
	if targetFormat == "" {
		targetFormat = "webp"
	}

	processed, contentType, err := images.ProcessImage(ctx, pageData, mediaType, images.ProcessOptions{
		Width: 400, Quality: 82, Format: targetFormat,
	})
	if err != nil || len(processed) == 0 {
		slog.WarnContext(ctx, "Primary thumbnail format generation failed, falling back to jpeg", "format", targetFormat, "path", candidate.path, "error", err)
		processed, contentType, err = images.ProcessImage(ctx, pageData, mediaType, images.ProcessOptions{
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
	// 缓存目录常和藏书在同一块盘上转，却落不进任何资料库的路径前缀，故把藏书那一侧交给 PolicyFrom。
	var writeDuration time.Duration
	writeWork := diskwork.Work{Kind: storageio.WorkKindCacheWrite, Path: thumbDir, PolicyFrom: candidate.path}
	writeStats, err := s.diskWork.Do(ctx, writeWork, func() error {
		var err error
		writeDuration, err = writeThumbnailFile(thumbDir, fullPath, processed)
		return err
	})
	batch.absorbDiskWork(writeStats)
	batch.countThumbnailWrite(writeDuration)
	if err != nil {
		return sql.NullString{}, err
	}
	if writeDuration >= slowThumbnailWrite || writeStats.Wait >= slowThumbnailWrite {
		slog.InfoContext(ctx, "Queued thumbnail cache write completed",
			"path", candidate.path,
			"thumbnail_path", fullPath,
			"storage_profile", writeStats.StorageProfile,
			"volume_key", writeStats.VolumeKey,
			"io_wait_ms", writeStats.Wait.Milliseconds(),
			"paused_ms", writeStats.PausedWait.Milliseconds(),
			"thumbnail_write_ms", writeDuration.Milliseconds(),
		)
	}
	return sql.NullString{String: filepath.ToSlash(filepath.Join(subDir, fileName)), Valid: true}, nil
}

// SetBookCoverFromPage 用书内指定页(1-based)重建封面并落到 cover_path，随后刷新系列统计。
// GetPages() 已按自然阅读顺序排序，故第 N 页即 pages[N-1]。返回新的相对封面路径。
func (s *Scanner) SetBookCoverFromPage(ctx context.Context, book database.Book, pageNumber int) (string, error) {
	materialized, err := s.contentReader.Page(ctx, book.Path, int64(pageNumber))
	if err != nil {
		return "", err
	}
	return s.applyCustomCover(ctx, book, materialized.Data, materialized.Page.MediaType)
}

// SetBookCoverFromImage 用外部上传的图片字节重建封面（上传封面）。
func (s *Scanner) SetBookCoverFromImage(ctx context.Context, book database.Book, imageData []byte, mediaType string) (string, error) {
	return s.applyCustomCover(ctx, book, imageData, mediaType)
}

// applyCustomCover 把给定图片处理成内容寻址的缩略图写盘，更新 cover_path 并刷新系列统计。
//
// 它经 SetBookCover 一并置上封面锁：自动缩略图按 sha1(path|mtime|size) 命名、这里按图片内容命名，
// 两个名字永不相等，扫描因此认不出「这张是人设的」——认不出就会在下一次强制扫描里改回自动封面。
func (s *Scanner) applyCustomCover(ctx context.Context, book database.Book, imageData []byte, mediaType string) (string, error) {
	cfg := s.currentConfig()
	relPath, err := s.writeCoverThumbnail(ctx, cfg, imageData, mediaType)
	if err != nil {
		return "", err
	}
	if err := s.store.SetBookCover(ctx, book.ID, relPath); err != nil {
		removeGeneratedThumbnail(cfg, relPath)
		return "", err
	}
	if err := s.store.RefreshSeriesStats(ctx, book.SeriesID); err != nil {
		slog.WarnContext(ctx, "refresh series stats after custom cover failed", "series_id", book.SeriesID, "error", err)
	}
	return relPath, nil
}

// writeCoverThumbnail 把原始图片处理成 400px 缩略图，用内容 SHA1 命名（与扫描封面同一目录方案），
// 内容寻址天然去重且能刷新浏览器缓存。返回相对路径（<2字符子目录>/<hash>.<ext>）。
func (s *Scanner) writeCoverThumbnail(ctx context.Context, cfg config.Config, imageData []byte, mediaType string) (string, error) {
	targetFormat := cfg.Scanner.ThumbnailFormat
	if targetFormat == "" {
		targetFormat = "webp"
	}
	processed, contentType, err := images.ProcessImage(ctx, imageData, mediaType, images.ProcessOptions{Width: 400, Quality: 82, Format: targetFormat})
	if err != nil || len(processed) == 0 {
		processed, contentType, err = images.ProcessImage(ctx, imageData, mediaType, images.ProcessOptions{Width: 400, Quality: 82, Format: "jpeg"})
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

// writeThumbnailFile 把缩略图落盘，返回落盘本身的耗时。等令牌与限流是调用方的事，这里只剩 IO。
func writeThumbnailFile(thumbDir, fullPath string, data []byte) (time.Duration, error) {
	started := time.Now()
	if err := os.MkdirAll(thumbDir, 0755); err != nil {
		return time.Since(started), err
	}
	if err := os.WriteFile(fullPath, data, 0644); err != nil {
		return time.Since(started), err
	}
	return time.Since(started), nil
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
// config.NonThumbnailDirs 点名的子目录整棵跳过——它们落在缩略图目录内，却不是缩略图。
// progressCb 只收计数：展示文案是调用方的事，扫描器不渲染用户可见文字。
func (s *Scanner) CleanupThumbnails(ctx context.Context, progressCb func(deleted, scanned int)) error {
	cfg := s.currentConfig()
	thumbDir := thumbnailBaseDir(cfg)
	skipDirs := make(map[string]bool)
	for _, dir := range config.NonThumbnailDirs(cfg) {
		skipDirs[dir] = true
	}

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
			if skipDirs[filepath.Clean(path)] {
				return filepath.SkipDir // 不是缩略图，整棵子树都不归这里清
			}
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

// Close joins standalone cover batches and releases the shared cover workers.
// Callers must first stop scan inputs and wait for owned scan and cover runs.
func (s *Scanner) Close() {
	if s == nil {
		return
	}
	s.selfDrained.Wait()
	s.coverMu.Lock()
	if !s.coverClosed {
		s.coverClosed = true
		if s.coverQueue != nil {
			close(s.coverQueue)
		}
	}
	s.coverMu.Unlock()
	s.coverWorkers.Wait()
}
