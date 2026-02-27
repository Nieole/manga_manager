package parser

import (
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
		}
	})
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
