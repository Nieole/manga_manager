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
}

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
				p.items[k].archive.Close()
				delete(p.items, k)
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
		_ = p.items[oldestKey].archive.Close()
		delete(p.items, oldestKey)
	}
}

func (p *ArchivePool) max() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxSize
}

// GetArchiveFromPool 尝试从池中获取已打开的文件，如果不存在则延迟创建
func GetArchiveFromPool(path string) (Archive, error) {
	if globalPool == nil {
		return OpenArchive(path)
	}
	if globalPool.max() <= 0 {
		return OpenArchive(path)
	}

	globalPool.mu.Lock()
	if item, ok := globalPool.items[path]; ok {
		// 校验文件是否在缓存后被更新：mtime/size 变化则关闭陈旧句柄并移出池，走下方重建路径，
		// 避免文件更新后最长 10 分钟仍返回旧内容。
		mt, sz := fileSignature(path)
		if mt.Equal(item.modTime) && sz == item.size {
			item.lastUsed = time.Now()
			globalPool.mu.Unlock()
			return item.archive, nil
		}
		item.archive.Close()
		delete(globalPool.items, path)
	}
	globalPool.mu.Unlock()

	// 池中没有，创建一个新的
	arc, err := OpenArchive(path)
	if err != nil {
		return nil, err
	}

	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()

	// 再次检查确认（双检锁变体，防止并发创建）
	if item, ok := globalPool.items[path]; ok {
		arc.Close() // 关闭多余的
		item.lastUsed = time.Now()
		return item.archive, nil
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
			oldest := globalPool.items[oldestKey]
			oldest.archive.Close()
			delete(globalPool.items, oldestKey)
		}
	}

	mt, sz := fileSignature(path)
	globalPool.items[path] = &poolItem{
		archive:  arc,
		lastUsed: time.Now(),
		path:     path,
		modTime:  mt,
		size:     sz,
	}

	return arc, nil
}

// EvictArchiveFromPool closes and removes a cached archive handle for the given path.
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

	_ = item.archive.Close()
	delete(globalPool.items, path)
}

// ResetArchivePool closes and removes every cached archive handle.
func ResetArchivePool() {
	if globalPool == nil {
		return
	}

	globalPool.mu.Lock()
	defer globalPool.mu.Unlock()

	for key, item := range globalPool.items {
		_ = item.archive.Close()
		delete(globalPool.items, key)
	}
}
