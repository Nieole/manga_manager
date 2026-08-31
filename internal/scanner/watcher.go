// 文件监听器：按资料库根递归注册 fsnotify、去抖后触发库扫描与 CleanupLibrary、
// 把事件路径按分隔符边界判回所属资料库，并在停机时收回派生出去的后台工作。
// 清理只在该库的改名重连已了结时才敢跑（见 rehomeUnsettledLocked），否则就是丢阅读进度。

package scanner

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"manga-manager/internal/config"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher 监听库目录的文件变动，自动触发增量扫描
type FileWatcher struct {
	scanner *Scanner
	watcher *fsnotify.Watcher
	mu      sync.Mutex
	// pending: 文件变动后按库排期，去抖（watcherTimings.scanDebounce）后触发一次增量扫描
	pending map[int64]time.Time
	libs    map[string]int64 // path -> libraryID
	watched map[string]struct{}
	// pendingCleanup: 检测到删除/重命名后按库排期，去抖后触发 CleanupLibrary 清除幽灵记录。
	// 一条清理只在该库「没有未了结的改名重连」时才敢跑，见 rehomeUnsettledLocked。
	pendingCleanup map[int64]cleanupSchedule
	// scansInFlight: 本 watcher 已派发、尚未返回的库扫描数（按库）。改名重连是扫描**末尾**
	// 才写的，扫描还在飞就等于重连还没落地，此时清理会把旧行当成「文件已消失」删掉。
	scansInFlight map[int64]int
	stopCh        chan struct{}
	stopOnce      sync.Once
	// formats 是**按库**的归档格式集（libraryID -> 集合）。共用一份全局列表会让库级
	// scan_formats 在监听侧形同虚设。
	formats map[int64]config.ScanFormatSet

	// baseCtx 随 Stop 一起取消，watcher 派生的所有扫描/清理都必须挂在它下面。挂到
	// context.Background() 上的裸 goroutine 既不可取消、停机也不等待——优雅关闭返回
	// 之后它们仍在往一个即将关掉的 store 里写。
	baseCtx    context.Context
	cancelBase context.CancelFunc
	// inFlight 追踪派生出去的扫描/清理，Stop 会等它们退出。
	inFlight sync.WaitGroup

	// scanLibrary / cleanupLibrary / libraryScanRunning 供测试注入桩；生产留 nil 时走 fw.scanner。
	scanLibrary    func(ctx context.Context, libraryID int64, rootPath string, force bool) error
	cleanupLibrary func(ctx context.Context, libraryID int64) error
	// libraryScanRunning 报告该库此刻是否有扫描在跑，含**别处**发起的（任务面板的「扫描资料库」）。
	// scansInFlight 只看得见本 watcher 派发的那些，而任何一次扫描的重连都写在末尾。
	libraryScanRunning func(libraryID int64) bool

	// timings 是事件循环的节拍与去抖参数，测试调小它以便在秒内跑完时序用例。
	timings watcherTimings
}

// cleanupSchedule 是一次库清理的排期。两个时刻各答一个问题：lastEvent 答「去抖窗口到了没」，
// firstEvent 答「最多还能再推迟多久」——推迟上限必须从**第一个**删除/重命名事件起算，
// 否则持续不断的删除会把 lastEvent 一直往后推，上限永远触发不了。
type cleanupSchedule struct {
	firstEvent time.Time
	lastEvent  time.Time
}

// due 报告这条清理本轮是否该被处理：去抖窗口已过，或已经等过了推迟上限。
func (c cleanupSchedule) due(now time.Time, t watcherTimings) bool {
	return now.Sub(c.lastEvent) >= t.cleanupDebounce || c.overdue(now, t)
}

// overdue 报告这条清理是否已等过推迟上限。它是让扫描**越过自己的去抖窗口**提前派发的唯一理由。
func (c cleanupSchedule) overdue(now time.Time, t watcherTimings) bool {
	return now.Sub(c.firstEvent) >= t.cleanupMaxDeferral
}

