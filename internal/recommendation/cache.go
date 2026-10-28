package recommendation

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type Service struct {
	mu       sync.RWMutex
	byLocale map[string][]Item
	at       map[string]time.Time
	group    singleflight.Group
	ttl      time.Duration
	// gen 是「缓存世代」。purge 递增它，storeAt 只接受与当前世代相同的回填。
	// 没有它的话，一次 LLM 推理（数秒到数十秒）期间若发生删库，推理**返回后**仍会把
	// 基于已删数据算出的推荐写回缓存——purge 看着执行了，陈旧结果却又被填了回来。
	gen int64
}

func New(ttl time.Duration) *Service {
	return &Service{
		byLocale: make(map[string][]Item),
		at:       make(map[string]time.Time),
		ttl:      ttl,
	}
}

// cached 返回某 locale 未过期的缓存推荐；无有效缓存时返回 nil。
func (r *Service) Cached(locale string) []Item {
	r.mu.RLock()
	defer r.mu.RUnlock()
	recs := r.byLocale[locale]
	if time.Since(r.at[locale]) < r.ttl && len(recs) > 0 {
		return recs
	}
	return nil
}

// snapshotGen 取当前世代号，供长耗时计算在开始前记录。
func (r *Service) SnapshotGen() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gen
}

// storeAt 仅在世代未变时回填。gen 与当前不符说明期间发生过 purge（如删库），
// 这份结果是基于已经不存在的数据算出来的，直接丢弃。
func (r *Service) StoreAt(gen int64, locale string, recs []Item) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if gen != r.gen {
		return
	}
	r.byLocale[locale] = recs
	r.at[locale] = time.Now()
}

// purge 清空全部 locale 的缓存并递增世代号。库结构变化（删库等）后必须调用。
func (r *Service) Purge() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gen++
	r.byLocale = make(map[string][]Item)
	r.at = make(map[string]time.Time)
}

// do 用 singleflight 合并同一 locale 的并发计算：并发请求只有一个执行 fn，其余搭车复用结果。
func (r *Service) Do(locale string, fn func() (any, error)) (any, error) {
	v, err, _ := r.group.Do(locale, fn)
	return v, err
}
