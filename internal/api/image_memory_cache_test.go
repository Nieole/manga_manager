// 本文件守卫页图内存缓存的字节预算：无论怎么写，总字节永不越过预算。
//
// 两条最容易写错的边界：同键覆盖必须先减旧值（用驱逐回调记账的实现正是栽在这里），
// 单条超预算必须直接不收。

package api

import (
	"bytes"
	"strconv"
	"sync"
	"testing"
)

func TestImageMemoryCacheRespectsByteBudget(t *testing.T) {
	const budget = 10 << 10 // 10 KiB
	cache := newImageMemoryCache(budget)

	// 塞入远超预算的数据。
	for i := 0; i < 100; i++ {
		cache.Add("k"+strconv.Itoa(i), make([]byte, 1<<10))
	}

	used, entries, evictions := cache.stats()
	if used > budget {
		t.Fatalf("总字节 %d 越过预算 %d", used, budget)
	}
	if entries == 0 {
		t.Fatal("缓存被清空了 —— 驱逐逻辑过度")
	}
	if evictions == 0 {
		t.Fatal("一次驱逐都没发生，用例前提不成立")
	}
	// 最近写入的应当还在（LRU 语义）。
	if _, ok := cache.Get("k99"); !ok {
		t.Fatal("最近写入的条目被驱逐了 —— 不是 LRU")
	}
}

// TestImageMemoryCacheOverwriteDoesNotLeakBytes 是最关键的一条。
//
// 「LRU + 驱逐回调里记账」的实现会栽在这里：同键覆盖不触发驱逐回调，
// 旧值的字节永久留在计数里，几轮覆盖后计数虚高越过预算，缓存从此再也存不进任何东西。
func TestImageMemoryCacheOverwriteDoesNotLeakBytes(t *testing.T) {
	const budget = 10 << 10
	cache := newImageMemoryCache(budget)

	payload := make([]byte, 1<<10)
	for i := 0; i < 200; i++ {
		cache.Add("same-key", payload)
	}

	used, entries, _ := cache.stats()
	if entries != 1 {
		t.Fatalf("同键覆盖后应只有 1 个条目，实际 %d", entries)
	}
	if used != int64(len(payload)) {
		t.Fatalf("同键覆盖 200 次后计数为 %d，应为单份大小 %d —— 字节计数泄漏了",
			used, len(payload))
	}
	// 计数没泄漏，才还能继续写别的键。
	cache.Add("other", make([]byte, 1<<10))
	if _, ok := cache.Get("other"); !ok {
		t.Fatal("计数虚高导致新条目存不进去")
	}
}

// TestImageMemoryCacheRejectsOversizedEntry：单条比整个预算还大时直接不收。
// 收了必然立刻把其它全部驱逐光，缓存等于只剩这一条。
func TestImageMemoryCacheRejectsOversizedEntry(t *testing.T) {
	const budget = 4 << 10
	cache := newImageMemoryCache(budget)

	cache.Add("small", make([]byte, 1<<10))
	cache.Add("huge", make([]byte, budget+1))

	if _, ok := cache.Get("huge"); ok {
		t.Fatal("超过整个预算的条目被收下了")
	}
	if _, ok := cache.Get("small"); !ok {
		t.Fatal("超大条目把已有条目挤掉了")
	}
}

func TestImageMemoryCachePurgeResetsAccounting(t *testing.T) {
	cache := newImageMemoryCache(10 << 10)
	cache.Add("a", make([]byte, 1<<10))
	cache.Purge()

	used, entries, _ := cache.stats()
	if used != 0 || entries != 0 {
		t.Fatalf("Purge 后 used=%d entries=%d, want 0/0", used, entries)
	}
	cache.Add("b", make([]byte, 1<<10))
	if _, ok := cache.Get("b"); !ok {
		t.Fatal("Purge 之后写不进去了")
	}
}

// TestImageMemoryCacheConcurrentAccess 保证记账与内容在同一把锁内，-race 下无漂移。
func TestImageMemoryCacheConcurrentAccess(t *testing.T) {
	const budget = 64 << 10
	cache := newImageMemoryCache(budget)

	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				key := "k" + strconv.Itoa((w*200+i)%50)
				cache.Add(key, make([]byte, 1<<10))
				cache.Get(key)
			}
		}(w)
	}
	wg.Wait()

	used, _, _ := cache.stats()
	if used > budget {
		t.Fatalf("并发写入后总字节 %d 越过预算 %d —— 记账与内容之间出现了漂移", used, budget)
	}
}

func TestImageMemoryCacheReturnsStoredBytes(t *testing.T) {
	cache := newImageMemoryCache(10 << 10)
	want := []byte("page-image-bytes")
	cache.Add("k", want)
	got, ok := cache.Get("k")
	if !ok || !bytes.Equal(got, want) {
		t.Fatalf("Get = %q ok=%v, want %q", got, ok, want)
	}
}
