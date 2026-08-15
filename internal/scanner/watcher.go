// 文件监听器：按资料库根递归注册 fsnotify、去抖后触发库扫描与 CleanupLibrary、
// 把事件路径按分隔符边界判回所属资料库，并在停机时收回派生出去的后台工作。

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
	// debounce: 同一库目录在 5 秒内只触发一次扫描
	pending map[int64]time.Time
	libs    map[string]int64 // path -> libraryID
	watched map[string]struct{}
	// pendingCleanup: 检测到删除/重命名后按库排期，去抖后触发 CleanupLibrary 清除幽灵记录
	pendingCleanup map[int64]time.Time
	stopCh         chan struct{}
	stopOnce       sync.Once
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

	// scanLibrary / cleanupLibrary 供测试注入桩；生产留 nil 时走 fw.scanner。
	scanLibrary    func(ctx context.Context, libraryID int64, rootPath string, force bool) error
	cleanupLibrary func(ctx context.Context, libraryID int64) error
}

func NewFileWatcher(s *Scanner) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &FileWatcher{
		scanner:        s,
		watcher:        w,
		pending:        make(map[int64]time.Time),
		formats:        make(map[int64]config.ScanFormatSet),
		libs:           make(map[string]int64),
		watched:        make(map[string]struct{}),
		pendingCleanup: make(map[int64]time.Time),
		stopCh:         make(chan struct{}),
		baseCtx:        ctx,
		cancelBase:     cancel,
	}, nil
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
	for libPath, libID := range fw.libs {
		if pathUnderRoot(name, libPath) {
			fw.pendingCleanup[libID] = time.Now()
		}
	}
	fw.mu.Unlock()

	for _, watchedPath := range toRemove {
		_ = fw.watcher.Remove(watchedPath) // 忽略错误：Linux 删目录已自动回收内核 watch
	}
}

// Start 启动文件监控事件循环
func (fw *FileWatcher) Start(publishEvent func(string)) {
	go func() {
		debounceTimer := time.NewTicker(2 * time.Second)
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
				now := time.Now()
				for libID, lastChange := range fw.pending {
					// 防抖 5 秒：最后一次文件变动距今超过 5 秒才触发扫描
					if now.Sub(lastChange) >= 5*time.Second {
						delete(fw.pending, libID)
						// 找到库路径
						var libPath string
						for p, id := range fw.libs {
							if id == libID {
								libPath = p
								break
							}
						}
						if libPath != "" {
							slog.Info("Hot reload triggered by file watcher", "library_id", libID)
							if publishEvent != nil {
								publishEvent("hot_reload:")
							}
							fw.inFlight.Add(1)
							go func(id int64, path string) {
								defer fw.inFlight.Done()
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
							}(libID, libPath)
						}
					}
				}
				// 去抖后触发库清理，清除删除/重命名遗留的幽灵记录。CleanupLibrary 自带根目录探测与
				// 占比熔断，存储离线时不会误删。
				for libID, lastChange := range fw.pendingCleanup {
					if now.Sub(lastChange) >= 5*time.Second {
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
	return fw.scanner.ScanLibrary(ctx, libraryID, rootPath, force)
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
