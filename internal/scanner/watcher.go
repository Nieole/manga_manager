// 业务说明：本文件是业务实现，属于漫画库扫描链路，负责发现文件、建立书籍和系列记录、提取封面、同步索引并维护任务进度。
// 它决定本地文件系统如何变成前端资料库、搜索结果和系列聚合视图。
// 维护时应重点关注增量扫描、重命名/删除处理、元数据回填、SQLite FTS5 搜索索引同步和长任务取消。

package scanner

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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
	// formats 是**按库**的归档格式集（libraryID -> 集合）。
	// 此前是一份全局列表，于是库级 scan_formats 在监听侧同样形同虚设。
	formats map[int64]config.ScanFormatSet
}

func NewFileWatcher(s *Scanner) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FileWatcher{
		scanner:        s,
		watcher:        w,
		pending:        make(map[int64]time.Time),
		formats:        make(map[int64]config.ScanFormatSet),
		libs:           make(map[string]int64),
		watched:        make(map[string]struct{}),
		pendingCleanup: make(map[int64]time.Time),
		stopCh:         make(chan struct{}),
	}, nil
}

// pathUnderRoot 报告 child 是否位于 root 之内（含 child == root）。
//
// 此前用的是无分隔符的 strings.HasPrefix，于是 /data/manga2 的事件会被判成属于 /data/manga——
// 两个前缀相同的兄弟目录库互相串台。事件循环与 handleRemoval 都是 `for ... range fw.libs` 后
// 第一个命中就 break，而 Go 的 map 迭代顺序随机，所以受害的是哪个库每次都不一样：
// 删除事件被记到错误的库上，真正该清理的库留下幽灵记录。
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

	err := fw.watchRecursive(path)
	if err != nil {
		slog.Warn("Failed to watch library directory", "path", path, "error", err)
	} else {
		slog.Info("File watcher started for library", "library_id", libraryID, "path", path)
	}
	return err
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
						if err := fw.watchRecursive(event.Name); err != nil {
							slog.Warn("Failed to watch new subdirectory", "path", event.Name, "error", err)
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
							go func(id int64, path string) {
								err := fw.scanner.ScanLibrary(context.Background(), id, path, false)
								switch {
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
						go func(id int64) {
							if err := fw.scanner.CleanupLibrary(context.Background(), id); err != nil {
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

// Stop 停止文件监控
func (fw *FileWatcher) Stop() {
	close(fw.stopCh)
	_ = fw.watcher.Close()
}

func (fw *FileWatcher) watchRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		fw.mu.Lock()
		_, exists := fw.watched[path]
		if !exists {
			fw.watched[path] = struct{}{}
		}
		fw.mu.Unlock()
		if exists {
			return nil
		}

		if err := fw.watcher.Add(path); err != nil {
			fw.mu.Lock()
			delete(fw.watched, path)
			fw.mu.Unlock()
			return err
		}
		return nil
	})
}
