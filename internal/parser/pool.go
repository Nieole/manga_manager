package parser

import (
	"log/slog"
	"sync"
	"time"
)

type poolItem struct {
	archive  Archive
	lastUsed time.Time
	path     string
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
	once       sync.Once
)

// InitPool 初始化全局归档池
func InitPool(size int) {
	once.Do(func() {
		globalPool = &ArchivePool{
			items:   make(map[string]*poolItem),
			maxSize: size,
			stopCh:  make(chan struct{}),
		}
		go globalPool.gcLoop()
	})
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

// GetArchiveFromPool 尝试从池中获取已打开的文件，如果不存在则延迟创建
func GetArchiveFromPool(path string) (Archive, error) {
	if globalPool == nil {
		return OpenArchive(path)
	}

	globalPool.mu.Lock()
	if item, ok := globalPool.items[path]; ok {
		item.lastUsed = time.Now()
		globalPool.mu.Unlock()
		return item.archive, nil
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

	globalPool.items[path] = &poolItem{
		archive:  arc,
		lastUsed: time.Now(),
		path:     path,
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