// watcherTimings 是去抖判定用到的四个时长。
type watcherTimings struct {
	// tick 是去抖判定的轮询周期。
	tick time.Duration
	// scanDebounce 是最后一次文件变动之后、触发扫描之前的静默期。
	scanDebounce time.Duration
	// cleanupDebounce 是最后一次删除/重命名之后、触发库清理之前的静默期。
	cleanupDebounce time.Duration
	// cleanupMaxDeferral 是一条清理从首个删除/重命名事件起最多被推迟多久。
	//
	// 清理要等「该库没有未了结的改名重连」，而持续写入的库里扫描的去抖窗口一直后移，
	// 这个条件可以永远不成立——删掉的书就永远以幽灵记录挂在库里。等过上限之后不再干等，
	// 改为主动为该库派发一次扫描、把清理接在它之后串行：安全性不变（清理仍在一次**成功**的
	// 扫描之后），代价只是这次扫描可能读到一个还在拷贝中的半成品归档，下一次扫描自愈。
	cleanupMaxDeferral time.Duration
}

func defaultWatcherTimings() watcherTimings {
	return watcherTimings{
		tick:               2 * time.Second,
		scanDebounce:       5 * time.Second,
		cleanupDebounce:    5 * time.Second,
		cleanupMaxDeferral: 2 * time.Minute,
	}
}

func NewFileWatcher(s *Scanner) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	fw := &FileWatcher{
		scanner:        s,
		watcher:        w,
		pending:        make(map[int64]time.Time),
		formats:        make(map[int64]config.ScanFormatSet),
		libs:           make(map[string]int64),
		watched:        make(map[string]struct{}),
		pendingCleanup: make(map[int64]cleanupSchedule),
		scansInFlight:  make(map[int64]int),
		stopCh:         make(chan struct{}),
		baseCtx:        ctx,
		cancelBase:     cancel,
		timings:        defaultWatcherTimings(),
	}
	if s != nil {
		fw.libraryScanRunning = s.libraryScanActive
	}
	return fw, nil
}

