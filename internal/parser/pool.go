// 业务说明：本文件是业务实现，属于漫画文件解析层，负责识别归档、目录、页序、页数和可读取图片条目。
// 它是扫描入库、封面提取、阅读器翻页和存储 IO 调度的底层事实来源。
// 维护时应保证多格式兼容、自然排序一致、异常归档可诊断，并避免重复解压造成性能浪费。

package parser

import (
	"log/slog"
	"os"
	"sync"
	"time"
)

type poolItem struct {
	archive  Archive
	lastUsed time.Time
	path     string
	modTime  time.Time // 缓存时文件的修改时间，用于检测文件更新后使缓存失效
	size     int64     // 缓存时文件大小

	// refs 是当前仍在使用该句柄的借用方数量，evicted 表示该 item 已被移出池、只等引用归零后关闭。
	// 池发放的是共享句柄：淘汰/GC/resize/失效都可能在某个请求正读到一半时命中它，
	// 若当场 Close，zip 侧后续读页返回 "file already closed"（500），rar 侧更会向 nil map 写入而 panic。
	// 因此改为引用计数：移出 map 是立即的（新请求拿不到旧句柄），真正 Close 推迟到最后一个借用方归还。
	refs    int
	evicted bool
}

// retainLocked 登记一次借用，调用方必须持有 p.mu。
func (p *ArchivePool) retainLocked(item *poolItem) {
	item.refs++
}

// releaseLocked 归还一次借用；若该 item 已被移出池且再无借用方，此时才真正关闭底层句柄。
func (p *ArchivePool) releaseLocked(item *poolItem) {
	item.refs--
	if item.refs <= 0 && item.evicted {
		_ = item.archive.Close()
	}
}

// evictLocked 把 item 移出池。仍有借用方时只打标记，把 Close 推迟到最后一次 release。
func (p *ArchivePool) evictLocked(key string, item *poolItem) {
	delete(p.items, key)
	item.evicted = true
	if item.refs <= 0 {
		_ = item.archive.Close()
	}
}

// releaseFor 返回该 item 的归还函数，保证幂等（重复调用只生效一次）。
func (p *ArchivePool) releaseFor(item *poolItem) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			p.releaseLocked(item)
			p.mu.Unlock()
		})
	}
}

// noopRelease 供不经池的直开路径使用，让调用方无需区分两种来源。
func noopRelease() {}

// fileSignature 返回文件的修改时间与大小，用作缓存失效判据；出错时返回零值。
func fileSignature(path string) (time.Time, int64) {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime(), info.Size()
	}
	return time.Time{}, 0
}

// ArchivePool 实现一个简单的句柄 LRU 缓存池
type ArchivePool struct {
	mu      sync.Mutex
	items   map[string]*poolItem
	maxSize int
	stopCh  chan struct{} // 停止 GC 协程的信号
}

var (
	globalPool *ArchivePool
	poolMu     sync.Mutex
)

// InitPool 初始化全局归档池
func InitPool(size int) {
	if size <= 0 {
		size = 1
	}

	poolMu.Lock()
	defer poolMu.Unlock()

	if globalPool == nil {
		globalPool = &ArchivePool{
			items:   make(map[string]*poolItem),
			maxSize: size,
			stopCh:  make(chan struct{}),
		}
		go globalPool.gcLoop()
		return
	}

	globalPool.resize(size)
}

// StopGC 停止后台 GC 协程（应用退出时调用）
func StopGC() {
	if globalPool != nil {
		close(globalPool.stopCh)
	}
}

// gcLoop 定期清理超过 10 分钟未被访问的过期句柄，释放文件描述符
func (p *ArchivePool) gcLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			now := time.Now()
			expiredKeys := make([]string, 0)
			for k, v := range p.items {
				if now.Sub(v.lastUsed) > 10*time.Minute {
					expiredKeys = append(expiredKeys, k)
				}
			}
			for _, k := range expiredKeys {
				p.evictLocked(k, p.items[k])
			}
			if len(expiredKeys) > 0 {
				slog.Info("Archive pool GC completed", "evicted", len(expiredKeys), "remaining", len(p.items))
			}
			p.mu.Unlock()
		case <-p.stopCh:
			return
		}
	}
}

