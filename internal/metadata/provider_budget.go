// 业务说明：本文件给各元数据源的出站 HTTP 请求加进程级并发闸门。
//
// 刮削任务的启动去重只按 taskKey：全库任务 key 是 scrape_all_series，各库任务是
// scrape_library_<id>。L 个资源库 + 一个全库任务 = 最多 L+1 个刮削任务同时在跑，
// 每个都各自 new 一个 Provider（自带独立 http.Client），进程内没有任何跨调用存活的状态，
// 于是对同一个数据源的请求速率被成倍放大。Comic Vine 的配额是每资源每小时 200 次，
// 撞上去的后果是整批刮削被 429 拖垮，甚至 Key 被封。
//
// 闸门做成 http.RoundTripper 装饰器而不是 Provider 装饰器，理由是取号点必须落在
// **每次真实 HTTP 请求之前**：各 provider 内部都有 3~4 次重试循环，包在 Provider 方法外面
// 会让一次「调用」占一个令牌却发出四次请求，闸门形同虚设。包在 Transport 上则
// 1 令牌 = 1 次真实请求，重试自动被覆盖。

package metadata

import (
	"net/http"
	"sync"
)

// providerConcurrency 是各数据源允许的**进程级**并发出站请求数。
//
// 取值依据是各家的公开限流口径与实际用法，不是拍脑袋：
//   - Comic Vine 明确限「每资源每小时 200 次」，是这里最紧的一家，给 1（完全串行）。
//   - 其余几家没有公布硬配额，但都是免费公共服务，给 2 已经足够让刮削跑满，
//     再高只是把压力转嫁给对方。
//
// 表里没有的 provider 不限流（例如本地 LLM：Ollama 跑在自己机器上，
// 限它没有意义，真正的瓶颈是 images 那边的 max_ai_concurrency）。
var providerConcurrency = map[string]int{
	"Comic Vine":  1,
	"AniList":     2,
	"MangaDex":    2,
	"MyAnimeList": 2,
	"Bangumi":     2,
}

var (
	providerGatesMu sync.Mutex
	providerGates   = make(map[string]chan struct{})
)

// providerGate 返回某数据源的并发闸门（进程级共享）。未配限流的返回 nil。
func providerGate(name string) chan struct{} {
	limit, ok := providerConcurrency[name]
	if !ok || limit <= 0 {
		return nil
	}
	providerGatesMu.Lock()
	defer providerGatesMu.Unlock()
	if gate, exists := providerGates[name]; exists {
		return gate
	}
	gate := make(chan struct{}, limit)
	providerGates[name] = gate
	return gate
}

// budgetedTransport 在每次出站请求前后取还令牌。
type budgetedTransport struct {
	base http.RoundTripper
	gate chan struct{}
}

func (t *budgetedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.gate != nil {
		select {
		case t.gate <- struct{}{}:
			defer func() { <-t.gate }()
		case <-req.Context().Done():
			// 任务被取消时不要傻等令牌。
			return nil, req.Context().Err()
		}
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

// withProviderBudget 给 client 套上该数据源的并发闸门，返回同一个 client 以便链式调用。
// 各 provider 的构造函数在建好 http.Client 之后调用它。
func withProviderBudget(client *http.Client, providerName string) *http.Client {
	gate := providerGate(providerName)
	if gate == nil || client == nil {
		return client
	}
	client.Transport = &budgetedTransport{base: client.Transport, gate: gate}
	return client
}