// pathUnderRoot 报告 child 是否位于 root 之内（含 child == root）。
//
// 判定必须落在路径分隔符边界上。无分隔符的 strings.HasPrefix 会把 /data/manga2 的事件判成属于
// /data/manga，两个前缀相同的兄弟目录库互相串台；事件循环与 handleRemoval 都是
// `for ... range fw.libs` 后第一个命中就 break，Go 的 map 迭代顺序又随机，受害的是哪个库每次
// 都不一样——删除事件记到错误的库上，真正该清理的库留下幽灵记录。
//
// 用 filepath.Rel 而不是「补一个分隔符再 HasPrefix」，是因为它顺带处理了 . 与 .. 的规范化；
// 跨盘符（Windows 的 C: 与 D:）时 Rel 返回错误而非 ".."，这里按「不在其内」处理，正确。
func pathUnderRoot(child, root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	absChild, err := filepath.Abs(child)
	if err != nil {
		absChild = filepath.Clean(child)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		absRoot = filepath.Clean(root)
	}
	if runtime.GOOS == "windows" {
		// Windows 的文件系统大小写不敏感：C:\Data\Manga 与 c:\data\manga 是同一个目录。
		absChild = strings.ToLower(absChild)
		absRoot = strings.ToLower(absRoot)
	}
	if absChild == absRoot {
		return true
	}
	rel, err := filepath.Rel(absRoot, absChild)
	if err != nil {
		return false // 跨盘符等无法表达相对关系的情形
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// WatchLibrary 开始监听指定库目录
func (fw *FileWatcher) WatchLibrary(libraryID int64, path string, scanFormats string) error {
	fw.mu.Lock()
	fw.libs[path] = libraryID
	fw.formats[libraryID] = config.NewScanFormatSet(scanFormats)
	fw.mu.Unlock()

	report := fw.watchRecursive(path)
	switch {
	case report.LimitReached:
		slog.Error("File watcher hit the kernel watch limit; most of this library will not be monitored",
			"library_id", libraryID, "path", path, "watched", report.Watched, "failed", report.Failed,
			"hint", "raise fs.inotify.max_user_watches", "error", report.FirstErr)
	case !report.OK():
		slog.Warn("File watcher registered only part of the library",
			"library_id", libraryID, "path", path,
			"total", report.Total, "watched", report.Watched, "failed", report.Failed,
			"unreadable_dirs", report.UnreadableDirs, "error", report.FirstErr)
	default:
		slog.Info("File watcher started for library", "library_id", libraryID, "path", path,
			"watched", report.Watched, "symlink_dirs", report.SymlinkDirs)
	}
	// 部分失败按部分成功处理：能监听多少算多少，比整库不监听强得多。
	// 只有一个目录都没能注册上才向调用方报错。
	if report.Watched == 0 && report.AlreadyWatched == 0 && report.FirstErr != nil {
		return report.FirstErr
	}
	return nil
}

// UnwatchLibrary 停止监听
func (fw *FileWatcher) UnwatchLibrary(path string) {
	fw.mu.Lock()
	if libID, ok := fw.libs[path]; ok {
		delete(fw.formats, libID) // 与 libs 配套清理，否则删库/改库路径会让 formats 泄漏
	}
	delete(fw.libs, path)
	var toRemove []string
	for watchedPath := range fw.watched {
		if watchedPath == path || strings.HasPrefix(watchedPath, path+string(filepath.Separator)) {
			toRemove = append(toRemove, watchedPath)
			delete(fw.watched, watchedPath)
		}
	}
	fw.mu.Unlock()

	for _, watchedPath := range toRemove {
		_ = fw.watcher.Remove(watchedPath)
	}
}

// handleRemoval 处理文件/目录的删除或重命名（旧名）：清理 watched 集合中该路径及其子项、
// 移除对应的 fsnotify watch（防 watched map 泄漏，并让重建的同名目录能被重新挂载而非因残留 key 跳过），
// 同时为所属库排期一次 CleanupLibrary 去除幽灵记录（删除的文件/系列在库视图与搜索中残留）。
func (fw *FileWatcher) handleRemoval(name string) {
	fw.mu.Lock()
	var toRemove []string
	for watchedPath := range fw.watched {
		if watchedPath == name || strings.HasPrefix(watchedPath, name+string(filepath.Separator)) {
			toRemove = append(toRemove, watchedPath)
			delete(fw.watched, watchedPath)
		}
	}
	// 对**所有**包含该路径的库排期，不是找到一个就 break。
	// 嵌套库（一个库的根在另一个库之内）共享同一棵子树，两边都需要清理幽灵记录；
	// 而 break 加上 map 的随机迭代顺序，等于随机挑一个库来清、另一个永远漏掉。
	// CleanupLibrary 本身幂等且自带熔断，重复排期是安全的。
	now := time.Now()
	for libPath, libID := range fw.libs {
		if pathUnderRoot(name, libPath) {
			fw.scheduleCleanupLocked(libID, now)
		}
	}
	fw.mu.Unlock()

	for _, watchedPath := range toRemove {
		_ = fw.watcher.Remove(watchedPath) // 忽略错误：Linux 删目录已自动回收内核 watch
	}
}

// scheduleCleanupLocked 排期（或续期）一条库清理。调用方须持有 fw.mu。
// firstEvent 只在这条排期首次建立时写：它是推迟上限的起算点，续期不该把它往后推。
func (fw *FileWatcher) scheduleCleanupLocked(libID int64, now time.Time) {
	sched := fw.pendingCleanup[libID]
	if sched.firstEvent.IsZero() {
		sched.firstEvent = now
	}
	sched.lastEvent = now
	fw.pendingCleanup[libID] = sched
}

// requeueCleanupAfterFailedScan 在同轮扫描没跑成时把清理放回排期，并**同时**重新排期一次扫描。
//
// 清理只有跟在一次成功的扫描之后才安全。只放回清理而不放回扫描，下一个去抖窗口会因为
// 「这个库没有排期中的扫描」而判定安全并直接删行——那正是改名重连还没发生的时刻。
// 两个时刻都重置为现在：推迟上限是用来打破「去抖窗口一直后移」这个死结的，
// 而扫描失败是另一回事，按正常去抖节奏重试即可，不该退化成每个 tick 顶一次扫描。
func (fw *FileWatcher) requeueCleanupAfterFailedScan(libID int64) {
	fw.mu.Lock()
	now := time.Now()
	fw.pendingCleanup[libID] = cleanupSchedule{firstEvent: now, lastEvent: now}
	fw.pending[libID] = now
	fw.mu.Unlock()
}

// rehomeUnsettledLocked 报告该库此刻是否还有**未了结的改名重连**，非空即为「清理必须继续等」的理由。
// 调用方须持有 fw.mu。
//
// 「安全」的实质不是「本轮有没有扫描要跑」，而是「这个库当前有没有改名还没被重连上」。
// 三条信号缺一不可，因为它们各自只看得见一段时间：
//
//   - pending 里还挂着这个库 —— 有文件变动排了扫描但还没派发。改名的 Create(新名) 半边一定会
//     排一次扫描，所以「排了但去抖窗口还没到」正是改名待重连的时刻。光靠「本轮有没有扫描要跑」
//     判不出这一条：持续写入（一边改名一边往库里拷新书）会让扫描的窗口一直后移，两条排期就此分叉。
//   - 本 watcher 有已派发未返回的该库扫描 —— 重连是扫描末尾才写的，扫描没返回就不算落地。
//   - 别处发起的扫描正在跑 —— 任务面板的「扫描资料库」同样在末尾写重连。
func (fw *FileWatcher) rehomeUnsettledLocked(libID int64) string {
	if _, ok := fw.pending[libID]; ok {
		return "scan_pending"
	}
	if fw.scansInFlight[libID] > 0 {
		return "scan_in_flight"
	}
	if fw.libraryScanRunning != nil && fw.libraryScanRunning(libID) {
		return "scan_running_elsewhere"
	}
	return ""
}

// libraryPathLocked 反查库根路径，查不到返回空串。调用方须持有 fw.mu。
func (fw *FileWatcher) libraryPathLocked(libID int64) string {
	for p, id := range fw.libs {
		if id == libID {
			return p
		}
	}
	return ""
}

// scanFinished 记一次派发出去的扫描已返回。
func (fw *FileWatcher) scanFinished(libID int64) {
	fw.mu.Lock()
	if fw.scansInFlight[libID] <= 1 {
		delete(fw.scansInFlight, libID)
	} else {
		fw.scansInFlight[libID]--
	}
	fw.mu.Unlock()
}

// dispatchDueLocked 派发本轮到期的扫描与清理。调用方须持有 fw.mu。
func (fw *FileWatcher) dispatchDueLocked(now time.Time, publishEvent func(string)) {
	for libID, lastChange := range fw.pending {
		// 去抖：最后一次文件变动距今超过 scanDebounce 才触发扫描。例外是该库有一条清理
		// 已经等过了推迟上限——那条清理必须跟在一次扫描之后才敢跑，而持续写入会让这个
		// 窗口一直后移，只能由它反过来把扫描顶出去（见 cleanupMaxDeferral）。
		sched, hasCleanup := fw.pendingCleanup[libID]
		forcedByCleanup := hasCleanup && sched.overdue(now, fw.timings)
		if now.Sub(lastChange) < fw.timings.scanDebounce && !forcedByCleanup {
			continue
		}
		delete(fw.pending, libID)
		libPath := fw.libraryPathLocked(libID)
		if libPath == "" {
			continue
		}
		// 同一个库若也有一条到期的清理，把它接在这次扫描之后串行跑。改名一本书会产生
		// Rename(旧名)+Create(新名) 两个事件，分别排期清理与扫描；清理只做 stat，
		// 比要走完整棵树、末尾才写 RehomeBookPath 的扫描快得多，并发派发时必然抢在
		// 改名重连之前把旧行当成"文件已消失"删掉，阅读进度、书签、合集归属、阅读清单
		// 条目随 ON DELETE CASCADE 一起没。
		cleanupAfterScan := hasCleanup && sched.due(now, fw.timings)
		if cleanupAfterScan {
			delete(fw.pendingCleanup, libID)
		}
		slog.Info("Hot reload triggered by file watcher", "library_id", libID, "forced_by_cleanup", forcedByCleanup)
		if publishEvent != nil {
			publishEvent("hot_reload:")
		}
		fw.scansInFlight[libID]++
		fw.inFlight.Add(1)
		go func(id int64, path string, cleanup bool) {
			defer fw.inFlight.Done()
			// 计数在**清理之后**才归零：多留这一会儿只会让别的清理更保守地多等一轮。
			defer fw.scanFinished(id)
			err := fw.runScanLibrary(fw.baseCtx, id, path, false)
			switch {
			case errors.Is(err, context.Canceled):
				// 停机取消，不是故障。

			case errors.Is(err, ErrScanAlreadyRunning):
				// 文件变更去抖后触发的扫描与在跑的扫描撞车属正常情况：
				// 正在跑的那次本就会看到这批新文件，无需重试也不必报错。
				slog.Info("Hot reload scan skipped, another scan is in progress", "library_id", id)
			case err != nil:
				slog.Error("Hot reload scan failed", "library_id", id, "error", err)
			}
			if !cleanup {
				return
			}
			// 扫描没跑成就不能清理：改名重连可能还没发生，此刻删行就是丢数据。
			// 连同扫描一起重新排期，交给下一个去抖窗口。
			if err != nil {
				fw.requeueCleanupAfterFailedScan(id)
				return
			}
			if err := fw.runCleanupLibrary(fw.baseCtx, id); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Watcher-triggered cleanup failed", "library_id", id, "error", err)
			}
		}(libID, libPath, cleanupAfterScan)
	}

	// 去抖后单独触发库清理，清除删除/重命名遗留的幽灵记录。同轮有扫描的已在上面被取走、
	// 接在扫描之后串行；走到这里的还必须再过一道闸：该库不能有未了结的改名重连。
	// CleanupLibrary 自带根目录探测与占比熔断，存储离线时不会误删。
	for libID, sched := range fw.pendingCleanup {
		if !sched.due(now, fw.timings) {
			continue
		}
		if reason := fw.rehomeUnsettledLocked(libID); reason != "" {
			// 留在排期里，时间戳原样不动：下一轮再判。什么时候能跑得看闸何时放开——
			// 在飞的扫描必然结束；排期中的扫描则由 cleanupMaxDeferral 兜底顶出去。
			slog.Debug("Watcher-triggered cleanup deferred", "library_id", libID, "reason", reason)
			continue
		}
		delete(fw.pendingCleanup, libID)
		fw.inFlight.Add(1)
		go func(id int64) {
			defer fw.inFlight.Done()
			if err := fw.runCleanupLibrary(fw.baseCtx, id); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("Watcher-triggered cleanup failed", "library_id", id, "error", err)
			}
		}(libID)
	}
}

