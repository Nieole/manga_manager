package scanner

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	stopCh  chan struct{}
	formats []string
}

func NewFileWatcher(s *Scanner) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &FileWatcher{
		scanner: s,
		watcher: w,
		pending: make(map[int64]time.Time),
		libs:    make(map[string]int64),
		stopCh:  make(chan struct{}),
		formats: []string{".zip", ".cbz", ".rar", ".cbr", ".pdf"},
	}, nil
}

// WatchLibrary 开始监听指定库目录
func (fw *FileWatcher) WatchLibrary(libraryID int64, path string) error {
	fw.mu.Lock()
	fw.libs[path] = libraryID
	fw.mu.Unlock()

	err := fw.watcher.Add(path)
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
	delete(fw.libs, path)
	fw.mu.Unlock()
	_ = fw.watcher.Remove(path)
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
				// 只关注 Create 和 Write
				if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
					continue
				}
				// 检查是否是支持的漫画文件
				ext := strings.ToLower(filepath.Ext(event.Name))
				supported := false
				for _, f := range fw.formats {
					if ext == f {
						supported = true
						break
					}
				}
				if !supported {
					continue
				}

				// 找到所属的库
				fw.mu.Lock()
				for libPath, libID := range fw.libs {
					if strings.HasPrefix(event.Name, libPath) {
						fw.pending[libID] = time.Now()
						slog.Debug("File change detected", "file", event.Name, "library_id", libID)
						break
					}
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
								if err := fw.scanner.ScanLibrary(context.Background(), id, path, false); err != nil {
									slog.Error("Hot reload scan failed", "library_id", id, "error", err)
								}
							}(libID, libPath)
						}
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
