// 业务说明：本文件把 AI 阅读推荐的缓存从 Controller 上帝对象里抽成独立组件。recommendationCache 按 locale
// 缓存推荐结果并带 TTL（默认 24h），并用 singleflight 合并同一 locale 的并发冷缓存/刷新请求，使这些请求只
// 触发一次 LLM 推理。Controller 仅持有 *recommendationCache 引用，取数/回填经其方法。

package api

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type recommendationCache struct {
	mu       sync.RWMutex
	byLocale map[string][]AIRecommendationResponse
	at       map[string]time.Time
	group    singleflight.Group
	ttl      time.Duration
}

func newRecommendationCache(ttl time.Duration) *recommendationCache {
	return &recommendationCache{
		byLocale: make(map[string][]AIRecommendationResponse),
		at:       make(map[string]time.Time),
		ttl:      ttl,
	}
}

// cached 返回某 locale 未过期的缓存推荐；无有效缓存时返回 nil。
func (r *recommendationCache) cached(locale string) []AIRecommendationResponse {
	r.mu.RLock()
	defer r.mu.RUnlock()
	recs := r.byLocale[locale]
	if time.Since(r.at[locale]) < r.ttl && len(recs) > 0 {
		return recs
	}
	return nil
}

// store 回填某 locale 的推荐缓存并记录时间戳。
func (r *recommendationCache) store(locale string, recs []AIRecommendationResponse) {
	r.mu.Lock()
	r.byLocale[locale] = recs
	r.at[locale] = time.Now()
	r.mu.Unlock()
}

// do 用 singleflight 合并同一 locale 的并发计算：并发请求只有一个执行 fn，其余搭车复用结果。
func (r *recommendationCache) do(locale string, fn func() (any, error)) (any, error) {
	v, err, _ := r.group.Do(locale, fn)
	return v, err
}