func (p *ArchivePool) resize(size int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.maxSize = size
	for len(p.items) > p.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range p.items {
			if oldestKey == "" || v.lastUsed.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastUsed
			}
		}
		if oldestKey == "" {
			return
		}
		p.evictLocked(oldestKey, p.items[oldestKey])
	}
}

func (p *ArchivePool) max() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxSize
}

// PoolMaxSize 返回当前全局归档池的容量上限（未初始化时为 0）。供运行时诊断与配置生效的回归测试观测。
func PoolMaxSize() int {
	if globalPool == nil {
		return 0
	}
	return globalPool.max()
}

// GetArchiveFromPool 尝试从池中获取已打开的文件，如果不存在则延迟创建。
//
// 返回的 release 必须在用完句柄后调用（通常 `defer release()`）：池发放的是共享句柄，
// 引用计数归零之前不会真正关闭，这样淘汰/GC 就不会在别人读到一半时抽走文件描述符。
// release 幂等，且在返回 error 时也非 nil，调用方可以无条件 defer。
func GetArchiveFromPool(path string) (Archive, func(), error) {
	if globalPool == nil || globalPool.max() <= 0 {
		// 不经池：句柄归调用方独占，release 负责关闭。
		arc, err := OpenArchive(path)
		if err != nil {
			return nil, noopRelease, err
		}
		return arc, func() { _ = arc.Close() }, nil
	}

	globalPool.mu.Lock()
	if item, ok := globalPool.items[path]; ok {
		// 校验文件是否在缓存后被更新：mtime/size 变化则移出池并走下方重建路径，
		// 避免文件更新后最长 10 分钟仍返回旧内容。
		mt, sz := fileSignature(path)
		if mt.Equal(item.modTime) && sz == item.size {
			item.lastUsed = time.Now()
			globalPool.retainLocked(item)
			globalPool.mu.Unlock()
			return item.archive, globalPool.releaseFor(item), nil
		}
		globalPool.evictLocked(path, item)
	}
	globalPool.mu.Unlock()

	// 池中没有，创建一个新的
	arc, err := OpenArchive(path)
	if err != nil {
		return nil, noopRelease, err
	}

	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()

	// 再次检查确认（双检锁变体，防止并发创建）
	if item, ok := globalPool.items[path]; ok {
		arc.Close() // 关闭多余的
		item.lastUsed = time.Now()
		globalPool.retainLocked(item)
		return item.archive, globalPool.releaseFor(item), nil
	}

	// 检查是否超过最大限制，执行淘汰
	if len(globalPool.items) >= globalPool.maxSize {
		var oldestKey string
		var oldestTime time.Time
		for k, v := range globalPool.items {
			if oldestKey == "" || v.lastUsed.Before(oldestTime) {
				oldestKey = k
				oldestTime = v.lastUsed
			}
		}
		if oldestKey != "" {
			globalPool.evictLocked(oldestKey, globalPool.items[oldestKey])
		}
	}

	mt, sz := fileSignature(path)
	item := &poolItem{
		archive:  arc,
		lastUsed: time.Now(),
		path:     path,
		modTime:  mt,
		size:     sz,
	}
	globalPool.items[path] = item
	globalPool.retainLocked(item)

	return arc, globalPool.releaseFor(item), nil
}

// EvictArchiveFromPool 把某路径的缓存句柄移出池；仍被借用时推迟到引用归零后关闭。
func EvictArchiveFromPool(path string) {
	if globalPool == nil {
		return
	}

	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()

	item, ok := globalPool.items[path]
	if !ok {
		return
	}

	globalPool.evictLocked(path, item)
}

// ResetArchivePool 移出全部缓存句柄；仍被借用的推迟到引用归零后关闭。
func ResetArchivePool() {
	if globalPool == nil {
		return
	}

	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()

	for key, item := range globalPool.items {
		globalPool.evictLocked(key, item)
	}
}