// Start 启动文件监控事件循环
func (fw *FileWatcher) Start(publishEvent func(string)) {
	go func() {
		debounceTimer := time.NewTicker(fw.timings.tick)
		defer debounceTimer.Stop()

		for {
			select {
			case <-fw.stopCh:
				return

			case event, ok := <-fw.watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create != 0 {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						if r := fw.watchRecursive(event.Name); r.FirstErr != nil {
							slog.Warn("Failed to watch part of the new subdirectory",
								"path", event.Name, "watched", r.Watched, "failed", r.Failed, "error", r.FirstErr)
						}
					}
				}
				// 删除/重命名（旧名）：清理监听集合并排期库清理，去除幽灵记录、修复重建目录失监。
				if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					fw.handleRemoval(event.Name)
				}
				// 只关注 Create 和 Write（用于触发增量扫描发现新增/变动文件）
				if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
					continue
				}
				// 检查是否是支持的漫画文件
				ext := strings.ToLower(filepath.Ext(event.Name))
				// 锁外的廉价预筛：先用全局白名单挡掉 .nfo/.jpg/编辑器临时文件这类绝大多数事件。
				// 库级格式过滤放到下面定位库之后再做——若把它也挪到这里，每个无关事件都要抢一次
				// fw.mu 并遍历全部库，而这是事件循环里唯一的全局锁，大库批量写入时会明显变热。
				if !config.IsSupportedArchiveExtension(ext) {
					continue
				}

				// 找到所属的库，并按**该库**的 scan_formats 判定是否值得重扫。
				// 同 handleRemoval：所有包含该文件的库都要排期（嵌套库共享子树）。
				fw.mu.Lock()
				for libPath, libID := range fw.libs {
					if !pathUnderRoot(event.Name, libPath) {
						continue
					}
					// formats 里查不到（库刚被 Unwatch 的竞态）时取零值 -> fail-open，与扫描器同口径。
					if !fw.formats[libID].Matches(event.Name) {
						continue
					}
					fw.pending[libID] = time.Now()
					slog.Debug("File change detected", "file", event.Name, "library_id", libID)
				}
				fw.mu.Unlock()

			case err, ok := <-fw.watcher.Errors:
				if !ok {
					return
				}
				slog.Warn("File watcher error", "error", err)

			case <-debounceTimer.C:
				fw.mu.Lock()
				fw.dispatchDueLocked(time.Now(), publishEvent)
				fw.mu.Unlock()
			}
		}
	}()
}

