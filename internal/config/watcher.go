// 本文件是配置文件的热重载监听器：目录事件经去抖合并后重载配置，坏值或文件短暂缺失都
// 保留当前内存配置不动。

package config

import (
	"bytes"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// configReloadDebounce 是事件去抖窗口。
//
// 一次保存会产生多个事件：编辑器自身可能写多次，而本项目的 AtomicWriteFile
// （建临时文件 → rename 覆盖）在目录监听下也会依次产生 Create / Rename / Write。
// 更麻烦的是设置页保存走的同样是 AtomicWriteFile，于是每次 UI 保存都会回弹一次事件，
// 让刚刚生效的配置再被完整重载一遍（自写回环）。
const configReloadDebounce = 500 * time.Millisecond

// Watcher 监听配置文件变化并在其取值合法时重建派生资源。零值不可用，须经 StartWatcher 构造。
//
// 监听的是文件所在目录而非文件本身：编辑器保存与本项目的原子写都是 rename 替换，直接 watch
// 文件在 Linux 上会因 inode 变化而永久失效。重载只走 LoadConfigFile，不得用 LoadConfig——
// 后者在文件不存在时会生成默认配置并写回磁盘，而这条路径必然会撞上「先 rename 走再写新文件」
// 的中间态。校验只跑纯值域（ValidateConfigValues）：触盘的深度校验可能在离线网络挂载上
// 无限阻塞，而它跑在事件循环里，卡住就等于连带卡死停机等待。
type Watcher struct {
	path     string
	manager  *Manager
	apply    func(*Config) error
	debounce time.Duration

	fs       *fsnotify.Watcher
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// StartWatcher 开始监听 path 所在目录中该配置文件的变化。
// apply 用于重建配置派生资源（与设置页保存路径共用同一个，避免两条路径副作用不一致）；可为 nil。
func StartWatcher(path string, manager *Manager, apply func(*Config) error) (*Watcher, error) {
	if manager == nil {
		return nil, errors.New("config: watcher requires a manager")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(absPath)
	if err := fsw.Add(dir); err != nil {
		fsw.Close()
		return nil, err
	}

	w := &Watcher{
		path:     absPath,
		manager:  manager,
		apply:    apply,
		debounce: configReloadDebounce,
		fs:       fsw,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go w.run(fsw.Events, fsw.Errors)
	slog.Info("Config hot-reload watcher started", "path", absPath, "watching_dir", dir)
	return w, nil
}

// Stop 停止监听并等待事件循环退出。对 nil 接收者安全（StartWatcher 失败时调用方拿到的就是 nil）。
func (w *Watcher) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() {
		close(w.stop)
		if w.fs != nil {
			w.fs.Close()
		}
	})
	<-w.done
}

// run 是事件循环。events/errors 作参数传入而非直接读 w.fs，使测试可以注入事件而不必真的碰文件系统。
func (w *Watcher) run(events <-chan fsnotify.Event, errs <-chan error) {
	defer close(w.done)

	// 去抖：收到事件后不立刻重载，而是把定时器推后；突发事件因此只触发一次重载。
	timer := time.NewTimer(w.debounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	pending := false

	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if filepath.Clean(event.Name) != w.path {
				continue // 只关心目标 config 文件，忽略同目录下的临时文件与数据文件事件
			}
			// 原子替换与编辑器保存表现为 Write、Create 或 Rename-到位，任一都算一次变更。
			if !event.Has(fsnotify.Write) && !event.Has(fsnotify.Create) && !event.Has(fsnotify.Rename) {
				continue
			}
			if pending && !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.debounce)
			pending = true
		case <-timer.C:
			pending = false
			w.reload()
		case err, ok := <-errs:
			if !ok {
				return
			}
			slog.Error("Config watcher error", "error", err)
		}
	}
}

// reload 读盘、校验、比对、生效。任一环节不通过都保留当前内存配置。
func (w *Watcher) reload() {
	if err := w.manager.Reload(w.path, w.apply, func() {
		applied := w.manager.Snapshot()
		slog.Info("Config hot-reload applied successfully",
			"log_level", applied.Logging.Level,
			"pool_size", applied.Scanner.ArchivePoolSize,
			"ai_concurrency", applied.Scanner.MaxAiConcurrency)
	}); err != nil {
		slog.Error("Config hot-reload failed; keeping current configuration", "path", w.path, "error", err)
		return
	}
}

// configsEqual 按 YAML 序列化结果比较两份配置。
// 用序列化而不是 reflect.DeepEqual：后者会把 nil 切片与空切片判为不等，
// 而这两者经 YAML 往返后本就会互相转化，会让「内容没变」的判断恒为 false。
func configsEqual(a, b *Config) bool {
	if a == nil || b == nil {
		return a == b
	}
	left, err1 := yaml.Marshal(a)
	right, err2 := yaml.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(left, right)
}

// FormatValidationIssues 把校验问题压成一行可读文本供日志输出
// （slog 直接打结构体切片可读性很差）。
func FormatValidationIssues(issues []ValidationIssue) string {
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, issue.Field+": "+issue.Message)
	}
	return strings.Join(parts, "; ")
}
