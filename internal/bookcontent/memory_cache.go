package bookcontent

import (
	"container/list"
	"sync"
)

// MemoryCache 是按字节预算的 LRU。零值不可用，须经 NewMemoryCache 构造。
type MemoryCache struct {
	mu         sync.Mutex
	maxBytes   int64
	usedBytes  int64
	order      *list.List // 队首最近使用
	byKey      map[string]*list.Element
	evictions  int64
	insertions int64
}

type imageCacheEntry struct {
	key  string
	data []byte
}

// NewMemoryCache creates a synchronized LRU bounded by total stored bytes.
func NewMemoryCache(maxBytes int64) *MemoryCache {
	if maxBytes <= 0 {
		maxBytes = DefaultImageCacheBytes
	}
	return &MemoryCache{
		maxBytes: maxBytes,
		order:    list.New(),
		byKey:    make(map[string]*list.Element),
	}
}

// Get returns immutable cached bytes and marks the entry recently used.
func (c *MemoryCache) Get(key string) ([]byte, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.byKey[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(elem)
	return elem.Value.(*imageCacheEntry).data, true
}

// Add 写入缓存并按需驱逐最久未用的条目，直到总字节回落到预算内。
//
// 单条大于整个预算时直接不收：收了必然立刻把其它全部驱逐光，缓存等于只剩这一条。
func (c *MemoryCache) Add(key string, data []byte) {
	if c == nil {
		return
	}
	size := int64(len(data))
	c.mu.Lock()
	defer c.mu.Unlock()

	if size > c.maxBytes {
		return
	}

	// 同键覆盖：先把旧值的字节减掉再加新值——这正是驱逐回调方案漏掉的那一步。
	if elem, ok := c.byKey[key]; ok {
		entry := elem.Value.(*imageCacheEntry)
		c.usedBytes -= int64(len(entry.data))
		entry.data = data
		c.usedBytes += size
		c.order.MoveToFront(elem)
		c.evictToBudgetLocked()
		return
	}

	elem := c.order.PushFront(&imageCacheEntry{key: key, data: data})
	c.byKey[key] = elem
	c.usedBytes += size
	c.insertions++
	c.evictToBudgetLocked()
}

// evictToBudgetLocked 从队尾驱逐直到总字节不超预算。调用方须持锁。
func (c *MemoryCache) evictToBudgetLocked() {
	for c.usedBytes > c.maxBytes {
		oldest := c.order.Back()
		if oldest == nil {
			// 理论不可达（usedBytes>0 必有条目），但不能让它变成死循环。
			c.usedBytes = 0
			return
		}
		entry := oldest.Value.(*imageCacheEntry)
		c.order.Remove(oldest)
		delete(c.byKey, entry.key)
		c.usedBytes -= int64(len(entry.data))
		c.evictions++
	}
}

// Purge 清空缓存。库结构变化（删库、重建缩略图）后调用。
func (c *MemoryCache) Purge() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.byKey = make(map[string]*list.Element)
	c.usedBytes = 0
}

// Stats reports the current byte use, entry count and evictions.
func (c *MemoryCache) Stats() (usedBytes int64, entries int, evictions int64) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usedBytes, len(c.byKey), c.evictions
}

// DefaultImageCacheBytes 是页图内存缓存的默认预算。
//
// 取 128 MiB 而不是更小：页图磁盘缓存默认是**关闭**的，所以内存未命中意味着
// 重新开归档 + 重解码 + 重转码（还可能拉起 waifu2x/realcugan 子进程）——这是拿内存换 CPU。
// 4 MiB 的大图在这个预算下仍能留下 30 张上下。
const DefaultImageCacheBytes int64 = 128 << 20