// Stop 停止监听，取消并等待 watcher 派生的扫描/清理退出。
//
// Stop 返回代表「事件循环与本 watcher 派生的 ScanLibrary/CleanupLibrary 调用栈都已退出」。
// **不包括** Scanner 的全局封面 worker 池——那是进程级共享的，取消后队列里的任务会
// 迅速空转排干（runCoverJob 入口先等暂停闸），残留窗口最多是几个在飞的封面任务。
func (fw *FileWatcher) Stop() {
	fw.stopOnce.Do(func() {
		close(fw.stopCh)
		fw.cancelBase()
		_ = fw.watcher.Close()
	})
	fw.inFlight.Wait()
}

// runScanLibrary / runCleanupLibrary 是可注入的间接层，生产走真实 scanner。
func (fw *FileWatcher) runScanLibrary(ctx context.Context, libraryID int64, rootPath string, force bool) error {
	if fw.scanLibrary != nil {
		return fw.scanLibrary(ctx, libraryID, rootPath, force)
	}
	// watcher 派生的扫描不属于任何任务：它由文件系统事件触发，没有发起方可以承接进度。
	return fw.scanner.ScanLibrary(ctx, libraryID, rootPath, force, nil)
}

func (fw *FileWatcher) runCleanupLibrary(ctx context.Context, libraryID int64) error {
	if fw.cleanupLibrary != nil {
		return fw.cleanupLibrary(ctx, libraryID)
	}
	return fw.scanner.CleanupLibrary(ctx, libraryID)
}

