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
	*ingestionCoordinator
	startOnce       sync.Once
	eventLoop       sync.WaitGroup
	watcher         WatchSource
	libs            map[string]int64
	watched         map[string]struct{}
	formats         map[int64]config.ScanFormatSet
	stopOnce        sync.Once
	registryMu      sync.Mutex
	registrationOps sync.Mutex
	registryPending map[string]registrationChange
	registryWake    chan struct{}
	registryDone    sync.WaitGroup
}

// ErrScanCoalesced 是 WatcherHooks.ScanLibrary 的一条约定出口：本次派发被**合并**进了
// 这个库上已经排着的那次扫描，因此本次没有真的扫，也不必重试。
//
// 它不是闸门——判「这个库是不是已经在扫」的是任务那一处的准入（ADR 0005），这里只是把那个
// 判定的结果讲给监听器听：被合并掉的那次扫描**还没跑完**，跟在它后面的清理因此必须重新排期。
var ErrScanCoalesced = errors.New("scanner: scan was coalesced into a queued run")

// ErrScanSuppressed 是 WatcherHooks.ScanLibrary 的另一条约定出口：这个库的自动发起被**停发**
// 挡下了（连败到阈值、**退避**没到期，或者用户人工禁用），因此本次没有扫。
//
// 它与 ErrScanCoalesced 的区别在于**要不要重排**：被合并的那次还有人替它扫，重排一次清理即可；
// 被停发挡下的这次没有人扫，而条件也不会因为再等一个去抖窗口就变——重排只会每隔几秒再被挡一次。
var ErrScanSuppressed = errors.New("scanner: automatic scan is suppressed for this library")

// WatcherHooks 是监听器派生工作的三个出口。监听器自己**不调用扫描器**：它只知道
// 「这个库该扫了」「这个库该清了」，去哪儿扫、这次扫描算作谁的一次**运行**，由装配方回答。
//
// 三个都必须给。留一个 nil 就等于让派生的扫描退回「不属于任何任务」——用户听见盘响、
// 任务中心一片空白，而那正是 ADR 0005 要消灭的东西。
type WatcherHooks struct {
	// ScanLibrary 扫这个库，**返回时这次扫描已经结束**：监听器按它的成败决定敢不敢接着清理。
	//
	// 参数只有库 id：扫哪条路径、按什么档位扫，是装配方从库行里读的，监听器不复述。
	ScanLibrary func(ctx context.Context, libraryID int64) (ScanOutcome, error)
	// CleanupLibrary 清这个库的幽灵记录，同样等它结束才返回。
	CleanupLibrary func(ctx context.Context, libraryID int64, eligible func() bool) error
	// LibraryScanRunning 报告这个库此刻有没有扫描在跑，含别处发起的。
	LibraryScanRunning func(libraryID int64) bool
}

func (h WatcherHooks) validate() error {
	switch {
	case h.ScanLibrary == nil:
		return errors.New("scanner: WatcherHooks.ScanLibrary 不得为 nil")
	case h.CleanupLibrary == nil:
		return errors.New("scanner: WatcherHooks.CleanupLibrary 不得为 nil")
	case h.LibraryScanRunning == nil:
		return errors.New("scanner: WatcherHooks.LibraryScanRunning 不得为 nil")
	}
	return nil
}

func NewFileWatcher(hooks WatcherHooks) (*FileWatcher, error) {
	if err := hooks.validate(); err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return NewFileWatcherWithSource(hooks, fsnotifySource{w})
}

// WatchSource supplies notifications and synchronous registrations. Close must unblock registrations.
type WatchSource interface {
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	Add(string) error
	Remove(string) error
	Close() error
}

type fsnotifySource struct{ *fsnotify.Watcher }

func (s fsnotifySource) Events() <-chan fsnotify.Event { return s.Watcher.Events }
func (s fsnotifySource) Errors() <-chan error          { return s.Watcher.Errors }

