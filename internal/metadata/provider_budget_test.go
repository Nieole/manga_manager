// 本文件守卫元数据源的并发闸门。
//
// 刮削任务的启动去重只按 taskKey，全库任务与各库任务可以同时在跑，每个都各自 new 一个
// Provider（自带独立 http.Client）。不在出站请求上收口，对同一数据源的请求速率就会被成倍放大——
// Comic Vine 的配额是每资源每小时 200 次，撞上去整批刮削会被 429 拖垮。

package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestProviderBudgetKeysMatchRealProviderNames 把「字符串表」钉死到真实实现上。
//
// providerConcurrency 用 provider 名做键。名字改了而表没跟着改，闸门会静默失效
// ——没有任何报错，只是限流不再生效。反过来表里留死键也要能被发现。
func TestProviderBudgetKeysMatchRealProviderNames(t *testing.T) {
	realProviders := []Provider{
		NewComicVineProvider("k"),
		NewAniListProvider(),
		NewMangaDexProvider(),
		NewMyAnimeListProvider("k"),
		NewBangumiProvider(),
	}

	seen := make(map[string]bool)
	for _, p := range realProviders {
		name := p.Name()
		seen[name] = true
		if _, ok := providerConcurrency[name]; !ok {
			t.Errorf("provider %q 不在 providerConcurrency 表里 —— 它的出站请求没有闸门", name)
		}
	}
	for name := range providerConcurrency {
		if !seen[name] {
			t.Errorf("providerConcurrency 里的 %q 不对应任何真实 provider —— 死键", name)
		}
	}
}

// TestProviderBudgetLimitsConcurrentRequests 证明闸门是**进程级**的：
// 多个独立构造的 Provider 实例共享同一个桶。实例级的闸门在这里会立刻失败。
func TestProviderBudgetLimitsConcurrentRequests(t *testing.T) {
	var inFlight, maxInFlight int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&inFlight, 1)
		for {
			old := atomic.LoadInt32(&maxInFlight)
			if cur <= old || atomic.CompareAndSwapInt32(&maxInFlight, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const providerName = "Comic Vine" // 表里配的是 1，完全串行
	want := providerConcurrency[providerName]
	if want != 1 {
		t.Fatalf("用例假设 %s 的并发上限是 1，实际配置为 %d", providerName, want)
	}

	// 每个 goroutine 都**独立构造**一个 client：只有按 provider 名跨实例分桶才能限住。
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := withProviderBudget(&http.Client{Timeout: 5 * time.Second}, providerName)
			req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxInFlight); got > int32(want) {
		t.Fatalf("同时在飞的请求数达到 %d，超过闸门上限 %d —— 闸门是实例级的，跨任务不生效", got, want)
	}
}

// TestProviderBudgetRespectsContextCancellation 保证取消时不会傻等令牌。
func TestProviderBudgetRespectsContextCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)

	const providerName = "Comic Vine"
	client := withProviderBudget(&http.Client{Timeout: 5 * time.Second}, providerName)

	// 先占住唯一的那个令牌。
	occupied := make(chan struct{})
	go func() {
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		close(occupied)
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	<-occupied
	time.Sleep(30 * time.Millisecond) // 让它确实进到 RoundTrip 里

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	done := make(chan error, 1)
	go func() {
		_, err := client.Do(req)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("已取消的请求竟然成功了")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("已取消的请求仍在等令牌 —— 取消时不该被闸门挂住")
	}
}

// TestProviderBudgetPassesThroughUnlistedProviders：表里没有的 provider 不加闸门。
func TestProviderBudgetPassesThroughUnlistedProviders(t *testing.T) {
	client := &http.Client{}
	got := withProviderBudget(client, "Ollama LLM")
	if got.Transport != nil {
		t.Fatal("未配限流的 provider 不该被套上闸门 —— 本地 LLM 跑在自己机器上，限它没有意义")
	}
}