// WatchReport 汇报一次递归注册的结果。
//
// 记账契约（用例会钉住）：Total 是**遍历到的目录数**，每个目录恰好落进
// Watched / AlreadyWatched / Failed 三者之一，三者之和恒等于 Total。
// SymlinkDirs 与 UnreadableDirs 是正交的补充计数，不参与上面的划分。
type WatchReport struct {
	Total          int
	Watched        int
	AlreadyWatched int
	Failed         int

	// SymlinkDirs 记「指向目录的软链」。它不在 Total 里：filepath.WalkDir 用的是 lstat，
	// 软链在它眼里不是目录，压根不会进入注册流程。单独记一笔只为让运维看得见
	// ——fsnotify 也不跟进软链目标，这棵子树确实没被监听。它**不算故障**，
	// 否则一个装饰性的软链就会让整库报「监听不完整」，所以不参与 OK()。
	SymlinkDirs int
	// UnreadableDirs 表示目录自身注册与否另说，但它的子目录没能枚举出来
	// （权限/瞬时 IO），也就是这棵子树有遗漏。它与上面三项正交：这类目录在
	// 前一次回调里已经被计过账了，不能重复划分。
	UnreadableDirs int
	// LimitReached 表示撞上了内核 watch 配额（Linux 的 fs.inotify.max_user_watches）。
	// 这一条要单独拎出来：它不是某个目录的偶发问题，而是「从这里往后基本都会失败」，
	// 运维动作也不同（调 sysctl，而不是查权限）。
	LimitReached bool
	// FirstErr 保留第一个失败原因，供调用方打日志时给出可诊断的样本。
	FirstErr error
}

