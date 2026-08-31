// 本文件给各元数据源的出站 HTTP 请求加进程级并发闸门：多个刮削任务可同时在跑，
// 各自 new 一个 Provider，没有跨调用存活的状态，对同一数据源的请求速率会被成倍
// 放大（Comic Vine 配额是每资源每小时 200 次，撞上去整批刮削会被 429 拖垮）。
//
// 闸门做成 http.RoundTripper 装饰器而非 Provider 装饰器：取号点必须落在每次真实
// HTTP 请求之前，包在 Provider 方法外会让内部的重试循环只占一个令牌却发出多次请求。

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
