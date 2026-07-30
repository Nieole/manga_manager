// 业务说明：本文件是页图/封面的内存缓存，按**总字节数**而非条数限流。
//
// 此前用的是 hashicorp/golang-lru 的按条数 LRU（256 条），配合 4 MiB 的单条上限，
// 常驻上界就是 256 × 4 MiB = 1 GiB —— 而这只是个加速缓存，没有任何理由占到那个量级。
//
// 为什么不复用现成库：golang-lru/v2 v2.0.7 没有 size/weight-aware 变体，
// 而「LRU + 驱逐回调里记账」这条路走不通，有两处硬伤：
//   - 回调在**锁外**调用（其源码注释明说 "invoke callback outside of critical section"），
//     字节计数与缓存内容之间没有原子性，并发写入下计数必然漂移；
//   - 同键覆盖不触发驱逐回调（命中已有 key 时只替换 value 并返回 false），
//     旧值的字节会永久留在计数里，几轮覆盖后计数虚高越过预算，缓存从此再也存不进任何东西。
//
// 所以这里自己维护一个「双向链表 + map」的 LRU，读写与记账在同一把锁内完成。

package api

import (
	"container/list"
	"sync"
)

// imageMemoryCache 是按字节预算的 LRU。零值不可用，须经 newImageMemoryCache 构造。
type imageMemoryCache struct {
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

func newImageMemoryCache(maxBytes int64) *imageMemoryCache {
	if maxBytes <= 0 {
		maxBytes = defaultImageCacheBytes
	}
	return &imageMemoryCache{
		maxBytes: maxBytes,
		order:    list.New(),
		byKey:    make(map[string]*list.Element),
	}
}

// Get 取缓存并把该条目提到队首。返回的切片是缓存内部持有的那一份，
// 调用方**不得修改**——所有调用点都只是把它写进 HTTP 响应。
func (c *imageMemoryCache) Get(key string) ([]byte, bool) {
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
func (c *imageMemoryCache) Add(key string, data []byte) {
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
func (c *imageMemoryCache) evictToBudgetLocked() {
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
func (c *imageMemoryCache) Purge() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.order.Init()
	c.byKey = make(map[string]*list.Element)
	c.usedBytes = 0
}

// stats 供测试与诊断读取当前水位。
func (c *imageMemoryCache) stats() (usedBytes int64, entries int, evictions int64) {
	if c == nil {
		return 0, 0, 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usedBytes, len(c.byKey), c.evictions
}

// defaultImageCacheBytes 是页图内存缓存的默认预算。
//
// 取 128 MiB 而不是更小：页图磁盘缓存默认是**关闭**的，所以内存未命中意味着
// 重新开归档 + 重解码 + 重转码（还可能拉起 waifu2x/realcugan 子进程）——这是拿内存换 CPU。
// 相对此前 1 GiB 的常驻上界收敛了 8 倍，同时 4 MiB 的大图仍能留下 30 张上下。
const defaultImageCacheBytes int64 = 128 << 20