// OK 报告是否全量注册成功。软链目录不算失败（见字段注释）。
func (r WatchReport) OK() bool {
	return r.Failed == 0 && r.UnreadableDirs == 0 && !r.LimitReached
}

// watchRecursive 递归注册目录监听，**遇错继续**。
//
// WalkDir 与 watcher.Add 的错误都不能直接 return 出去：一个不可读的子目录（权限）或一次配额
// 不足就会让 WalkDir 立刻中止，该目录之后的**整棵子树**静默失监，而唯一的调用方只打一条 Warn。
// 大库里这意味着用户以为开着热重载，实际上大半个库的改动永远不会被发现。
//
// 登记必须在 **Add 成功之后**。事件循环里新建目录走的也是这个函数，`exists` 短路一旦命中就
// 直接返回——若先登记再按失败回删，只要有一瞬间记错，那个目录就再也不会被重试注册。
func (fw *FileWatcher) watchRecursive(root string) WatchReport {
	var report WatchReport
	fail := func(err error) {
		report.Failed++
		if report.FirstErr == nil {
			report.FirstErr = err
		}
		if errors.Is(err, syscall.ENOSPC) {
			// ENOSPC 在 inotify 语境下不是「磁盘满」，而是 watch 数触顶。
			report.LimitReached = true
		}
	}

	_ = walkDirFollowingSymlinks(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if d == nil {
				// 连根都 lstat 不了，这一趟什么也没遍历到。
				report.Total++
				fail(err)
				return nil
			}
			// 走到这里意味着「目录本身访问过了（上一次回调已计过账），但子项枚举失败」。
			// WalkDir 对同一个目录会先以 err==nil 回调一次、ReadDir 出错后再回调一次，
			// 这里若再 Total++ 就会重复计数、破坏「四项之和 == Total」的契约。
			report.UnreadableDirs++
			if report.FirstErr == nil {
				report.FirstErr = err
			}
			return filepath.SkipDir // 跳过这一棵，继续走兄弟目录
		}
		if !d.IsDir() {
			return nil
		}
		report.Total++
		// 软链目录是被 walkDirFollowingSymlinks 跟进来的（链接路径这一侧）。
		// 单独记一笔：inotify/kqueue 注册的是链接解析后的真实目录，
		// 事件名却挂在链接路径下，出问题时这个数字能省掉一轮排查。
		if lst, lerr := os.Lstat(path); lerr == nil && lst.Mode()&os.ModeSymlink != 0 {
			report.SymlinkDirs++
		}

		fw.mu.Lock()
		_, exists := fw.watched[path]
		fw.mu.Unlock()
		if exists {
			report.AlreadyWatched++
			return nil
		}

		if err := fw.watcher.Add(path); err != nil {
			fail(err)
			// 不登记进 fw.watched：登记了就会被上面的 exists 短路永久跳过，再也没有重试机会。
			return nil
		}
		fw.mu.Lock()
		fw.watched[path] = struct{}{}
		fw.mu.Unlock()
		report.Watched++
		return nil
	})
	return report
}