// NewFileWatcherWithSource owns the source until Stop has drained input and derived work.
func NewFileWatcherWithSource(hooks WatcherHooks, source WatchSource) (*FileWatcher, error) {
	if err := hooks.validate(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, errors.New("scanner: watch source is required")
	}
	fw := &FileWatcher{
		ingestionCoordinator: newIngestionCoordinator(hooks),
		watcher:              source,
		formats:              make(map[int64]config.ScanFormatSet),
		libs:                 make(map[string]int64),
		watched:              make(map[string]struct{}),
		registryPending:      make(map[string]registrationChange),
		registryWake:         make(chan struct{}, 1),
	}
	fw.startRegistry()
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
	fw.registrationOps.Lock()
	defer fw.registrationOps.Unlock()
	fw.mu.Lock()
	if fw.stopping {
		fw.mu.Unlock()
		return errors.New("scanner: watcher is stopped")
	}
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

// UnwatchLibrary 停止监听指定库，保留其他已注册资料库共用的目录监听。
func (fw *FileWatcher) UnwatchLibrary(path string) {
	fw.registrationOps.Lock()
	defer fw.registrationOps.Unlock()
	fw.mu.Lock()
	for root, libID := range fw.libs {
		if pathUnderRoot(root, path) && pathUnderRoot(path, root) {
			delete(fw.formats, libID)
			delete(fw.pending, libID)
			delete(fw.pendingCleanup, libID)
			delete(fw.libs, root)
		}
	}
	var toRemove []string
	for watchedPath := range fw.watched {
		if !pathUnderRoot(watchedPath, path) {
			continue
		}
		shared := false
		for root := range fw.libs {
			if pathUnderRoot(watchedPath, root) {
				shared = true
				break
			}
		}
		if shared {
			continue
		}
		delete(fw.watched, watchedPath)
		toRemove = append(toRemove, watchedPath)
	}
	fw.mu.Unlock()

	// fsnotify 的 reader 可能正等待事件消费者，底层注销不得持有 fw.mu。
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
			fw.changedLocked(libID, true, now)
		}
	}
	fw.mu.Unlock()

	for _, watchedPath := range toRemove {
		fw.queueRegistration(watchedPath, false)
	}
}

// Start 启动文件监控事件循环
func (fw *FileWatcher) Start(publishEvent func(string)) {
	fw.startOnce.Do(func() {
		fw.mu.Lock()
		if fw.stopping {
			fw.mu.Unlock()
			return
		}
		fw.start(publishEvent)
		fw.eventLoop.Add(1)
		fw.mu.Unlock()
		go func() {
			defer fw.eventLoop.Done()

			for {
				select {
				case <-fw.stopCh:
					return

				case event, ok := <-fw.watcher.Events():
					if !ok {
						return
					}
					createdDirectory := false
					if event.Op&fsnotify.Create != 0 {
						if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
							createdDirectory = true
							fw.queueRegistration(event.Name, true)
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
					if !createdDirectory && !config.IsSupportedArchiveExtension(ext) {
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
						if !createdDirectory && !fw.formats[libID].Matches(event.Name) {
							continue
						}
						fw.changedLocked(libID, false, time.Now())
						slog.Debug("File change detected", "file", event.Name, "library_id", libID)
					}
					fw.mu.Unlock()

				case err, ok := <-fw.watcher.Errors():
					if !ok {
						return
					}
					slog.Warn("File watcher error", "error", err)

				}
			}
		}()
	})
}

// Stop 停止监听，取消并等待 watcher 派生的扫描/清理退出。
//
// Stop 返回代表「事件循环与本 watcher 派生的 ScanLibrary/CleanupLibrary 调用栈都已退出」。
// 共享封面 worker 池由 Scanner.Close 在应用运行全部退出之后释放。
func (fw *FileWatcher) Stop() {
	fw.stopOnce.Do(func() {
		fw.mu.Lock()
		fw.stopping = true
		fw.mu.Unlock()
		close(fw.stopCh)
		fw.cancelBase()
		_ = fw.watcher.Close()
	})
	// 先与事件循环在 fw.mu 上握一次手再 Wait：拿到锁即证明没有派发正在进行，stopping
	// 置上之后也不会再有。close(stopCh) 单独做不到这件事——select 仍可能先挑中 ticker。
	fw.mu.Lock()
	fw.stopping = true
	fw.mu.Unlock()
	fw.eventLoop.Wait()
	// Close releases synchronous Add/Remove before joining a caller-owned registration.
	fw.registrationOps.Lock()
	fw.registrationOps.Unlock()
	fw.registryDone.Wait()
	fw.coordinationLoop.Wait()
	fw.inFlight.Wait()
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

	_ = walkDirFollowingSymlinks(fw.baseCtx, root, func(path string, d fs.DirEntry, err error) error {
		if err := fw.baseCtx.Err(); err != nil {
			return err
		}
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
